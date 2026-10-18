import { useCallback,useEffect,useRef,useState,type Dispatch,type SetStateAction } from 'react';
import { clearDefaultReplyRecords,deleteDefaultReply,deleteItemDefaultReply,getDefaultReply,getItemDefaultReplies,getItemDefaultReply,updateDefaultReply,updateItemDefaultReply } from './api';
import { createDefaultReplyForm,defaultReplyImageFields,validateDefaultReplyForm } from './defaultReplyState';
import { idleRuleSubmitState,type RuleSubmitState } from './interactionState';
import type { ItemDefaultReply } from './models';
import type { DefaultReplyForm } from './types';

/** DefaultReplyActionsOptions 提供当前账号、账号列表刷新和由外层拥有的轻提示。 */
interface DefaultReplyActionsOptions {
  /** selectedAccountId 是规则页面当前账号筛选，切换后使旧弹窗请求失效。 */
  selectedAccountId: string;
  /** loadDefaultReplies 刷新已有账号兜底列表，不控制商品配置列表。 */
  loadDefaultReplies: () => Promise<void>;
  /** notify 展示当前范围的操作结果，不展示已切换范围的迟到消息。 */
  notify: (type: 'success' | 'error', text: string) => void;
}

/** useDefaultReplyActions 拥有账号／商品默认回复的表单和请求代次，关闭、切换和卸载均隔离迟到结果。 */
export const useDefaultReplyActions = ({ selectedAccountId,loadDefaultReplies,notify }: DefaultReplyActionsOptions) => {
  /** showDefaultModal/setModalVisible 管理弹窗可见性，业务动作只能通过代次包装器关闭。 */
  const [showDefaultModal, setModalVisible] = useState(false);
  /** defaultForm/setDefaultForm 保存正在编辑的草稿，不复用服务端列表对象。 */
  const [defaultForm, setDefaultForm] = useState<DefaultReplyForm>(() => createDefaultReplyForm(''));
  /** defaultReplyLoading/setDefaultReplyLoading 区分读取中和可编辑状态。 */
  const [defaultReplyLoading, setDefaultReplyLoading] = useState(false);
  /** defaultReplyLoadFailed/setDefaultReplyLoadFailed 阻止加载失败后用空草稿覆盖真实配置。 */
  const [defaultReplyLoadFailed, setDefaultReplyLoadFailed] = useState(false);
  /** defaultReplyError/setDefaultReplyError 保存当前弹窗的读取或保存错误。 */
  const [defaultReplyError, setDefaultReplyError] = useState('');
  /** defaultReplySubmitState/setDefaultReplySubmitState 保留规则动作接口的提交状态。 */
  const [defaultReplySubmitState, setDefaultReplySubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  /** itemDefaultReplies/setItemDefaultReplies 保存当前用户的商品配置列表，展示时仍按账号过滤。 */
  const [itemDefaultReplies, setItemDefaultReplies] = useState<ItemDefaultReply[]>([]);
  /** itemRepliesLoading/setItemRepliesLoading 表示商品配置列表正在刷新。 */
  const [itemRepliesLoading, setItemRepliesLoading] = useState(false);
  /** itemRepliesError/setItemRepliesError 保留列表读取失败，不伪装成成功空列表。 */
  const [itemRepliesError, setItemRepliesError] = useState('');
  /** defaultReplyMutationBusy/setDefaultReplyMutationBusy 串行化列表删除与记录清理操作。 */
  const [defaultReplyMutationBusy, setDefaultReplyMutationBusy] = useState(false);
  /** generation 只属于当前编辑器；每次选账号、商品、关闭或卸载都会递增。 */
  const generation = useRef(0);
  /** listGeneration 隔离并发列表刷新和卸载后的响应。 */
  const listGeneration = useRef(0);
  /** mutationGeneration 防止旧删除请求清除新操作的忙碌状态。 */
  const mutationGeneration = useRef(0);
  /** modalVisible 同步保存弹窗状态，使同一事件内的关闭立即使请求失效。 */
  const modalVisible = useRef(false);
  /** submitting 拥有实际 PUT 的生命周期；关闭或切换编辑器只能撤销回调，不能释放在途写入锁。 */
  const submitting = useRef(false);
  /** mounted 阻止异步写入收口更新已卸载的页面；账号切换仍复用同一锁。 */
  const mounted = useRef(true);
  /** mutating 同步保护列表操作，不依赖 React 异步状态提交。 */
  const mutating = useRef(false);

  /** setShowDefaultModal 包装旧公开 setter；关闭操作同时撤销该弹窗的读取和保存回调。 */
  const setShowDefaultModal: Dispatch<SetStateAction<boolean>> = useCallback(/* value 是直接可见性或兼容 React setter 的更新器。 */ value => {
    // next 是本次更新后的可见性，不从可能过期的渲染闭包读取。
    const next = typeof value === 'function' ? value(modalVisible.current) : value;
    modalVisible.current = next;
    if (!next) {
      generation.current += 1;
      setDefaultReplyLoading(false);
      if (!submitting.current) setDefaultReplySubmitState(idleRuleSubmitState);
    }
    setModalVisible(next);
  }, []);

  useEffect(/* 账号切换仅撤销界面反馈，保存和删除锁由实际请求完成后释放。 */ () => {
    mounted.current = true;
    setShowDefaultModal(false);
    mutationGeneration.current += 1;
    return /* 当前 cleanup 阻止已离开的账号和已卸载组件接收异步结果。 */ () => {
      mounted.current = false;
      generation.current += 1;
      listGeneration.current += 1;
      mutationGeneration.current += 1;
    };
  }, [selectedAccountId, setShowDefaultModal]);

  /** loadItemDefaultReplies 刷新全用户商品配置列表，只提交最后一次请求，错误留在页面。 */
  const loadItemDefaultReplies = useCallback(/* 当前回调由默认回复页签或成功写入显式调用。 */ async () => {
    // requestID 是本轮列表刷新的最新代次。
    const requestID = ++listGeneration.current;
    setItemRepliesLoading(true);
    setItemRepliesError('');
    try {
      // replies 是后端已按本地用户隔离的配置集合。
      const replies = await getItemDefaultReplies();
      if (requestID === listGeneration.current) setItemDefaultReplies(replies);
    } catch (/* error 是当前列表请求的失败信息。 */ error) {
      if (requestID === listGeneration.current) setItemRepliesError('加载商品默认回复失败：' + (error as Error).message);
    } finally {
      if (requestID === listGeneration.current) setItemRepliesLoading(false);
    }
  }, []);

  /** openEditor 立即清空旧草稿并读取指定目标；商品未选择时只展示空表单而不发出无效请求。 */
  const openEditor = useCallback(/* cookieID/itemID/scope 确定本次编辑器唯一的配置目标。 */ async (cookieID: string, itemID: string, scope: 'account' | 'item') => {
    if (!cookieID) return notify('error', '请先选择账号');
    // requestID 隔离连续选择账号或商品时的旧详情响应。
    const requestID = ++generation.current;
    setDefaultForm(createDefaultReplyForm(cookieID, itemID, scope));
    setDefaultReplyError('');
    setDefaultReplyLoadFailed(false);
    if (!submitting.current) setDefaultReplySubmitState(idleRuleSubmitState);
    setDefaultReplyLoading(scope === 'account' || Boolean(itemID));
    setShowDefaultModal(true);
    if (scope === 'item' && !itemID) return;
    try {
      // reply 是当前编辑目标的服务端配置，不能合并到其他目标的草稿。
      const reply = scope === 'item' ? await getItemDefaultReply(cookieID, itemID) : await getDefaultReply(cookieID);
      if (requestID === generation.current) setDefaultForm(createDefaultReplyForm(cookieID, itemID, scope, reply));
    } catch (/* error 是配置读取失败，保留不可保存状态以免覆盖真实数据。 */ error) {
      if (requestID === generation.current) {
        setDefaultReplyLoadFailed(true);
        setDefaultReplyError('加载默认回复失败：' + (error as Error).message);
      }
    } finally {
      if (requestID === generation.current) setDefaultReplyLoading(false);
    }
  }, [notify, setShowDefaultModal]);

  /** openDefaultReplyModal 打开账号兜底，不携带之前商品的表单内容。 */
  const openDefaultReplyModal = useCallback(/* cookieID 缺省使用页面当前账号。 */ (cookieID = selectedAccountId) => openEditor(cookieID, '', 'account'), [openEditor, selectedAccountId]);
  /** openItemDefaultReplyModal 打开指定商品，空 itemID 用于新增配置时选择本地商品。 */
  const openItemDefaultReplyModal = useCallback(/* cookieID/itemID 是用户选择的账号和商品。 */ (cookieID = selectedAccountId, itemID = '') => openEditor(cookieID, itemID, 'item'), [openEditor, selectedAccountId]);

  /** handleSaveDefaultReply 校验当前来源并保存，失败保留草稿，迟到成功不得关闭新编辑器。 */
  const handleSaveDefaultReply = useCallback(/* 当前回调仅由用户保存动作触发。 */ async () => {
    if (submitting.current || mutating.current || defaultReplyLoading || defaultReplyLoadFailed) return;
    // validation 是当前草稿的第一项输入错误，商品空图文则合法继承账号。
    const validation = validateDefaultReplyForm(defaultForm);
    if (validation) {
      setDefaultReplyError(validation);
      return;
    }
    // requestID 是本次写入所属编辑器的代次。
    const requestID = generation.current;
    // images 明确包含两种图片字段，切换来源时不遗留旧值。
    const images = defaultReplyImageFields(defaultForm);
    submitting.current = true;
    setDefaultReplyError('');
    setDefaultReplySubmitState({ submitting: true, result: 'idle' });
    try {
      if (defaultForm.scope === 'item') {
        await updateItemDefaultReply(defaultForm.cookie_id, defaultForm.item_id || '', { reply_content: defaultForm.reply_content, reply_once: defaultForm.reply_once, ...images });
      } else {
        await updateDefaultReply(defaultForm.cookie_id, { enabled: defaultForm.enabled, reply_once: defaultForm.reply_once, reply_content: defaultForm.reply_content, ...images });
      }
      if (requestID !== generation.current) return;
      // 写入已成功，刷新失败只显示读取错误，不引导用户重复提交。
      setShowDefaultModal(false);
      setDefaultReplySubmitState({ submitting: true, result: 'success' });
      // refreshID 标识关闭成功编辑器后的界面，后续再打开或切换时不展示旧刷新错误。
      const refreshID = generation.current;
      notify('success', '保存成功');
      if (defaultForm.scope === 'item') await loadItemDefaultReplies();
      else await loadDefaultReplies().catch(/* error 是成功写入后的列表刷新失败。 */ error => {
        if (refreshID === generation.current) notify('error', '已保存，但刷新列表失败：' + (error as Error).message);
      });
    } catch (/* error 是当前写入失败，只有原编辑器仍有效时展示。 */ error) {
      if (requestID === generation.current) setDefaultReplyError('保存失败：' + (error as Error).message);
    } finally {
      submitting.current = false;
      if (mounted.current) setDefaultReplySubmitState(/* current 保留新编辑器状态或已成功结果，只释放实际写入锁。 */ current => ({
        submitting: false, result: requestID === generation.current ? 'failure' : current.result,
      }));
    }
  }, [defaultForm, defaultReplyLoadFailed, defaultReplyLoading, loadDefaultReplies, loadItemDefaultReplies, notify, setShowDefaultModal]);

  /** runListMutation 串行执行用户确认后的列表副作用，账号切换后丢弃反馈和忙碌状态更新。 */
  const runListMutation = useCallback(/* operation 执行一次明确副作用；refresh 刷新对应列表；message 是成功文案。 */ async (operation: () => Promise<unknown>, refresh: (() => Promise<void>) | undefined, message: string) => {
    if (mutating.current || submitting.current) return;
    // requestID 标识当前列表操作，防止旧 finally 清理新操作状态。
    const requestID = ++mutationGeneration.current;
    mutating.current = true;
    setDefaultReplyMutationBusy(true);
    try {
      await operation();
      if (requestID !== mutationGeneration.current) return;
      notify('success', message);
      if (refresh) await refresh().catch(/* error 是已完成副作用后的列表刷新失败。 */ error => {
        if (requestID === mutationGeneration.current) notify('error', '操作已完成，但刷新失败：' + (error as Error).message);
      });
    } catch (/* error 是列表副作用失败，保留原列表和配置。 */ error) {
      if (requestID === mutationGeneration.current) notify('error', '操作失败：' + (error as Error).message);
    } finally {
      mutating.current = false;
      if (mounted.current) setDefaultReplyMutationBusy(false);
    }
  }, [notify]);

  /** handleDeleteDefaultReply 删除账号兜底，不改变商品专属配置。 */
  const handleDeleteDefaultReply = useCallback(/* cookieID 是用户确认删除的账号。 */ async (cookieID: string) => {
    if (!confirm('确定删除该账号默认回复吗？商品专属回复不受影响。')) return;
    await runListMutation(/* 当前回调删除确认的账号配置。 */ () => deleteDefaultReply(cookieID), loadDefaultReplies, '删除成功');
  }, [loadDefaultReplies, runListMutation]);
  /** handleDeleteItemDefaultReply 删除商品覆盖，下次触发重新使用账号兜底。 */
  const handleDeleteItemDefaultReply = useCallback(/* cookieID/itemID 唯一定位用户确认恢复继承的商品。 */ async (cookieID: string, itemID: string) => {
    if (!confirm('确定删除该商品默认回复并恢复使用账号兜底吗？')) return;
    await runListMutation(/* 当前回调删除确认的商品配置。 */ () => deleteItemDefaultReply(cookieID, itemID), loadItemDefaultReplies, '已恢复账号兜底');
  }, [loadItemDefaultReplies, runListMutation]);
  /** handleClearDefaultReplyRecords 清理账号级会话去重记录，商品回复仍保持每次触发。 */
  const handleClearDefaultReplyRecords = useCallback(/* cookieID 是用户确认清空会话记录的账号。 */ async (cookieID: string) => {
    if (!confirm('确定清空该账号的默认回复记录吗？清空后可重新对所有会话使用“只回复一次”。')) return;
    await runListMutation(/* 当前回调清理确认的账号会话记录。 */ () => clearDefaultReplyRecords(cookieID), undefined, '清空成功');
  }, [runListMutation]);

  return {
    showDefaultModal, setShowDefaultModal, defaultForm, setDefaultForm, defaultReplySubmitState,
    defaultReplyLoading, defaultReplyLoadFailed, defaultReplyError,
    defaultReplyMutationBusy: defaultReplyMutationBusy || defaultReplySubmitState.submitting,
    itemDefaultReplies, itemRepliesLoading, itemRepliesError, loadItemDefaultReplies,
    openDefaultReplyModal, openItemDefaultReplyModal, handleSaveDefaultReply,
    handleDeleteDefaultReply, handleDeleteItemDefaultReply, handleClearDefaultReplyRecords,
  };
};

/** DefaultReplyActionsState 是页面与默认回复组件共享的动作边界，不暴露网络或 transport 类型。 */
export type DefaultReplyActionsState = ReturnType<typeof useDefaultReplyActions>;
