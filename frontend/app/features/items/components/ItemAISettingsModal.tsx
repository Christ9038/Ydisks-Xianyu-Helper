import { AlertCircle,Bot,Loader2,Save,X } from 'lucide-react';
import React,{ useCallback,useEffect,useRef,useState } from 'react';
import { createPortal } from 'react-dom';
import { getItemAISettings,itemErrorMessage,updateItemAISettings } from '../api';
import type { Item,ItemAIOverride,ItemAISettings } from '../api';

// ITEM_CONTEXT_LIMIT 是商品专属资料允许提交的最大字符数，与服务端校验保持一致。
export const ITEM_CONTEXT_LIMIT = 12_000;

// ItemAIOverrideOption 描述三态控件中的单个可选策略。
interface ItemAIOverrideOption {
  // value 是提交给服务端的商品级覆盖值。
  value: ItemAIOverride;
  // label 是三态控件中的简短标题。
  label: string;
  // description 说明该策略与账号级 AI 开关的关系。
  description: string;
}

// ITEM_AI_OVERRIDE_OPTIONS 按继承、启用、停用顺序提供稳定的三态选项。
const ITEM_AI_OVERRIDE_OPTIONS: readonly ItemAIOverrideOption[] = [
  { value: 'inherit', label: '继承账号', description: '跟随所属账号的 AI 客服开关' },
  { value: 'enabled', label: '强制启用', description: '该商品始终允许 AI 客服参与回复' },
  { value: 'disabled', label: '强制停用', description: '该商品跳过 AI，继续使用其他回复规则' },
];

// ItemAISettingsModalProps 描述商品级 AI 配置弹窗的输入和保存回调。
export interface ItemAISettingsModalProps {
  // item 是当前正在编辑 AI 配置的商品；为空时弹窗不渲染。
  item: Item | null;
  // open 表示弹窗当前是否可见。
  open: boolean;
  // onClose 关闭弹窗并释放当前请求所有权。
  onClose: () => void;
  // onSaved 将服务端确认后的配置同步回商品列表卡片。
  onSaved: (settings: ItemAISettings) => void;
}

// ItemAISettingsModal 负责按需加载、编辑并保存单件商品的 AI 客服配置。
export const ItemAISettingsModal: React.FC<ItemAISettingsModalProps> = ({ item, open, onClose, onSaved }) => {
  // [settings, setSettings] 保存弹窗内可编辑且在失败后继续保留的配置草稿。
  const [settings, setSettings] = useState<ItemAISettings>({ ai_override: 'inherit', item_context: '' });
  // [loading, setLoading] 表示弹窗是否正在读取服务端配置。
  const [loading, setLoading] = useState(false);
  // [saving, setSaving] 表示用户提交是否仍在进行。
  const [saving, setSaving] = useState(false);
  // [errorMessage, setErrorMessage] 保存当前加载或保存失败的用户可见说明。
  const [errorMessage, setErrorMessage] = useState('');
  // requestGeneration 标识当前弹窗请求代次，旧商品响应不得覆盖新商品草稿。
  const requestGeneration = useRef(0);
  // loadController 保存当前读取请求的取消控制器。
  const loadController = useRef<AbortController | null>(null);
  // saveController 保存当前保存请求的取消控制器。
  const saveController = useRef<AbortController | null>(null);

  // cancelRequests 取消弹窗当前持有的读取和保存请求。
  const cancelRequests = useCallback(/* cancelRequestsCallback 释放旧商品或已关闭弹窗的网络请求。 */ () => {
    loadController.current?.abort();
    saveController.current?.abort();
    loadController.current = null;
    saveController.current = null;
  }, []);

  useEffect(/* itemSettingsLoadEffect 在弹窗打开或商品切换时读取一次完整配置。 */ () => {
    if (!open || !item) return undefined;
    cancelRequests();
    // generation 是本次商品配置读取的单调递增代次。
    const generation = ++requestGeneration.current;
    // controller 允许关闭弹窗或切换商品时取消本次读取。
    const controller = new AbortController();
    loadController.current = controller;
    setSettings({ ai_override: item.ai_override || 'inherit', item_context: '' });
    setErrorMessage('');
    setLoading(true);
    setSaving(false);
    void getItemAISettings(item.cookie_id, item.item_id, { signal: controller.signal })
      .then(/* settingsResponseHandler 仅接收当前商品仍有效的读取结果。 */ response => {
        if (generation !== requestGeneration.current || controller.signal.aborted) return;
        setSettings(response);
      })
      .catch(/* settingsLoadErrorHandler 忽略主动取消，并展示仍有效的加载失败。 */ error => {
        if (generation !== requestGeneration.current || controller.signal.aborted) return;
        setErrorMessage(itemErrorMessage(error, '商品 AI 配置加载失败，请重试。'));
      })
      .finally(/* settingsLoadFinallyHandler 只结束当前代次的加载状态。 */ () => {
        if (generation === requestGeneration.current && !controller.signal.aborted) setLoading(false);
      });
    return /* itemSettingsLoadCleanup 取消离开当前商品后仍在执行的读取或保存。 */ () => {
      controller.abort();
      saveController.current?.abort();
    };
  }, [cancelRequests, item, open]);

  // closeModal 关闭弹窗，并阻止所有晚到响应继续写入界面状态。
  const closeModal = () => {
    requestGeneration.current += 1;
    cancelRequests();
    setLoading(false);
    setSaving(false);
    onClose();
  };

  // saveSettings 校验资料长度并提交当前商品的配置草稿。
  const saveSettings = async () => {
    if (!item || saving || loading) return;
    if (settings.item_context.length > ITEM_CONTEXT_LIMIT) {
      setErrorMessage(`商品资料不能超过 ${ITEM_CONTEXT_LIMIT} 个字符。`);
      return;
    }
    saveController.current?.abort();
    // generation 是本次保存所属的弹窗请求代次。
    const generation = requestGeneration.current;
    // controller 允许关闭弹窗或切换商品时取消保存。
    const controller = new AbortController();
    saveController.current = controller;
    setSaving(true);
    setErrorMessage('');
    try {
      // savedSettings 是服务端校验并持久化后的商品级 AI 配置。
      const savedSettings = await updateItemAISettings(item.cookie_id, item.item_id, settings, { signal: controller.signal });
      if (generation !== requestGeneration.current || controller.signal.aborted) return;
      onSaved(savedSettings);
      closeModal();
    } catch (error /* error 是保存接口返回且需要保留当前草稿的失败原因。 */) {
      if (generation !== requestGeneration.current || controller.signal.aborted) return;
      setErrorMessage(itemErrorMessage(error, '商品 AI 配置保存失败，请重试。'));
    } finally {
      if (generation === requestGeneration.current && !controller.signal.aborted) setSaving(false);
    }
  };

  if (!open || !item) return null;

  return createPortal(
    <div className="modal-overlay-centered" role="presentation">
      <div className="modal-container" role="dialog" aria-modal="true" aria-labelledby="item-ai-settings-title" style={{ maxWidth: '680px' }}>
        <div className="modal-header flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-brand">
              <Bot className="h-5 w-5 shrink-0" />
              <h3 id="item-ai-settings-title" className="truncate text-xl font-extrabold text-gray-900">商品 AI 客服</h3>
            </div>
            <p className="mt-1 truncate text-xs text-gray-500" title={item.item_title || item.item_id}>{item.item_title || `商品 ${item.item_id}`}</p>
          </div>
          <button type="button" onClick={closeModal} className="rounded-xl p-2 transition-colors hover:bg-gray-100" aria-label="关闭商品 AI 配置">
            <X className="h-5 w-5 text-gray-500" />
          </button>
        </div>

        <div className="modal-body space-y-6">
          {loading ? (
            <div className="flex min-h-52 items-center justify-center gap-2 text-sm font-bold text-gray-500">
              <Loader2 className="h-5 w-5 animate-spin text-brand" />
              正在加载配置
            </div>
          ) : (
            <>
              <fieldset className="space-y-3">
                <legend className="text-sm font-extrabold text-gray-900">AI 回复策略</legend>
                <div className="grid grid-cols-1 gap-2 rounded-xl bg-gray-100 p-1 sm:grid-cols-3" role="radiogroup" aria-label="商品 AI 回复策略">
                  {ITEM_AI_OVERRIDE_OPTIONS.map(/* overrideOptionRenderer 渲染单个商品 AI 三态选项。 */ option => {
                    // selected 表示当前选项是否与配置草稿一致。
                    const selected = settings.ai_override === option.value;
                    return (
                      <label key={option.value} className={`min-w-0 cursor-pointer rounded-lg px-3 py-2.5 text-left transition-colors ${selected ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-800'}`}>
                        <input
                          type="radio"
                          name="item-ai-override"
                          value={option.value}
                          checked={selected}
                          onChange={/* overrideChangeHandler 将用户选择写入配置草稿。 */ () => setSettings(/* previousSettings 保留尚未提交的商品资料。 */ previousSettings => ({ ...previousSettings, ai_override: option.value }))}
                          className="sr-only"
                        />
                        <span className="block text-xs font-extrabold">{option.label}</span>
                        <span className="mt-1 block text-[11px] leading-4 text-gray-500">{option.description}</span>
                      </label>
                    );
                  })}
                </div>
              </fieldset>

              <div className="space-y-2">
                <div className="flex items-end justify-between gap-3">
                  <label htmlFor="item-ai-context" className="text-sm font-extrabold text-gray-900">商品专属资料</label>
                  <span className={`text-xs font-bold ${settings.item_context.length >= ITEM_CONTEXT_LIMIT ? 'text-red-600' : 'text-gray-400'}`}>{settings.item_context.length} / {ITEM_CONTEXT_LIMIT}</span>
                </div>
                <textarea
                  id="item-ai-context"
                  value={settings.item_context}
                  maxLength={ITEM_CONTEXT_LIMIT}
                  rows={10}
                  onChange={/* contextChangeHandler 更新商品专属资料草稿并清理旧保存错误。 */ event => {
                    setSettings(/* previousSettings 保留当前商品 AI 覆盖状态。 */ previousSettings => ({ ...previousSettings, item_context: event.target.value }));
                    if (errorMessage) setErrorMessage('');
                  }}
                  placeholder={'例如：\n- 材质：全新未拆封，黑色款\n- 发货：付款后 24 小时内寄出\n- 使用方式：收到后按包装内说明操作'}
                  className="ios-input min-h-56 w-full resize-y rounded-xl px-4 py-3 text-sm leading-6"
                />
              </div>

              {errorMessage && (
                <div role="alert" className="flex items-start gap-2 rounded-xl border border-red-100 bg-red-50 px-3 py-2.5 text-xs font-medium leading-5 text-red-700">
                  <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                  <span>{errorMessage}</span>
                </div>
              )}
            </>
          )}
        </div>

        <div className="modal-footer flex gap-3">
          <button type="button" onClick={closeModal} className="flex-1 rounded-xl bg-gray-100 px-5 py-3 font-bold text-gray-700 transition-colors hover:bg-gray-200">取消</button>
          <button type="button" onClick={/* saveClickHandler 提交当前商品级 AI 配置。 */ () => void saveSettings()} disabled={loading || saving} className="ios-btn-primary flex flex-1 items-center justify-center gap-2 rounded-xl px-5 py-3 font-bold disabled:cursor-not-allowed disabled:opacity-50">
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {saving ? '保存中...' : '保存配置'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
};
