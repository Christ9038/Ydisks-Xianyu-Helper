import { AlertCircle,Bot,Edit,Plus,RefreshCw,Trash2 } from 'lucide-react';
import { useEffect } from 'react';
import type { DefaultReplyActionsState } from '../defaultReplyActions';
import type { AccountDetail,DefaultReply,Item } from '../models';
import { accountLabel } from '../utils';

/** DefaultRepliesPanelProps 提供账号兜底摘要、商品参考数据及统一配置动作。 */
interface DefaultRepliesPanelProps {
  /** selectedAccountId 是页面账号筛选，空值显示当前用户全部账号。 */
  selectedAccountId: string;
  /** accounts 是当前用户账号摘要。 */
  accounts: AccountDetail[];
  /** replies 是已有账号兜底配置索引。 */
  replies: Record<string, DefaultReply>;
  /** items 是已同步商品，用于显示账号隔离的商品标题。 */
  items: Item[];
  /** actions 管理商品列表和两类配置操作，组件不直接访问 HTTP。 */
  actions: DefaultReplyActionsState;
}

/** DefaultRepliesPanel 分开展示商品覆盖和账号兜底，明确商品每次触发、不继承账号开关的既有语义。 */
export default function DefaultRepliesPanel({ selectedAccountId,accounts,replies,items,actions }: DefaultRepliesPanelProps) {
  /** loadItemDefaultReplies 是稳定的列表刷新动作，只在挂载或账号切换时触发。 */
  const { loadItemDefaultReplies } = actions;
  useEffect(/* 当前副作用加载默认回复页签的商品列表；Hook 代次负责取消过期响应。 */ () => { void loadItemDefaultReplies(); }, [loadItemDefaultReplies, selectedAccountId]);
  /** visibleAccounts 是当前筛选下可展示的账号集合。 */
  const visibleAccounts = accounts.filter(/* account 是当前用户的账号摘要。 */ account => !selectedAccountId || account.id === selectedAccountId);
  /** visibleItemReplies 是当前筛选下的商品配置，不混入其他账号的同名商品。 */
  const visibleItemReplies = actions.itemDefaultReplies.filter(/* reply 是单个账号商品覆盖配置。 */ reply => !selectedAccountId || reply.cookie_id === selectedAccountId);
  return <section className="space-y-6">
    <div className="flex items-start gap-2 text-sm text-blue-700 bg-blue-50 px-4 py-3 rounded-xl">
      <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
      <p>默认回复只处理买家用户消息。关键词未命中且 AI 未接管后，先使用商品专属图文，再使用账号兜底；不会把两者拼接发送。</p>
    </div>
    <div className="bg-white rounded-xl border border-gray-100 p-6 shadow-sm space-y-4">
      <div className="flex flex-wrap justify-between gap-3 items-center">
        <div><h3 className="font-extrabold text-lg">商品默认回复</h3><p className="text-xs text-gray-500 mt-1">独立生效，每次触发；未配置或图文都为空时使用账号兜底。</p></div>
        <div className="flex gap-2">
          <button type="button" disabled={actions.itemRepliesLoading} onClick={/* 当前回调显式刷新商品配置，失败信息保留在区域中。 */ () => void loadItemDefaultReplies()} className="px-3 py-2 text-sm rounded-xl bg-gray-100 disabled:opacity-50" aria-label="刷新商品默认回复"><RefreshCw className={`w-4 h-4 ${actions.itemRepliesLoading ? 'animate-spin' : ''}`} /></button>
          <button type="button" disabled={!selectedAccountId || actions.defaultReplyMutationBusy} onClick={/* 当前回调为选中账号创建商品配置草稿。 */ () => void actions.openItemDefaultReplyModal(selectedAccountId)} className="ios-btn-primary px-4 py-2 rounded-xl text-sm font-bold flex items-center gap-2 disabled:opacity-50"><Plus className="w-4 h-4" />新增商品默认回复</button>
        </div>
      </div>
      {!selectedAccountId && <p className="text-xs text-gray-500">新增配置前请在页面上方选择账号。</p>}
      {actions.itemRepliesError && <p role="alert" className="text-sm text-red-700 bg-red-50 rounded-xl p-3">{actions.itemRepliesError}</p>}
      {actions.itemRepliesLoading && <p role="status" className="text-sm text-gray-500">正在加载商品默认回复…</p>}
      {visibleItemReplies.map(/* reply 是当前账号筛选下的一条商品默认回复。 */ reply => {
        /** item 是同账号的本地商品，标题缺失时使用稳定商品标识。 */
        const item = items.find(/* candidate 是按账号和商品双重定位的候选。 */ candidate => candidate.cookie_id === reply.cookie_id && candidate.item_id === reply.item_id);
        /** account 是该商品配置所属的账号摘要。 */
        const account = accounts.find(/* candidate 是当前用户的账号候选。 */ candidate => candidate.id === reply.cookie_id);
        /** hasContent 表示本条配置是否实际覆盖账号兜底。 */
        const hasContent = Boolean(reply.reply_content || reply.reply_image_url || reply.reply_image_path);
        return <div key={`${reply.cookie_id}:${reply.item_id}`} className="flex flex-col md:flex-row md:items-center justify-between p-4 rounded-2xl border border-gray-100 bg-surface-subtle gap-3">
          <div className="min-w-0 space-y-1">
            <h4 className="font-bold text-gray-900 break-words">{item?.item_title || reply.item_id}</h4>
            <p className="text-xs text-gray-500">{account ? accountLabel(account) : reply.cookie_id} · {reply.item_id} · {hasContent ? '商品专属' : '继承账号兜底'}{hasContent && reply.reply_once ? ' · 单个会话只回复一次' : ''}</p>
            <p className="text-sm text-gray-600 line-clamp-2">{reply.reply_content || '无文字回复'}</p>
            {(reply.reply_image_path || reply.reply_image_url) && <p className="text-xs text-gray-500 break-all">{reply.reply_image_path ? `本地图片：${reply.reply_image_path}` : `图片 URL：${reply.reply_image_url}`}</p>}
          </div>
          <div className="flex gap-2 shrink-0">
            <button type="button" disabled={actions.defaultReplyMutationBusy} aria-label={`编辑商品 ${reply.item_id} 默认回复`} onClick={/* 当前回调重新读取该商品配置再打开编辑器。 */ () => void actions.openItemDefaultReplyModal(reply.cookie_id, reply.item_id)} className="p-2 text-gray-500 hover:bg-gray-100 rounded-xl"><Edit className="w-4 h-4" /></button>
            <button type="button" disabled={actions.defaultReplyMutationBusy} aria-label={`删除商品 ${reply.item_id} 默认回复`} onClick={/* 当前回调确认删除覆盖并恢复账号兜底。 */ () => void actions.handleDeleteItemDefaultReply(reply.cookie_id, reply.item_id)} className="p-2 text-gray-500 hover:text-red-600 hover:bg-red-50 rounded-xl"><Trash2 className="w-4 h-4" /></button>
          </div>
        </div>;
      })}
      {!actions.itemRepliesLoading && !actions.itemRepliesError && visibleItemReplies.length === 0 && <p className="text-sm text-gray-400 text-center py-8">暂无商品专属默认回复，当前使用账号兜底。</p>}
    </div>
    <div className="bg-white rounded-xl border border-gray-100 p-6 shadow-sm space-y-4">
      <div><h3 className="font-extrabold text-lg">账号默认回复（兜底）</h3><p className="text-xs text-gray-500 mt-1">只在商品没有有效专属配置时使用，开关和“只回复一次”仅控制本账号兜底。</p></div>
      {visibleAccounts.map(/* account 是当前账号筛选下的一条账号摘要。 */ account => {
        /** reply 是该账号已有的兜底配置，缺失时展示未配置。 */
        const reply = replies[account.id];
        /** enabled 只表示账号兜底状态，不能据此禁用商品覆盖。 */
        const enabled = Boolean(reply?.enabled);
        return <div key={account.id} className={`flex flex-col md:flex-row md:items-center justify-between p-5 rounded-2xl border gap-4 ${enabled ? 'border-purple-100 bg-purple-50/50' : 'border-gray-100 bg-surface-subtle'}`}>
          <div className="flex items-center gap-4 min-w-0">
            <div className={`w-12 h-12 rounded-2xl shrink-0 flex items-center justify-center ${enabled ? 'bg-purple-600 text-white' : 'bg-gray-200 text-gray-400'}`}><Bot className="w-5 h-5" /></div>
            <div className="min-w-0">
              <div className="flex items-center gap-2 flex-wrap mb-2"><h4 className="font-bold text-gray-900 text-lg">{accountLabel(account)}</h4><span className="text-xs text-gray-500">{enabled ? '已启用' : reply ? '已停用' : '未配置'}</span>{reply?.reply_once && <span className="text-xs text-purple-700">只回复一次</span>}</div>
              <p className="text-sm text-gray-600 line-clamp-2">{reply?.reply_content || (reply?.reply_image_path ? `本地图片：${reply.reply_image_path}` : reply?.reply_image_url) || '未配置回复内容'}</p>
            </div>
          </div>
          <div className="flex gap-2 shrink-0 items-center">
            <button type="button" disabled={actions.defaultReplyMutationBusy} aria-label={`编辑账号 ${account.id} 默认回复`} onClick={/* 当前回调读取该账号的兜底配置。 */ () => void actions.openDefaultReplyModal(account.id)} className="p-2 text-gray-500 hover:bg-gray-100 rounded-xl"><Edit className="w-4 h-4" /></button>
            {reply && <>
              <button type="button" disabled={actions.defaultReplyMutationBusy} onClick={/* 当前回调要求确认后清空账号的会话去重记录。 */ () => void actions.handleClearDefaultReplyRecords(account.id)} className="px-3 py-2 text-xs font-bold text-blue-600 hover:bg-blue-50 rounded-xl">清空记录</button>
              <button type="button" disabled={actions.defaultReplyMutationBusy} aria-label={`删除账号 ${account.id} 默认回复`} onClick={/* 当前回调删除账号兜底而不影响商品配置。 */ () => void actions.handleDeleteDefaultReply(account.id)} className="p-2 text-gray-500 hover:text-red-600 hover:bg-red-50 rounded-xl"><Trash2 className="w-4 h-4" /></button>
            </>}
          </div>
        </div>;
      })}
      {visibleAccounts.length === 0 && <p className="text-center text-sm text-gray-400 py-8">暂无账号</p>}
    </div>
  </section>;
}
