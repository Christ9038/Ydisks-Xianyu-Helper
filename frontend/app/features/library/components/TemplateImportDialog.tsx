import { Download, X } from 'lucide-react';
import { useState } from 'react';
import { createPortal } from 'react-dom';
import { consumeSelectedFile } from '../../../../shared/browser/fileInput';
import { downloadImportTemplate, useTemplateImport } from '../templateImport';
import type { LibraryImportProgressRow } from '../types';

/** TemplateImportDialogProps 描述 Excel 导入商品模板弹窗。 */
export interface TemplateImportDialogProps {
  /** onClose 关闭弹窗；未完成的导入任务会先被丢弃。 */
  onClose: () => void;
  /** onImported 在导入完成或中途取消时收到已创建的模板主键。 */
  onImported: (templateIds: number[]) => void;
}

/** statusText 是导入行状态的展示文案。 */
const statusText: Record<LibraryImportProgressRow['status'], string> = { invalid: '预检未通过', pending: '待导入', created: '已导入', failed: '导入失败' };

/** TemplateImportDialog 引导用户上传表格与图片压缩包、查看逐行预检，再分段导入为商品模板。 */
export const TemplateImportDialog = ({ onClose, onImported }: TemplateImportDialogProps) => {
  // flow 是导入流程状态与操作。
  const flow = useTemplateImport(onImported);
  // onlyProblems 为真时预检表只显示有错误或提示的行，仅影响展示。
  const [onlyProblems, setOnlyProblems] = useState(false);
  // preview 是最近一次预检结果。
  const preview = flow.preview;
  // progress 是最近一次导入进度。
  const progress = flow.progress;
  // busy 表示预检或导入请求进行中。
  const busy = flow.phase === 'previewing' || flow.phase === 'importing';
  // visibleRows 是按“只看有问题”筛选后的预检行。
  const visibleRows = (preview?.rows ?? []).filter(/* row 保留有问题的行或全部行。 */ row => !onlyProblems || row.errors.length > 0 || row.warnings.length > 0);
  // failedRows 是导入时失败的行。
  const failedRows = (progress?.rows ?? []).filter(/* row 保留导入失败的行。 */ row => row.status === 'failed');
  // processed 是已处理（成功或失败）的可导入行数，用于进度条。
  const processed = progress ? progress.created + progress.failed : 0;
  // total 是可导入行总数。
  const total = preview?.valid ?? 0;

  /** close 关闭弹窗：导入完成后直接关闭，其余阶段先取消并丢弃未完成任务。 */
  const close = (): void => {
    if (flow.phase !== 'done') flow.cancel();
    onClose();
  };

  return createPortal(
    <div className="modal-overlay" role="presentation">
      <div className="modal-container" style={{ maxWidth: '60rem' }} role="dialog" aria-modal="true" aria-labelledby="template-import-title">
        <div className="modal-header flex items-start justify-between gap-4">
          <div>
            <h2 id="template-import-title" className="text-xl font-black text-gray-950">从 Excel 导入商品模板</h2>
            <p className="mt-1 text-sm text-gray-500">表格列与批量铺货相同，但不需要账号；导入后在列表中勾选模板，再选择账号发布。单次最多 300 行。</p>
          </div>
          <button type="button" onClick={close} aria-label="关闭导入弹窗" className="flex h-10 w-10 items-center justify-center rounded-2xl bg-gray-100 hover:bg-gray-200"><X className="h-5 w-5 text-gray-600" /></button>
        </div>
        <div className="modal-body space-y-4">
          {flow.error && <div role="alert" className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{flow.error}</div>}
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block rounded-xl border border-dashed border-gray-300 bg-white p-4 text-sm font-bold text-gray-800">商品表格（必填）
              <span className="mt-1 block text-xs font-normal text-gray-500">{flow.sheet ? flow.sheet.name : '支持 .xlsx / .csv / .tsv，最大 20 MiB'}</span>
              <input aria-label="选择商品表格" type="file" accept=".xlsx,.csv,.tsv" className="mt-2 block w-full text-xs" disabled={busy}
                onChange={/* 读取表格并作废旧预检。 */ event => flow.selectSheet(consumeSelectedFile(event.currentTarget))} />
            </label>
            <label className="block rounded-xl border border-dashed border-gray-300 bg-white p-4 text-sm font-bold text-gray-800">图片压缩包（可选）
              <span className="mt-1 block text-xs font-normal text-gray-500">{flow.imagesZip ? flow.imagesZip.name : '表格“图片”列填 zip 内路径时上传，最大 200 MiB；网址图片会在导入时下载'}</span>
              <input aria-label="选择图片压缩包" type="file" accept=".zip" className="mt-2 block w-full text-xs" disabled={busy}
                onChange={/* 读取压缩包并作废旧预检。 */ event => flow.selectImagesZip(consumeSelectedFile(event.currentTarget))} />
            </label>
          </div>
          <button type="button" onClick={downloadImportTemplate} className="inline-flex items-center gap-2 rounded-xl ios-btn-secondary px-4 py-2 text-sm font-bold"><Download className="h-4 w-4" />下载导入模板</button>
          {preview && (
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-4">
                <p className="text-sm font-bold">共 {preview.total} 行，<span className="text-emerald-700">可导入 {preview.valid}</span>，<span className="text-red-700">需修正 {preview.invalid}</span>{preview.remoteImages > 0 && <span className="text-gray-500">，导入时需下载 {preview.remoteImages} 张网址图片</span>}</p>
                <label className="inline-flex items-center gap-1.5 text-xs font-bold text-gray-600"><input type="checkbox" checked={onlyProblems} onChange={/* 切换只看有问题。 */ event => setOnlyProblems(event.target.checked)} />只看有问题的行</label>
              </div>
              <div className="max-h-72 overflow-auto rounded-xl border border-gray-200">
                <table className="min-w-full text-left text-sm">
                  <thead className="bg-gray-50 text-xs text-gray-500"><tr><th className="px-3 py-2">行</th><th className="px-3 py-2">模板名称</th><th className="px-3 py-2">标题</th><th className="px-3 py-2">价格</th><th className="px-3 py-2">图片</th><th className="px-3 py-2">状态</th></tr></thead>
                  <tbody className="divide-y divide-gray-100">
                    {visibleRows.map(/* row 渲染一行预检。 */ row => (
                      <tr key={row.rowNo}>
                        <td className="px-3 py-2 text-gray-500">{row.rowNo}</td><td className="px-3 py-2">{row.name}</td><td className="px-3 py-2">{row.title}</td><td className="px-3 py-2">{row.price}</td>
                        <td className="px-3 py-2">{row.imageCount}{row.remoteImageCount > 0 ? `（网址 ${row.remoteImageCount}）` : ''}</td>
                        <td className="px-3 py-2">{row.valid ? <span className="text-emerald-700">可导入</span> : <span className="text-red-700">{row.errors.join('；')}</span>}
                          {row.warnings.length > 0 && <span className="block text-xs text-amber-700">{row.warnings.join('；')}</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
          {progress && (
            <div className="space-y-2" role="status">
              <p className="text-sm font-bold">{progress.done ? '导入完成' : '正在导入'}：已导入 {progress.created}，失败 {progress.failed}，剩余 {progress.pending}</p>
              <div className="h-2 overflow-hidden rounded-full bg-gray-100"><div className="h-full bg-emerald-500" style={{ width: `${total > 0 ? Math.round((processed / total) * 100) : 0}%` }} /></div>
              {failedRows.length > 0 && (
                <ul className="max-h-40 overflow-auto rounded-xl bg-red-50 px-4 py-2 text-xs text-red-700">
                  {failedRows.map(/* row 渲染一行失败原因。 */ row => <li key={row.rowNo}>第 {row.rowNo} 行{statusText[row.status]}：{row.errors.join('；')}</li>)}
                </ul>
              )}
              {progress.done && progress.created > 0 && <p className="text-xs text-gray-500">新导入的模板已在列表中勾选，关闭弹窗后点击“发布所选”即可选择账号发布。</p>}
            </div>
          )}
        </div>
        <div className="modal-footer"><div className="flex gap-3 w-full">
          <button type="button" onClick={close} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors">{flow.phase === 'done' ? '完成' : flow.phase === 'importing' ? '停止导入' : '取消'}</button>
          {flow.phase !== 'done' && (
            <>
              <button type="button" disabled={busy || !flow.sheet} onClick={/* 上传并预检。 */ () => void flow.runPreview()} className="flex-1 ios-btn-secondary px-6 py-3 rounded-xl font-bold disabled:opacity-50">{flow.phase === 'previewing' ? '预检中...' : '预检'}</button>
              <button type="button" disabled={busy || !preview || preview.valid <= 0} onClick={/* 开始或继续导入。 */ () => void flow.startImport()} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold disabled:opacity-50">{flow.phase === 'importing' ? '导入中...' : progress ? '继续导入' : `导入 ${preview?.valid ?? 0} 个模板`}</button>
            </>
          )}
        </div></div>
      </div>
    </div>,
    document.body,
  );
};

