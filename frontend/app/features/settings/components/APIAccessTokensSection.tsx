import React, { useState } from 'react';
import { Copy, KeyRound, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import { useAPIAccessTokenList } from '../useAPIAccessTokenList';
import {
  createAPIAccessToken,
  revokeAPIAccessToken,
  type APIAccessScope,
  type APIAccessTokenSummary,
} from '../api';

// APIAccessScopeOption 描述一项可签发权限及其用户可读的业务含义。
interface APIAccessScopeOption {
  /** value 是提交给服务端的精确权限标识。 */
  value: APIAccessScope;
  /** label 是权限选择器展示的简短名称。 */
  label: string;
  /** description 说明授予该权限后允许执行的实际操作。 */
  description: string;
}

// scopeOptions 是用户可独立勾选的 API 权限及其实际业务范围说明。
const scopeOptions: APIAccessScopeOption[] = [
  { value: 'accounts:read', label: '账号列表读取', description: '读取当前用户的闲鱼账号 ID 与非敏感账号摘要' },
  { value: 'items:read', label: '商品与发布结果读取', description: '读取商品、同步商品列表及批量发布结果' },
  { value: 'items:publish', label: '商品发布', description: '单品发布、批量发布、取消任务与失败重试' },
  { value: 'items:unpublish', label: '商品平台下架', description: '在闲鱼平台下架当前用户拥有的商品，不删除商品' },
  { value: 'orders:read', label: '订单读取', description: '读取订单列表和详情（包含收件人姓名、电话和地址），不包含已发货卡密' },
  { value: 'chat:read', label: '聊天读取', description: '读取聊天会话、消息、商品上下文、快捷回复与买家备注' },
  { value: 'chat:send', label: '发送客服消息', description: '通过现有聊天接口向买家发送文本消息；发送后沿用现有人工介入识别' },
  { value: 'chat:takeover', label: '人工介入设置', description: '调整人工回复后暂停自动回复的时长和范围，不允许修改账号凭据' },
  { value: 'replies:manage', label: '客服规则管理', description: '管理 AI、关键词、默认回复、商品首次回复和 Agent 配置' },
  { value: 'automation:read', label: '自动化审计读取', description: '读取自动化规则、发货模板、运行问题、审计和账号任务记录' },
  { value: 'automation:manage', label: '自动化管理', description: '修改自动化规则与发货模板，更新并执行现有账号任务，处理待处理任务' },
];

// formatUnixTime 将 Unix 秒数转换为本地可读时间；0 表示没有对应记录。
const formatUnixTime = (value: number): string => value > 0 ? new Date(value * 1000).toLocaleString() : '从未';

// APIAccessTokensSection 提供外部 API 令牌的签发、查看和撤销管理。
const APIAccessTokensSection: React.FC = () => {
  // tokens、loading、loadError、loadTokens 来自隔离过期响应的列表读取 Hook。
  const { tokens, loading, loadError, loadTokens } = useAPIAccessTokenList();
  // name 保存新令牌的用途名称草稿。
  const [name, setName] = useState('');
  // scopes 保存用户选择的最小权限集合。
  const [scopes, setScopes] = useState<APIAccessScope[]>([]);
  // expiresInDays 保存新令牌的有效期。
  const [expiresInDays, setExpiresInDays] = useState(90);
  // creating 表示当前是否正在签发令牌。
  const [creating, setCreating] = useState(false);
  // revokingId 保存正在撤销的令牌标识。
  const [revokingId, setRevokingId] = useState('');
  // createdToken 保存刚签发、仅当前页面可见的一次性明文。
  const [createdToken, setCreatedToken] = useState('');
  // notice 保存复制或操作的反馈信息。
  const [notice, setNotice] = useState('');

  // toggleScope 在用户勾选或取消权限时更新集合。
  const toggleScope = (scope: APIAccessScope) => {
    setScopes(
      /* current 是 React 在本次勾选操作前保存的权限集合。 */
      current => current.includes(scope)
        ? current.filter(/* value 是当前权限集合中待判断是否保留的权限标识。 */ value => value !== scope)
        : [...current, scope],
    );
  };

  // handleCreate 校验最小输入后签发令牌，并保留一次性明文供用户复制。
  const handleCreate = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!name.trim() || scopes.length === 0 || creating) return;
    setCreating(true);
    setNotice('');
    try {
      // result 是本次签发返回的一次性令牌和非敏感摘要。
      const result = await createAPIAccessToken({ name: name.trim(), scopes, expires_in_days: expiresInDays });
      setCreatedToken(result.token);
      setName('');
      setScopes([]);
      await loadTokens();
    } catch {
      setNotice('签发失败，请检查名称和权限设置后重试。');
    } finally {
      setCreating(false);
    }
  };

  // handleCopy 将刚签发的明文复制到系统剪贴板；不写入浏览器持久存储。
  const handleCopy = async () => {
    if (!createdToken) return;
    try {
      await navigator.clipboard.writeText(createdToken);
      setNotice('令牌已复制。请立即保存到安全的密码管理器。');
    } catch {
      setNotice('自动复制不可用，请手动复制上方令牌并立即保存。');
    }
  };

  // handleRevoke 撤销指定令牌；服务端会立即拒绝后续 Bearer 请求。
  const handleRevoke = async (token: APIAccessTokenSummary) => {
    if (revokingId || token.revoked_at > 0 || !window.confirm(`确定撤销「${token.name}」吗？撤销后使用该令牌的调用会立即失效。`)) return;
    setRevokingId(token.id);
    setNotice('');
    try {
      await revokeAPIAccessToken(token.id);
      await loadTokens();
      setNotice(`已撤销「${token.name}」。`);
    } catch {
      setNotice('撤销失败，请刷新后重试。');
    } finally {
      setRevokingId('');
    }
  };

  return (
    <section className="space-y-4">
      <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
        <span className="p-1.5 rounded-lg bg-blue-600 text-white"><KeyRound className="w-4 h-4" /></span>
        外部 API 访问
      </h3>
      <div className="ios-card rounded-xl p-6 bg-white space-y-5">
        <div className="space-y-2">
          <p className="text-sm text-gray-700">为 ChatGPT、Agent 或其他外部程序签发独立访问令牌。令牌只绑定当前 Ydisks 用户，不包含闲鱼 Cookie。</p>
          <p className="text-xs text-gray-500">安全规则：服务端只保存 SHA-256 摘要；创建时展示一次；默认 90 天有效，最长 365 天；可单独撤销。API 令牌不能管理其他令牌、修改登录凭据或访问管理员设置。</p>
        </div>

        {createdToken && (
          <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 space-y-3">
            <div className="flex items-center gap-2 text-sm font-bold text-amber-900"><ShieldCheck className="w-4 h-4" />新令牌已签发，请立即保存</div>
            <p className="text-xs text-amber-800">离开此页面或关闭本提示后，系统不会再次显示完整令牌。不要把令牌发到聊天记录、代码仓库或截图中。</p>
            <div className="flex gap-2">
              <input readOnly value={createdToken} className="min-w-0 flex-1 rounded-lg border border-amber-200 bg-white px-3 py-2 font-mono text-xs text-gray-800" />
              <button type="button" onClick={handleCopy} className="shrink-0 rounded-lg bg-gray-900 px-3 py-2 text-xs font-bold text-white flex items-center gap-1"><Copy className="w-3.5 h-3.5" />复制</button>
            </div>
            <button type="button" onClick={/* 点击后关闭一次性令牌展示并清除页面内的明文。 */ () => setCreatedToken('')} className="text-xs font-bold text-amber-900 underline">我已安全保存，关闭一次性展示</button>
          </div>
        )}

        <form onSubmit={handleCreate} className="space-y-4 border-t border-gray-100 pt-5">
          <div className="grid grid-cols-1 sm:grid-cols-[1fr_160px] gap-3">
            <label className="space-y-1.5">
              <span className="block text-sm font-bold text-gray-800">令牌名称</span>
              <input value={name} onChange={/* event 是用户修改令牌用途名称时产生的输入事件。 */ event => setName(event.target.value)} maxLength={64} placeholder="例如：ChatGPT 商品客服 Agent" className="w-full ios-input px-3 py-2.5 rounded-lg text-sm" />
            </label>
            <label className="space-y-1.5">
              <span className="block text-sm font-bold text-gray-800">有效期</span>
              <select value={expiresInDays} onChange={/* event 是用户切换令牌有效期时产生的选择事件。 */ event => setExpiresInDays(Number(event.target.value))} className="w-full ios-input px-3 py-2.5 rounded-lg text-sm">
                <option value={30}>30 天</option>
                <option value={90}>90 天</option>
                <option value={180}>180 天</option>
                <option value={365}>365 天</option>
              </select>
            </label>
          </div>
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-bold text-gray-800">授权权限</span>
              <span className="text-xs text-gray-500">已选 {scopes.length} 项；未选择的能力默认拒绝</span>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
              {scopeOptions.map(/* option 是当前权限选择卡片及其展示说明。 */ option => (
                <label key={option.value} className={`flex items-start gap-2.5 rounded-lg border p-3 cursor-pointer ${scopes.includes(option.value) ? 'border-blue-300 bg-blue-50' : 'border-gray-200 bg-white'}`}>
                  <input type="checkbox" checked={scopes.includes(option.value)} onChange={/* 点击当前权限复选框后切换其授权状态。 */ () => toggleScope(option.value)} className="mt-0.5 h-4 w-4 rounded border-gray-300" />
                  <span className="min-w-0"><span className="block text-sm font-semibold text-gray-800">{option.label}</span><span className="block mt-0.5 text-xs text-gray-500">{option.description}</span><span className="block mt-1 font-mono text-[10px] text-gray-400">{option.value}</span></span>
                </label>
              ))}
            </div>
          </div>
          <button type="submit" disabled={creating || !name.trim() || scopes.length === 0} className="rounded-lg bg-gray-900 px-5 py-2.5 text-sm font-bold text-white disabled:opacity-40">{creating ? '正在签发...' : '创建 API 访问令牌'}</button>
        </form>

        <div className="space-y-3 border-t border-gray-100 pt-5">
          <div className="flex items-center justify-between gap-3">
            <h4 className="text-sm font-bold text-gray-800">已签发令牌</h4>
            <button type="button" onClick={/* 用户手动刷新时重新读取当前用户的令牌摘要。 */ () => void loadTokens()} disabled={loading} className="flex items-center gap-1.5 text-xs font-semibold text-gray-600 hover:text-gray-900 disabled:opacity-50"><RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />刷新</button>
          </div>
          {loadError && <p className="text-sm text-red-600">{loadError}</p>}
          {!loading && !loadError && tokens.length === 0 && <p className="rounded-lg bg-gray-50 px-4 py-6 text-center text-sm text-gray-500">尚未签发 API 访问令牌。</p>}
          <div className="space-y-2">
            {tokens.map(/* token 是当前用户拥有的一条令牌摘要。 */ token => {
              // expired 表示令牌已过期；撤销状态由服务端持久化字段决定。
              const expired = token.expires_at <= Math.floor(Date.now() / 1000);
              // disabled 表示令牌已经撤销或超过有效期。
              const disabled = token.revoked_at > 0 || expired;
              return (
                <div key={token.id} className="rounded-lg border border-gray-200 p-3 space-y-2">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="min-w-0"><span className="block truncate text-sm font-bold text-gray-800">{token.name}</span><span className="font-mono text-xs text-gray-500">{token.prefix}••••••••</span></div>
                    <div className="flex items-center gap-2">
                      <span className={`rounded-full px-2 py-1 text-[11px] font-semibold ${disabled ? 'bg-gray-100 text-gray-500' : 'bg-green-50 text-green-700'}`}>{token.revoked_at > 0 ? '已撤销' : expired ? '已过期' : '有效'}</span>
                      {!disabled && <button type="button" onClick={/* 用户确认撤销后调用当前令牌的撤销流程。 */ () => void handleRevoke(token)} disabled={revokingId === token.id} className="flex items-center gap-1 rounded-md px-2 py-1 text-xs font-semibold text-red-600 hover:bg-red-50 disabled:opacity-50"><Trash2 className="w-3.5 h-3.5" />{revokingId === token.id ? '撤销中' : '撤销'}</button>}
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-1">{token.scopes.map(/* scope 是当前令牌被授予的一项权限。 */ scope => <span key={scope} className="rounded bg-gray-100 px-1.5 py-1 font-mono text-[10px] text-gray-600">{scope}</span>)}</div>
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-1 text-[11px] text-gray-500"><span>签发：{formatUnixTime(token.created_at)}</span><span>过期：{formatUnixTime(token.expires_at)}</span><span>最近使用：{formatUnixTime(token.last_used_at)}</span></div>
                </div>
              );
            })}
          </div>
        </div>
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900">
          商品平台下架使用独立的 `items:unpublish` 权限和下架接口；本地“删除”仍是另一种操作，不会被当作平台下架。下架接口已按公开抓包资料接入，但仍需使用真实账号完成一次生产外部验收后，才能确认当前账号环境可用。
        </div>
        {notice && <p className="text-xs text-gray-600" role="status">{notice}</p>}
      </div>
    </section>
  );
};

export default APIAccessTokensSection;
