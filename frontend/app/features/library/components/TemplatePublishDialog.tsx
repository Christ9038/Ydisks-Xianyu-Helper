import { X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { libraryErrorMessage, previewTemplatePublish, startPublishBatch } from '../api';
import type { LibraryAccountOption, LibraryPublishPreview, ProductTemplate } from '../types';

/** TemplatePublishDialogProps 描述模板发布弹窗。 */
export interface TemplatePublishDialogProps {
  /** templates 是用户多选的模板。 */
  templates: ProductTemplate[];
  /** accounts 是可选目标账号。 */
  accounts: LibraryAccountOption[];
  /** onClose 关闭弹窗。 */
  onClose: () => void;
  /** onStarted 在批次启动成功后通知父级，参数是批次标识。 */
  onStarted: (batchId: string) => void;
}

/** maxPublishRows 是单批最多行数（模板数 × 账号数），与服务端一致。 */
const maxPublishRows = 50;

/** TemplatePublishDialog 先生成“模板 × 账号”预检，用户确认后再启动既有批量铺货批次。 */
export const TemplatePublishDialog = ({ templates, accounts, onClose, onStarted }: TemplatePublishDialogProps) => {
  // selectedAccounts 是已勾选账号。
  const [selectedAccounts, setSelectedAccounts] = useState<string[]>([]);
  // interval 是发布间隔秒数文本。
  const [interval, setInterval] = useState('5');
  // preview 是最近一次预检结果。
  const [preview, setPreview] = useState<LibraryPublishPreview | null>(null);
  // busy 表示预检或启动请求进行中。
  const [busy, setBusy] = useState(false);
  // error 是最近一次失败原因。
  const [error, setError] = useState('');
  // controllerRef 保存进行中请求的取消控制器，关闭弹窗或重复触发时取消。
  const controllerRef = useRef<AbortController | null>(null);

  useEffect(/* 卸载时取消未完成的预检或启动请求。 */ () => /* 卸载清理取消进行中的请求。 */ () => controllerRef.current?.abort(), []);

  // rowCount 是将要生成的行数，由选择推导。
  const rowCount = templates.length * selectedAccounts.length;

  /** toggleAccount 切换账号勾选并使旧预检失效，避免确认与当前选择不一致的批次。 */
  const toggleAccount = (id: string): void => {
    setSelectedAccounts(/* previous 是切换前的勾选。 */ previous => previous.includes(id) ? previous.filter(/* item 移除该账号。 */ item => item !== id) : [...previous, id]);
    setPreview(null);
  };

  /** runPreview 生成预检；不访问平台。 */
  const runPreview = async (): Promise<void> => {
    controllerRef.current?.abort();
    // controller 控制本次请求。
    const controller = new AbortController();
    controllerRef.current = controller;
    setBusy(true);
    setError('');
    try {
      // result 是预检结果。
      const result = await previewTemplatePublish({ templateIds: templates.map(/* template 提取主键。 */ template => template.id), cookieIds: selectedAccounts, intervalSeconds: Number(interval) || 5 }, { signal: controller.signal });
      if (!controller.signal.aborted) setPreview(result);
    } catch (failure /* failure 是预检失败原因。 */) {
      if (!controller.signal.aborted) setError(libraryErrorMessage(failure, '生成发布预检失败'));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  };

  /** confirm 启动批次；成功后交给父级跳转到进度页。 */
  const confirm = async (): Promise<void> => {
    if (!preview) return;
    controllerRef.current?.abort();
    // controller 控制本次请求。
    const controller = new AbortController();
    controllerRef.current = controller;
    setBusy(true);
    setError('');
    try {
      // batchId 是新启动的批次标识。
      const batchId = await startPublishBatch(preview.previewId, { signal: controller.signal });
      if (!controller.signal.aborted) onStarted(batchId);
    } catch (failure /* failure 是启动失败原因。 */) {
      if (!controller.signal.aborted) setError(libraryErrorMessage(failure, '启动发布失败'));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  };

  return createPortal(
    <div className="modal-overlay" role="presentation">
      <div className="modal-container" style={{ maxWidth: '56rem' }} role="dialog" aria-modal="true" aria-labelledby="template-publish-title">
        <div className="modal-header flex items-start justify-between gap-4">
          <div>
            <h2 id="template-publish-title" className="text-xl font-black text-gray-950">发布 {templates.length} 个模板</h2>
            <p className="mt-1 text-sm text-gray-500">选择目标账号并生成预检，确认后沿用批量铺货的串行发布、进度与失败重试。</p>
          </div>
          <button type="button" onClick={onClose} aria-label="关闭发布弹窗" className="flex h-10 w-10 items-center justify-center rounded-2xl bg-gray-100 hover:bg-gray-200"><X className="h-5 w-5 text-gray-600" /></button>
        </div>
        <div className="modal-body space-y-4">
          {error && <div role="alert" className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}
          <fieldset className="space-y-2">
            <legend className="text-sm font-extrabold text-gray-900">目标账号</legend>
            {accounts.length === 0 && <p className="text-sm text-slate-500">没有可用账号，请先在账号管理中启用账号。</p>}
            <div className="grid gap-2 sm:grid-cols-2">
              {accounts.map(/* account 渲染一个账号勾选项。 */ account => (
                <label key={account.id} className="flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm"><input type="checkbox" checked={selectedAccounts.includes(account.id)} onChange={/* 切换账号。 */ () => toggleAccount(account.id)} />{account.label}</label>
              ))}
            </div>
          </fieldset>
          <label className="block text-sm font-bold text-gray-800">发布间隔（秒）<input aria-label="发布间隔秒数" type="number" min={0} max={3600} className="ios-input mt-1 w-32 rounded-xl px-3 py-2" value={interval} onChange={/* 修改间隔并使旧预检失效。 */ event => { setInterval(event.target.value); setPreview(null); }} /></label>
          <p className="text-xs text-slate-500">将生成 {rowCount} 行（模板 × 账号，上限 {maxPublishRows}）；发货地使用账号平台默认值。</p>
          {preview && (
            <div className="space-y-2">
              <p className="text-sm font-bold">共 {preview.total} 行，<span className="text-emerald-700">可发布 {preview.valid}</span>，<span className="text-red-700">需修正 {preview.invalid}</span></p>
              <div className="max-h-72 overflow-auto rounded-xl border border-slate-200">
                <table className="min-w-full text-left text-sm">
                  <thead className="bg-slate-50 text-xs text-slate-500"><tr><th className="px-3 py-2">账号</th><th className="px-3 py-2">标题</th><th className="px-3 py-2">价格</th><th className="px-3 py-2">库存</th><th className="px-3 py-2">状态</th></tr></thead>
                  <tbody className="divide-y divide-slate-100">
                    {preview.rows.map(/* row 渲染一行预检。 */ row => (
                      <tr key={row.rowNo}><td className="px-3 py-2">{row.cookieId}</td><td className="px-3 py-2">{row.title}</td><td className="px-3 py-2">{row.price}</td><td className="px-3 py-2">{row.quantity}</td>
                        <td className="px-3 py-2">{row.valid ? <span className="text-emerald-700">可发布</span> : <span className="text-red-700">{row.errors.join('；') || '需修正'}</span>}</td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
        <div className="modal-footer flex justify-end gap-3">
          <button type="button" onClick={onClose} className="rounded-xl bg-gray-100 px-4 py-2.5 text-sm font-bold text-gray-700">取消</button>
          <button type="button" disabled={busy || rowCount === 0 || rowCount > maxPublishRows} onClick={/* 生成预检。 */ () => void runPreview()} className="rounded-xl bg-slate-950 px-4 py-2.5 text-sm font-bold text-white disabled:opacity-50">{busy && !preview ? '处理中...' : '生成预检'}</button>
          <button type="button" disabled={busy || !preview || preview.valid <= 0} onClick={/* 确认并启动批次。 */ () => void confirm()} className="rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-bold text-white disabled:opacity-50">{preview ? `确认发布 ${preview.valid} 个商品` : '确认发布'}</button>
        </div>
      </div>
    </div>,
    document.body,
  );
};
