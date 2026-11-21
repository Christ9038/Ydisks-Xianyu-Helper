import { AlertCircle,CheckCircle2,Loader2,Package,Save,X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import type { Item } from '../api';
import { useItemStockEditor } from '../useItemStockEditor';

/** ItemStockModalProps 描述商品库存弹窗的输入。 */
export interface ItemStockModalProps {
  /** item 是正在调整库存的商品；为空时弹窗不渲染。 */
  item: Item | null;
  /** open 表示弹窗当前是否可见。 */
  open: boolean;
  /** onClose 关闭弹窗并释放当前请求所有权。 */
  onClose: () => void;
}

// centsToYuan 把分格式化为带两位小数的元文本，仅用于展示规格售价。
const centsToYuan = (cents: number): string => (cents / 100).toFixed(2);

/** ItemStockModal 按需读取并修改单件商品的库存；单规格一个输入框，多规格逐行输入。 */
export const ItemStockModal: React.FC<ItemStockModalProps> = ({ item, open, onClose }) => {
  // editor 是库存读取、草稿与提交的状态机。
  const editor = useItemStockEditor({ item, open, onClose });
  if (!open || !item) return null;
  // stock 是当前库存快照，读取完成前为空。
  const { stock } = editor;
  // blocked 表示当前不允许提交。
  const blocked = editor.loading || editor.saving || editor.loadFailed || editor.needsReload || !stock;

  return createPortal(
    <div className="modal-overlay-centered" role="presentation">
      <div className="modal-container" role="dialog" aria-modal="true" aria-labelledby="item-stock-title" style={{ maxWidth: '560px' }}>
        <div className="modal-header flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-brand">
              <Package className="h-5 w-5 shrink-0" />
              <h3 id="item-stock-title" className="truncate text-xl font-extrabold text-gray-900">调整库存</h3>
            </div>
            <p className="mt-1 truncate text-xs text-gray-500" title={item.item_title || item.item_id}>{item.item_title || `商品 ${item.item_id}`}</p>
          </div>
          <button type="button" onClick={editor.close} className="rounded-xl p-2 transition-colors hover:bg-gray-100" aria-label="关闭库存调整">
            <X className="h-5 w-5 text-gray-500" />
          </button>
        </div>

        <div className="modal-body space-y-4">
          {editor.loading ? (
            <div className="flex min-h-40 items-center justify-center gap-2 text-sm font-bold text-gray-500">
              <Loader2 className="h-5 w-5 animate-spin text-brand" />
              正在读取库存
            </div>
          ) : editor.loadFailed ? (
            <div className="flex min-h-40 flex-col items-center justify-center gap-4 text-center">
              <AlertCircle className="h-7 w-7 text-red-500" />
              <p role="alert" className="max-w-md text-sm font-medium leading-6 text-red-700">{editor.errorMessage}</p>
              <button type="button" onClick={editor.reload} className="rounded-xl bg-contrast px-5 py-2.5 text-sm font-bold text-on-contrast hover:bg-contrast/90">重新加载</button>
            </div>
          ) : stock ? (
            <>
              {stock.multiSpec ? (
                <div className="space-y-2">
                  <p className="text-xs font-bold text-gray-500">多规格商品：按规格分别设置库存，会整体提交全部规格。</p>
                  {stock.skus.map(/* sku 是当前规格行，渲染为一行库存输入。 */ sku => (
                    <label key={sku.key} className="flex items-center justify-between gap-3 rounded-xl border border-gray-200 bg-white px-3 py-2.5">
                      <span className="min-w-0">
                        <span className="block truncate text-sm font-bold text-gray-900">{sku.label || sku.key}</span>
                        <span className="block text-[11px] text-gray-500">¥{centsToYuan(sku.priceCents)} · 当前 {sku.quantity}</span>
                      </span>
                      <input
                        inputMode="numeric"
                        aria-label={`${sku.label || sku.key} 库存`}
                        value={editor.skuInputs[sku.key] ?? ''}
                        onChange={/* skuInputHandler 写入该规格行的库存草稿。 */ event => editor.setSkuInput(sku.key, event.target.value)}
                        className="ios-input w-28 shrink-0 rounded-xl px-3 py-2 text-right text-sm"
                      />
                    </label>
                  ))}
                </div>
              ) : (
                <label className="flex items-center justify-between gap-3 rounded-xl border border-gray-200 bg-white px-3 py-3">
                  <span>
                    <span className="block text-sm font-bold text-gray-900">库存数量</span>
                    <span className="block text-[11px] text-gray-500">当前 {stock.quantity}</span>
                  </span>
                  <input
                    inputMode="numeric"
                    aria-label="库存数量"
                    value={editor.singleInput}
                    onChange={/* singleInputHandler 写入单规格库存草稿。 */ event => editor.setSingleInput(event.target.value)}
                    className="ios-input w-32 shrink-0 rounded-xl px-3 py-2 text-right text-sm"
                  />
                </label>
              )}
              {editor.needsReload && (
                <button type="button" onClick={editor.reload} className="w-full rounded-xl bg-contrast px-4 py-2.5 text-sm font-bold text-on-contrast hover:bg-contrast/90">重新读取最新库存</button>
              )}
              {editor.errorMessage && (
                <div role="alert" className="flex items-start gap-2 rounded-xl border border-red-100 bg-red-50 px-3 py-2.5 text-xs font-medium leading-5 text-red-700">
                  <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                  <span>{editor.errorMessage}</span>
                </div>
              )}
              {editor.successMessage && (
                <div role="status" className="flex items-start gap-2 rounded-xl border border-emerald-100 bg-emerald-50 px-3 py-2.5 text-xs font-bold leading-5 text-emerald-700">
                  <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
                  <span>{editor.successMessage}</span>
                </div>
              )}
              <p className="text-[11px] leading-5 text-gray-400">保存会向闲鱼发起一次读取和一次提交；部分商品类目不支持网页端编辑，此时请在闲鱼 App 中修改。</p>
            </>
          ) : null}
        </div>

        <div className="modal-footer flex gap-3">
          <button type="button" onClick={editor.close} className="flex-1 rounded-xl bg-gray-100 px-5 py-3 font-bold text-gray-700 transition-colors hover:bg-gray-200">关闭</button>
          <button type="button" onClick={/* saveClickHandler 提交当前库存草稿。 */ () => void editor.save()} disabled={blocked} className="ios-btn-primary flex flex-1 items-center justify-center gap-2 rounded-xl px-5 py-3 font-bold disabled:cursor-not-allowed disabled:opacity-50">
            {editor.saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {editor.saving ? '保存中...' : '保存库存'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
};
