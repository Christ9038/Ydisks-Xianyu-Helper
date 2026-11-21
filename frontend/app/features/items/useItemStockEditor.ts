import { useCallback,useEffect,useRef,useState } from 'react';
import { getItemStock,isItemStockChangedError,itemErrorMessage,updateItemStock } from './api';
import type { Item,ItemStock } from './api';
import { applyStockUpdate,buildStockUpdate } from './stockInput';

/** UseItemStockEditorOptions 描述库存编辑状态机的输入。 */
export interface UseItemStockEditorOptions {
  /** item 是正在调整库存的商品；为空时不发起任何请求。 */
  item: Item | null;
  /** open 表示弹窗当前是否可见，只有打开时才会读取库存。 */
  open: boolean;
  /** onClose 关闭弹窗。 */
  onClose: () => void;
}

/** ItemStockEditor 是库存编辑弹窗使用的状态与动作集合。 */
export interface ItemStockEditor {
  /** stock 是最近一次读取（或修改成功后应用）的库存快照。 */
  stock: ItemStock | null;
  /** singleInput 是单规格库存输入框的文本草稿。 */
  singleInput: string;
  /** skuInputs 是多规格各行库存输入框的文本草稿，键为规格行标识。 */
  skuInputs: Record<string, string>;
  /** loading 表示正在读取库存。 */
  loading: boolean;
  /** loadFailed 表示读取失败，此时禁止提交。 */
  loadFailed: boolean;
  /** needsReload 表示库存已在平台侧变化，必须重新读取后才能再次提交。 */
  needsReload: boolean;
  /** saving 表示提交仍在进行，用于阻止重复提交。 */
  saving: boolean;
  /** errorMessage 是当前读取或提交失败的用户可见说明。 */
  errorMessage: string;
  /** successMessage 是提交成功后的用户可见说明。 */
  successMessage: string;
  /** setSingleInput 更新单规格库存草稿。 */
  setSingleInput: (value: string) => void;
  /** setSkuInput 更新某一规格行的库存草稿。 */
  setSkuInput: (key: string, value: string) => void;
  /** save 校验并提交当前草稿。 */
  save: () => Promise<void>;
  /** reload 由用户点击触发，重新读取库存。 */
  reload: () => void;
  /** close 取消在途请求并关闭弹窗。 */
  close: () => void;
}

/**
 * useItemStockEditor 管理单件商品的库存读取与修改：
 * 打开弹窗时读取一次；关闭、切换商品或卸载会取消在途请求，晚到的响应通过代次丢弃；
 * 提交只在用户点击时发生且同一时刻最多一个；库存已变化时不自动重读，要求用户点击重新加载。
 */
export const useItemStockEditor = ({ item, open, onClose }: UseItemStockEditorOptions): ItemStockEditor => {
  // [stock, setStock] 保存服务端库存快照，作为并发校验的“原库存”。
  const [stock, setStock] = useState<ItemStock | null>(null);
  // [singleInput, setSingleInput] 保存单规格库存输入草稿。
  const [singleInput, setSingleInput] = useState('');
  // [skuInputs, setSkuInputs] 保存多规格各行库存输入草稿。
  const [skuInputs, setSkuInputs] = useState<Record<string, string>>({});
  // [loading, setLoading] 表示库存读取是否仍在进行。
  const [loading, setLoading] = useState(false);
  // [loadFailed, setLoadFailed] 表示读取失败，阻止在没有原库存的情况下提交。
  const [loadFailed, setLoadFailed] = useState(false);
  // [needsReload, setNeedsReload] 表示平台库存已变化，必须重读后才能提交。
  const [needsReload, setNeedsReload] = useState(false);
  // [saving, setSaving] 表示提交是否仍在进行。
  const [saving, setSaving] = useState(false);
  // [errorMessage, setErrorMessage] 保存读取或提交失败的说明。
  const [errorMessage, setErrorMessage] = useState('');
  // [successMessage, setSuccessMessage] 保存提交成功后的说明。
  const [successMessage, setSuccessMessage] = useState('');
  // [loadRevision, setLoadRevision] 由用户点击重新加载时递增，触发新的读取。
  const [loadRevision, setLoadRevision] = useState(0);
  // requestGeneration 标识当前请求代次，旧商品或已关闭弹窗的响应不得写入状态。
  const requestGeneration = useRef(0);
  // loadController 保存当前读取请求的取消控制器。
  const loadController = useRef<AbortController | null>(null);
  // saveController 保存当前提交请求的取消控制器。
  const saveController = useRef<AbortController | null>(null);

  // cancelRequests 取消当前持有的读取与提交请求。
  const cancelRequests = useCallback(/* cancelRequestsCallback 释放旧商品或已关闭弹窗的网络请求。 */ () => {
    loadController.current?.abort();
    saveController.current?.abort();
    loadController.current = null;
    saveController.current = null;
  }, []);

  useEffect(/* stockLoadEffect 在弹窗打开、商品切换或用户点击重新加载时读取一次库存。 */ () => {
    if (!open || !item) return undefined;
    cancelRequests();
    // generation 是本次读取的单调递增代次。
    const generation = ++requestGeneration.current;
    // controller 允许关闭弹窗或切换商品时取消本次读取。
    const controller = new AbortController();
    loadController.current = controller;
    setStock(null);
    setSingleInput('');
    setSkuInputs({});
    setErrorMessage('');
    setSuccessMessage('');
    setLoadFailed(false);
    setNeedsReload(false);
    setSaving(false);
    setLoading(true);
    void getItemStock(item.cookie_id, item.item_id, { signal: controller.signal })
      .then(/* stockResponseHandler 仅接收当前商品仍有效的读取结果。 */ response => {
        if (generation !== requestGeneration.current || controller.signal.aborted) return;
        setStock(response);
        setSingleInput(String(response.quantity));
        setSkuInputs(Object.fromEntries(response.skus.map(/* sku 是一行规格，草稿初始为当前库存。 */ sku => [sku.key, String(sku.quantity)])));
      })
      .catch(/* stockLoadErrorHandler 忽略主动取消，并展示仍有效的读取失败。 */ error => {
        if (generation !== requestGeneration.current || controller.signal.aborted) return;
        setLoadFailed(true);
        setErrorMessage(itemErrorMessage(error, '商品库存读取失败，请重试。'));
      })
      .finally(/* stockLoadFinallyHandler 只结束当前代次的读取状态。 */ () => {
        if (generation === requestGeneration.current && !controller.signal.aborted) setLoading(false);
      });
    return /* stockLoadCleanup 取消离开当前商品后仍在执行的读取或提交。 */ () => {
      controller.abort();
      saveController.current?.abort();
    };
  }, [cancelRequests, item, loadRevision, open]);

  // close 取消在途请求、让晚到响应失效并关闭弹窗。
  const close = useCallback(/* closeCallback 关闭弹窗并阻止晚到响应写入状态。 */ () => {
    requestGeneration.current += 1;
    cancelRequests();
    setLoading(false);
    setSaving(false);
    onClose();
  }, [cancelRequests, onClose]);

  // save 校验当前草稿并提交；提交期间重复点击会被忽略。
  const save = useCallback(/* saveCallback 在用户点击保存时提交库存修改。 */ async () => {
    if (!item || !stock || saving || loading || loadFailed || needsReload) return;
    // result 是草稿校验结果，失败时不发起任何请求。
    const result = buildStockUpdate(stock, singleInput, skuInputs);
    if (result.error !== undefined) {
      setErrorMessage(result.error);
      setSuccessMessage('');
      return;
    }
    saveController.current?.abort();
    // generation 是本次提交所属的请求代次。
    const generation = requestGeneration.current;
    // controller 允许关闭弹窗或切换商品时取消提交。
    const controller = new AbortController();
    saveController.current = controller;
    setSaving(true);
    setErrorMessage('');
    setSuccessMessage('');
    try {
      await updateItemStock(item.cookie_id, item.item_id, result.update, { signal: controller.signal });
      if (generation !== requestGeneration.current || controller.signal.aborted) return;
      setStock(/* previousStock 是提交前的快照，成功后应用新库存作为后续并发校验的原值。 */ previousStock => (previousStock ? applyStockUpdate(previousStock, result.update) : previousStock));
      setSuccessMessage('库存已更新。');
    } catch (error /* error 是提交失败的原因，草稿保持不变。 */) {
      if (generation !== requestGeneration.current || controller.signal.aborted) return;
      if (isItemStockChangedError(error)) setNeedsReload(true);
      setErrorMessage(itemErrorMessage(error, '库存修改失败，请稍后重试。'));
    } finally {
      if (generation === requestGeneration.current && !controller.signal.aborted) setSaving(false);
    }
  }, [item, loadFailed, loading, needsReload, saving, singleInput, skuInputs, stock]);

  // reload 由用户点击触发，重新读取一次库存。
  const reload = useCallback(/* reloadCallback 递增读取版本以重新触发读取。 */ () => setLoadRevision(/* previousRevision 保证连续点击都产生新的读取。 */ previousRevision => previousRevision + 1), []);

  // updateSingleInput 更新单规格草稿并清理旧提示。
  const updateSingleInput = useCallback(/* singleInputCallback 写入单规格库存草稿。 */ (value: string) => {
    setSingleInput(value);
    setErrorMessage('');
    setSuccessMessage('');
  }, []);

  // updateSkuInput 更新某一规格行草稿并清理旧提示。
  const updateSkuInput = useCallback(/* skuInputCallback 写入某一规格行的库存草稿。 */ (key: string, value: string) => {
    setSkuInputs(/* previousInputs 保留其他规格行尚未提交的草稿。 */ previousInputs => ({ ...previousInputs, [key]: value }));
    setErrorMessage('');
    setSuccessMessage('');
  }, []);

  return { stock, singleInput, skuInputs, loading, loadFailed, needsReload, saving, errorMessage, successMessage, setSingleInput: updateSingleInput, setSkuInput: updateSkuInput, save, reload, close };
};
