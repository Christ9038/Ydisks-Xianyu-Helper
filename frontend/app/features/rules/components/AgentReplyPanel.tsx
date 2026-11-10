import { AlertCircle } from 'lucide-react';
import React, { useEffect } from 'react';
import { AGENT_REQUEST_TIMEOUT_MAX, AGENT_REQUEST_TIMEOUT_MIN, AGENT_SESSION_MAX, AGENT_SESSION_MIN, useAgentReply } from '../agentReply';
import type { AgentReplyTestResult } from '../api';
import Toast from './Toast';

// AGENT_REPLY_TOAST_MS 是操作提示的自动消失时长（毫秒）。
const AGENT_REPLY_TOAST_MS = 2500;

// AGENT_PROTOCOL_EXAMPLE 是配置页展示的协议示例，只包含占位数据，不含任何真实凭证。
const AGENT_PROTOCOL_EXAMPLE = `请求（POST JSON，可选 Authorization: Bearer <令牌>）：
{"version":1,"event":"message","turn_id":"a1b2","sequence":1,
 "account_id":"账号","chat_id":"会话","item_id":"商品",
 "buyer":{"user_id":"买家","name":"昵称"},
 "message":{"id":"消息","type":"text","text":"在吗","image_url":""}}
// 买家发图片时 type 为 "image"，image_url 为公网地址；仍在处理时助手会用 event:"poll" 继续查询（不带 message）。

响应：
{"handled":true,"working":false,
 "replies":[{"text":"在的","image_url":""}],"poll_after_ms":2000}
// handled:false 表示不接手，交给 AI / 默认回复；working:true 表示仍在处理，助手稍后继续 poll。`;

/** AgentReplyPanelProps 描述 Agent 回复面板需要的账号上下文。 */
interface AgentReplyPanelProps {
  /** accountID 是规则页当前选择的账号；空串表示全部账号，此时不能配置。 */
  accountID: string;
}

// testResultText 把连接测试的诊断结果转换为一行用户可读文案。
const testResultText = (result: AgentReplyTestResult): string => {
  switch (result.status) {
    case 'ok': return `连接成功（${result.duration_ms} ms）：${result.handled ? '接手' : '不接手'}，${result.working ? '仍在处理' : '已完成'}，返回 ${result.reply_count} 条回复`;
    case 'http_error': return `Agent 返回了 HTTP ${result.status_code}，请检查地址与访问令牌`;
    case 'invalid_response': return 'Agent 返回的内容不符合协议，请检查响应 JSON';
    default: return '无法连接到 Agent，请检查地址、网络和出站限制';
  }
};

/** AgentReplyPanel 配置“把买家消息转发给自建 Agent”的账号级回复：地址、令牌、超时与连接测试。 */
const AgentReplyPanel: React.FC<AgentReplyPanelProps> = ({ accountID }) => {
  // agent 汇总配置、表单草稿和异步动作；所有请求都由 Hook 负责取消与过期丢弃。
  const agent = useAgentReply(accountID);
  // form、settings、toast 是编辑中的草稿、服务端确认的配置与操作提示。
  const { form, settings, toast, dismissToast } = agent;

  useEffect(/* 当前副作用在出现提示后定时清除，卸载或提示变化时清理计时器。 */ () => {
    if (!toast) return;
    // timer 是提示自动消失的计时器。
    const timer = window.setTimeout(dismissToast, AGENT_REPLY_TOAST_MS);
    return /* 当前回调清理未触发的提示计时器。 */ () => window.clearTimeout(timer);
  }, [toast, dismissToast]);

  // tokenConfigured 表示服务端已保存访问令牌；令牌明文永远不会回到浏览器。
  const tokenConfigured = settings?.token_configured === true;
  // busy 表示读取、保存或测试正在进行，期间禁用相关按钮。
  const busy = agent.loading || agent.submitting;

  return (
    <section className="bg-white rounded-xl border border-gray-100 p-6 shadow-sm space-y-5">
      <div className="flex items-start gap-2 text-sm text-blue-700 bg-blue-50 px-4 py-2 rounded-xl">
        <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
        <span>
          开启后，没有命中关键词的买家消息（文字或图片）会转发给你自己开发的 Agent，并把 Agent 的回复发回给买家；助手本身不做 Agent。
          优先级：商品首次回复 → 关键词 → Agent → AI → 默认回复。Agent 不接手或请求失败时，会继续走 AI 与默认回复。Agent 的回复不会触发自动改价。
        </span>
      </div>

      {!accountID && <div className="text-center py-16 text-gray-400">请先在右上角选择账号</div>}
      {accountID && agent.loadError && (
        <div className="text-sm text-red-600 bg-red-50 px-4 py-2 rounded-xl flex items-center justify-between gap-3">
          <span>加载失败：{agent.loadError}</span>
          <button onClick={/* 当前回调重新读取配置。 */ () => void agent.reload()} className="font-bold underline">重试</button>
        </div>
      )}

      {accountID && !agent.loadError && (
        <div className="space-y-5">
          <label className="flex items-center justify-between p-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
            <span>启用 Agent 回复</span>
            <input type="checkbox" checked={form.enabled} disabled={agent.loading} onChange={/* 当前回调更新启用状态。 */ event => agent.updateForm({ enabled: event.target.checked })} className="w-5 h-5" />
          </label>

          <div>
            <label htmlFor="agent-endpoint" className="block text-sm font-bold text-gray-700 mb-2">Agent 地址</label>
            <input id="agent-endpoint" value={form.endpointUrl} disabled={agent.loading} onChange={/* 当前回调更新 Agent 地址。 */ event => agent.updateForm({ endpointUrl: event.target.value })} className="w-full ios-input px-4 py-3 rounded-xl" placeholder="https://agent.example.com/xianyu" autoComplete="off" />
          </div>

          <div>
            <span className="block text-sm font-bold text-gray-700 mb-2">访问令牌（可选，作为 Bearer 令牌发送）</span>
            {form.tokenAction === 'retain' && tokenConfigured && (
              <div className="flex items-center gap-3 text-sm">
                <span className="px-3 py-2 bg-green-50 text-green-700 rounded-xl font-bold">已保存令牌（不会显示）</span>
                <button onClick={agent.beginReplaceToken} className="font-bold text-blue-600 hover:underline">更换</button>
                <button onClick={agent.markClearToken} className="font-bold text-red-500 hover:underline">清除</button>
              </div>
            )}
            {form.tokenAction === 'clear' && (
              <div className="flex items-center gap-3 text-sm">
                <span className="px-3 py-2 bg-amber-50 text-amber-700 rounded-xl font-bold">保存后将清除已保存的令牌</span>
                <button onClick={agent.keepToken} className="font-bold text-blue-600 hover:underline">撤销</button>
              </div>
            )}
            {(form.tokenAction === 'replace' || (form.tokenAction === 'retain' && !tokenConfigured)) && (
              <div className="flex items-center gap-3">
                <input type="password" value={form.tokenValue} onChange={/* 当前回调更新新令牌输入。 */ event => agent.setTokenValue(event.target.value)} className="w-full ios-input px-4 py-3 rounded-xl" placeholder="留空表示不使用令牌" autoComplete="new-password" aria-label="访问令牌" />
                {tokenConfigured && <button onClick={agent.keepToken} className="shrink-0 font-bold text-sm text-gray-600 hover:underline">取消更换</button>}
              </div>
            )}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label htmlFor="agent-request-timeout" className="block text-sm font-bold text-gray-700 mb-2">单次请求超时（秒）</label>
              <input id="agent-request-timeout" type="number" min={AGENT_REQUEST_TIMEOUT_MIN} max={AGENT_REQUEST_TIMEOUT_MAX} value={form.requestTimeoutSeconds} onChange={/* 当前回调更新单次请求超时。 */ event => agent.updateForm({ requestTimeoutSeconds: Number(event.target.value) })} className="w-full ios-input px-4 py-3 rounded-xl" />
              <p className="mt-2 text-xs text-gray-400">{AGENT_REQUEST_TIMEOUT_MIN}–{AGENT_REQUEST_TIMEOUT_MAX} 秒</p>
            </div>
            <div>
              <label htmlFor="agent-session" className="block text-sm font-bold text-gray-700 mb-2">单轮对话总时长上限（秒）</label>
              <input id="agent-session" type="number" min={AGENT_SESSION_MIN} max={AGENT_SESSION_MAX} value={form.maxSessionSeconds} onChange={/* 当前回调更新单轮总时长。 */ event => agent.updateForm({ maxSessionSeconds: Number(event.target.value) })} className="w-full ios-input px-4 py-3 rounded-xl" />
              <p className="mt-2 text-xs text-gray-400">含轮询等待；{AGENT_SESSION_MIN}–{AGENT_SESSION_MAX} 秒。单轮最多请求 60 次、发送 10 条回复（其中图片 3 张）。</p>
            </div>
          </div>

          {agent.testResult && (
            <div role="status" className={`text-sm px-4 py-3 rounded-xl ${agent.testResult.status === 'ok' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-600'}`}>{testResultText(agent.testResult)}</div>
          )}

          <div className="flex justify-end gap-3">
            <button onClick={/* 当前回调发起连接测试。 */ () => void agent.test()} disabled={busy || agent.testing} className="px-5 py-3 rounded-2xl font-bold bg-gray-100 hover:bg-gray-200 text-gray-700 disabled:opacity-50">{agent.testing ? '测试中…' : '测试连接'}</button>
            <button onClick={/* 当前回调保存配置。 */ () => void agent.save()} disabled={busy} className="ios-btn-primary px-6 py-3 rounded-2xl font-extrabold disabled:opacity-50">{agent.submitting ? '保存中…' : '保存'}</button>
          </div>

          <details className="text-sm text-gray-600">
            <summary className="cursor-pointer font-bold text-gray-700">Agent 接口协议</summary>
            <pre className="mt-3 p-4 bg-gray-50 rounded-xl overflow-x-auto text-xs whitespace-pre-wrap break-all">{AGENT_PROTOCOL_EXAMPLE}</pre>
          </details>
        </div>
      )}
      <Toast toast={toast} />
    </section>
  );
};

export default AgentReplyPanel;
