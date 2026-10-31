import { useCallback, useEffect, useRef, useState } from 'react';
import { clearItemGreetingRecords, deleteItemGreeting, getItemGreetings, getItems, saveItemGreeting } from './api';
import type { Item, ItemGreetingRule } from './api';
import { finishRuleSubmission, idleRuleSubmitState, isCurrentRequest, nextRequestGeneration, startRuleSubmission } from './interactionState';
import type { RuleSubmitState } from './interactionState';

// emptyItemGreetingDraft 创建指定账号下的空白首次回复草稿。
export const emptyItemGreetingDraft = (cookieID: string): ItemGreetingRule => ({
  id: 0,
  cookie_id: cookieID,
  name: '',
  enabled: true,
  reply_content: '',
  reply_image_url: '',
  reply_library_image_id: 0,
  item_ids: [],
});

// ItemGreetingErrorLike 描述保存失败时可能携带的统一错误信封字段。
interface ItemGreetingErrorLike {
  // status 是 HTTP 状态码，409 表示商品冲突。
  status?: number;
  // details 是服务端附带的结构化信息，冲突时包含 item_ids。
  details?: Record<string, unknown>;
  // message 是服务端返回的用户可读错误消息。
  message?: string;
}

// itemGreetingErrorMessage 把保存失败转换为用户可读文案；商品冲突时附带冲突商品标识。
export const itemGreetingErrorMessage = (error: unknown): string => {
  // status、details、message 是统一错误信封中的状态码、结构化详情和错误消息。
  const { status, details, message } = error as ItemGreetingErrorLike;
  if (status === 409 && Array.isArray(details?.item_ids)) {
    return `部分商品已被其他首次回复规则使用：${(details.item_ids as unknown[]).join('、')}`;
  }
  return message || '操作失败';
};

// ItemGreetingToast 是一次操作的短提示。
export interface ItemGreetingToast {
  // type 决定提示的语义颜色。
  type: 'success' | 'error';
  // text 是提示文案，不包含任何凭证。
  text: string;
}

// useItemGreetings 管理商品首次回复规则的列表、编辑草稿和写操作。
// accountID 为空表示查看全部账号；列表和编辑器商品请求都带最新代次，过期响应不会覆盖当前状态。
export const useItemGreetings = (accountID: string) => {
  // rules 是当前账号筛选下的规则列表。
  const [rules, setRules] = useState<ItemGreetingRule[]>([]);
  // loading 表示列表请求是否在途。
  const [loading, setLoading] = useState(false);
  // loadError 保存最近一次列表加载失败文案，成功后清空。
  const [loadError, setLoadError] = useState('');
  // draft 是正在编辑的规则草稿；null 表示编辑器关闭。
  const [draft, setDraft] = useState<ItemGreetingRule | null>(null);
  // draftItems 是草稿所属账号的商品候选。
  const [draftItems, setDraftItems] = useState<Item[]>([]);
  // submitState 阻断重复提交并记录最近结果。
  const [submitState, setSubmitState] = useState<RuleSubmitState>(idleRuleSubmitState);
  // toast 是最近一次操作反馈。
  const [toast, setToast] = useState<ItemGreetingToast | null>(null);
  // listGeneration、itemsGeneration 保存两类请求的最新代次，用于丢弃过期响应。
  const listGeneration = useRef(0);
  const itemsGeneration = useRef(0);
  // accountRef 保存最新账号筛选，供异步回调判断响应是否仍属于当前筛选。
  const accountRef = useRef(accountID);
  accountRef.current = accountID;
  // submittingRef 同步记录是否已有写操作在途。
  const submittingRef = useRef(false);
  // listAbort、itemsAbort 取消被新请求取代的在途请求。
  const listAbort = useRef<AbortController | null>(null);
  const itemsAbort = useRef<AbortController | null>(null);

  // reload 重新读取当前账号筛选下的规则。
  const reload = useCallback(/* 当前回调按最新代次加载规则列表。 */ async () => {
    // generation、key 标识本次请求及其账号筛选。
    const generation = nextRequestGeneration(listGeneration.current);
    listGeneration.current = generation;
    // key 是本次请求对应的账号筛选，用于响应返回时核对是否仍然有效。
    const key = accountRef.current;
    listAbort.current?.abort();
    // controller 用于在新请求或卸载时取消本次列表请求。
    const controller = new AbortController();
    listAbort.current = controller;
    setLoading(true);
    try {
      // next 是服务端返回的规则列表。
      const next = await getItemGreetings(key, { signal: controller.signal });
      if (!isCurrentRequest(generation, listGeneration.current, key, accountRef.current)) return;
      setRules(next);
      setLoadError('');
    } catch (/* error 表示列表加载失败的原因。 */ error) {
      if (!isCurrentRequest(generation, listGeneration.current, key, accountRef.current)) return;
      setLoadError(itemGreetingErrorMessage(error));
    } finally {
      if (generation === listGeneration.current) setLoading(false);
    }
  }, []);

  useEffect(/* 当前副作用在账号筛选变化时重新加载规则，并在卸载时取消在途请求。 */ () => {
    void reload();
    return /* 当前回调使卸载后到达的响应失效并取消请求。 */ () => {
      listGeneration.current = nextRequestGeneration(listGeneration.current);
      listAbort.current?.abort();
    };
  }, [accountID, reload]);

  useEffect(/* 当前副作用在卸载时取消商品候选请求。 */ () => /* 当前回调使在途商品请求失效并取消。 */ () => {
    itemsGeneration.current = nextRequestGeneration(itemsGeneration.current);
    itemsAbort.current?.abort();
  }, []);

  // loadDraftItems 读取草稿所属账号的商品候选；失败时保留空候选并提示。
  const loadDraftItems = useCallback(/* 当前回调按最新代次加载编辑器商品候选。 */ async (cookieID: string) => {
    // generation 标识本次商品候选请求，响应返回时用于丢弃过期结果。
    const generation = nextRequestGeneration(itemsGeneration.current);
    itemsGeneration.current = generation;
    itemsAbort.current?.abort();
    // controller 用于取消本次商品候选请求。
    const controller = new AbortController();
    itemsAbort.current = controller;
    try {
      // list 是该账号已同步的商品。
      const list = await getItems(cookieID, { signal: controller.signal });
      if (generation === itemsGeneration.current) setDraftItems(list);
    } catch {
      if (generation === itemsGeneration.current) {
        setDraftItems([]);
        setToast({ type: 'error', text: '加载商品列表失败' });
      }
    }
  }, []);

  // openCreate 为指定账号打开空白草稿。
  const openCreate = useCallback(/* 当前回调打开新建编辑器并加载商品候选。 */ (cookieID: string) => {
    setDraft(emptyItemGreetingDraft(cookieID));
    setDraftItems([]);
    void loadDraftItems(cookieID);
  }, [loadDraftItems]);

  // openEdit 打开已有规则的编辑器。
  const openEdit = useCallback(/* 当前回调打开编辑器并加载规则所属账号的商品候选。 */ (rule: ItemGreetingRule) => {
    setDraft({ ...rule, item_ids: [...rule.item_ids] });
    setDraftItems([]);
    void loadDraftItems(rule.cookie_id);
  }, [loadDraftItems]);

  // closeEditor 关闭编辑器并使在途商品请求失效，避免旧账号商品回填。
  const closeEditor = useCallback(/* 当前回调清空草稿并取消商品候选请求。 */ () => {
    itemsGeneration.current = nextRequestGeneration(itemsGeneration.current);
    itemsAbort.current?.abort();
    setDraft(null);
    setDraftItems([]);
  }, []);

  // run 执行一次写操作：阻断重复提交，成功后刷新列表，失败时提示。
  const run = useCallback(/* 当前回调串行化写操作并统一收口提示。 */ async (action: () => Promise<unknown>, successText: string): Promise<boolean> => {
    // 同步标志位阻断同一事件循环内的重复点击；React 状态更新是异步的，不能用来做判断。
    if (submittingRef.current) return false;
    submittingRef.current = true;
    setSubmitState(startRuleSubmission);
    // succeeded 记录写操作是否成功，用于收口提交状态。
    let succeeded = false;
    try {
      await action();
      succeeded = true;
      setToast({ type: 'success', text: successText });
      await reload();
    } catch (/* error 表示写操作失败的原因。 */ error) {
      setToast({ type: 'error', text: itemGreetingErrorMessage(error) });
    } finally {
      submittingRef.current = false;
      setSubmitState(/* current 是提交状态。 */ current => finishRuleSubmission(current, succeeded));
    }
    return succeeded;
  }, [reload]);

  // save 校验并保存当前草稿，成功后关闭编辑器。
  const save = useCallback(/* 当前回调校验草稿后保存。 */ async () => {
    if (!draft) return;
    if (draft.item_ids.length === 0) {
      setToast({ type: 'error', text: '请至少选择一个商品' });
      return;
    }
    if (!draft.reply_content.trim() && !draft.reply_image_url.trim() && !draft.reply_library_image_id) {
      setToast({ type: 'error', text: '回复文字和图片至少填写一项' });
      return;
    }
    if (await run(/* 当前回调保存草稿。 */ () => saveItemGreeting(draft), '保存成功')) closeEditor();
  }, [closeEditor, draft, run]);

  // remove 删除一条规则。
  const remove = useCallback(/* 当前回调删除规则。 */ (id: number) => run(/* 当前回调调用删除接口。 */ () => deleteItemGreeting(id), '删除成功'), [run]);

  // toggle 切换规则启用状态。
  const toggle = useCallback(/* 当前回调保存翻转后的启用状态。 */ (rule: ItemGreetingRule) => run(/* 当前回调保存规则。 */ () => saveItemGreeting({ ...rule, enabled: !rule.enabled }), rule.enabled ? '已停用' : '已启用'), [run]);

  // clearRecords 清空账号的发送记录。
  const clearRecords = useCallback(/* 当前回调清空发送记录。 */ (cookieID: string) => run(/* 当前回调调用清理接口。 */ () => clearItemGreetingRecords(cookieID), '已清空发送记录'), [run]);

  // dismissToast 清除提示。
  const dismissToast = useCallback(/* 当前回调清除提示。 */ () => setToast(null), []);

  return { rules, loading, loadError, draft, setDraft, draftItems, submitState, toast, dismissToast, reload, openCreate, openEdit, closeEditor, save, remove, toggle, clearRecords };
};
