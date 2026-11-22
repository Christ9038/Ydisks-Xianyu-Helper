import { CreditCard, FileText, Globe, Image as ImageIcon, Plus, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ImageSourceField } from '../../../../shared/ui/ImageSourceField';
import type { CardCreateDialogProps } from '../../../../shared/ui/cardCreate';
import { createCard, testCardAPI } from '../api';
import { buildCreatePayload, emptyAddForm, validateAddForm } from '../createCardDraft';
import { updateAPIAddForm, apiConfigFromAddForm } from '../editConfig';
import type { AddCardForm } from '../types';
import { APIRequestBuilder } from './APIRequestBuilder';

// typeOptions 是新增弹窗按显示顺序排列的四种卡密类型及其图标。
const typeOptions = [
  { type: 'data', label: '批量库存', Icon: CreditCard },
  { type: 'text', label: '文本', Icon: FileText },
  { type: 'image', label: '图片', Icon: ImageIcon },
  { type: 'api', label: 'API 接口', Icon: Globe },
] as const;

// CardCreateDialog 渲染“添加新卡密”弹窗；草稿只存在于弹窗内，关闭即卸载并丢弃（含 API 请求头等敏感内容）。
export const CardCreateDialog = ({ onClose, onCreated, libraryImageChooser }: CardCreateDialogProps) => {
  // form 是新增卡密草稿。
  const [form, setForm] = useState<AddCardForm>(emptyAddForm);
  // error 是校验或创建失败后展示在弹窗内的原因。
  const [error, setError] = useState('');
  // busy 表示创建请求在途，期间禁用提交避免重复创建。
  const [busy, setBusy] = useState(false);
  // alive 标记弹窗仍挂载；卸载后迟到的响应不再回调调用方或写入状态。
  const alive = useRef(true);
  useEffect(/* 挂载时标记存活，卸载时置为 false。 */ () => {
    alive.current = true;
    return /* 卸载清理：丢弃迟到的响应。 */ () => { alive.current = false; };
  }, []);

  /** switchType 切换类型并清空与类型相关的内容，避免把上一类型的内容带入。 */
  const switchType = (type: AddCardForm['type']): void => setForm(/* current 是切换前的草稿。 */ current => ({ ...current, type, content: '', library_image_id: 0 }));

  /** submit 校验并创建卡密；成功后回传新卡密，失败留在弹窗内供重试。 */
  const submit = async (): Promise<void> => {
    if (busy) return;
    // message 是校验失败原因，空串表示通过。
    const message = validateAddForm(form);
    setError(message);
    if (message) return;
    setBusy(true);
    try {
      // result 是创建接口返回的新卡密主键。
      const result = await createCard(buildCreatePayload(form));
      if (alive.current) onCreated({ id: result.id, name: form.name.trim() });
    } catch (/* failure 是创建请求的失败原因。 */ failure: unknown) {
      if (alive.current) setError(failure instanceof Error && failure.message ? failure.message : '添加失败，请重试');
    } finally {
      if (alive.current) setBusy(false);
    }
  };

  return createPortal(
    <div className="modal-overlay-centered">
      <div className="modal-container" style={{ maxWidth: '780px' }} role="dialog" aria-modal="true" aria-labelledby="card-create-title">
        <div className="modal-header flex items-center justify-between gap-4">
          <div>
            <h3 id="card-create-title" className="text-2xl font-extrabold text-gray-900">添加新卡密</h3>
            <p className="text-sm text-gray-500 mt-1">选择交付方式并录入自动发货内容</p>
          </div>
          <button onClick={onClose} className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0" title="关闭">
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>

        <div className="modal-body">
          <div className="space-y-6">
            {error && <div role="alert" className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}
            <div className="space-y-2">
              <label className="block text-sm font-bold text-gray-700 mb-2">卡密名称</label>
              <input type="text" value={form.name} onChange={/* 修改卡密名称。 */ e => setForm({ ...form, name: e.target.value })} placeholder="例如：VIP会员卡密" className="w-full ios-input px-4 py-3 rounded-xl" />
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-bold text-gray-700 mb-2">类型</label>
              <div className="grid grid-cols-4 gap-2">
                {typeOptions.map(/* option 渲染一个类型按钮。 */ ({ type, label, Icon }) => (
                  <button key={type} type="button" onClick={/* 切换到该类型。 */ () => switchType(type)}
                    className={`p-3 rounded-xl font-bold transition-all ${form.type === type ? 'bg-brand text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>
                    <Icon className="w-5 h-5 mx-auto mb-1" />
                    {label}
                  </button>
                ))}
              </div>
            </div>

            {form.type !== 'api' && <div className="space-y-2">
              <label className="block text-sm font-bold text-gray-700 mb-2">
                {form.type === 'data' ? '库存内容（一行一个）' : form.type === 'text' ? '固定回复内容' : '卡密图片'}
              </label>
              {form.type === 'image' ? (
                <div className="space-y-2">
                  <ImageSourceField
                    value={{ url: form.content, libraryImageId: form.library_image_id }}
                    onChange={/* source 是用户选择的新图片来源；外链与图片库图片互斥。 */ source => setForm({ ...form, content: source.url, library_image_id: source.libraryImageId })}
                    chooser={libraryImageChooser}
                    urlPlaceholder="https://example.com/card.png"
                  />
                  <p className="text-xs text-gray-500">图片库图片发货时由服务端读取；外链图片发货时会临时下载并上传到闲鱼</p>
                </div>
              ) : (
                <textarea value={form.content} onChange={/* 修改卡密内容。 */ e => setForm({ ...form, content: e.target.value })}
                  className={`w-full ios-input px-4 py-3 rounded-xl resize-none text-sm ${form.type === 'data' ? 'h-48 font-mono' : 'h-32'}`}
                  placeholder={form.type === 'data' ? 'CODE-123456\nCODE-789012\n...' : '请输入每次发货时发送的固定文字'} />
              )}
              {form.type === 'data' && (
                <p className="text-xs text-gray-500">当前库存：<span className="font-bold text-brand">{form.content.split('\n').filter(/* line 是单行库存文本。 */ line => line.trim()).length}</span> 条</p>
              )}
            </div>}

            {form.type === 'api' && (
              <APIRequestBuilder
                url={form.content}
                method={form.api_method}
                timeout={form.api_timeout}
                headers={form.api_headers}
                params={form.api_params}
                contentType={form.api_content_type}
                body={form.api_body}
                responsePath={form.api_response_path}
                retryEnabled={form.api_retry_enabled}
                onTest={/* 使用当前草稿发起临时 API 测试，不创建卡密。 */ signal => testCardAPI(apiConfigFromAddForm(form), undefined, { signal })}
                onChange={/* 将字段修改写回当前草稿。 */ (field, value) => setForm(/* current 是最新草稿。 */ current => updateAPIAddForm(current, field, value))}
              />
            )}

            <div className="grid grid-cols-1 sm:grid-cols-[1fr_180px] gap-4">
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">描述</label>
                <input value={form.description} onChange={/* 修改描述。 */ e => setForm({ ...form, description: e.target.value })} placeholder="卡密用途描述（可选）" className="w-full ios-input px-4 py-3 rounded-xl" />
              </div>
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">延时发货（秒）</label>
                <input type="number" value={form.delay_seconds} onChange={/* 修改延时秒数。 */ e => setForm({ ...form, delay_seconds: parseInt(e.target.value) || 0 })} className="w-full ios-input px-4 py-3 rounded-xl" min="0" max="3600" />
              </div>
            </div>
          </div>
        </div>

        <div className="modal-footer">
          <div className="flex gap-3 w-full">
            <button onClick={onClose} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors">取消</button>
            <button onClick={/* 提交创建。 */ () => void submit()} disabled={busy} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2 disabled:opacity-50">
              <Plus className="w-4 h-4" />
              {busy ? '添加中...' : '添加卡密'}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
};

export default CardCreateDialog;
