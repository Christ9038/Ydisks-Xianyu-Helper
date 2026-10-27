import { Download,RefreshCw,Search,X } from 'lucide-react';
import React,{ useState } from 'react';
import { createPortal } from 'react-dom';
import { formatLocalDateTime } from '../../../../dateTime';
import type { Card,CardSaleRecord,CardSaleStatus } from '../models';
import { unsoldLines,useCardExport,useCardSaleRecords } from '../saleRecords';

// CardStockDialogProps 描述库存记录弹窗需要的卡密组和关闭回调。
export interface CardStockDialogProps {
  // card 是被查看的卡密组；弹窗按 card.id 作为 key 挂载，切换卡密组会重置筛选。
  card: Card;
  // onClose 在用户关闭弹窗时调用，弹窗卸载即取消进行中的请求并清空卡密明文。
  onClose: () => void;
}

// unsoldPreviewLimit 限制未售页签一次渲染的行数，避免超大库存拖慢页面；完整内容通过导出获取。
const unsoldPreviewLimit = 200;

// statusLabels 是售出状态的展示文案。
const statusLabels: Record<CardSaleStatus, string> = { sold: '已售', reserved: '待核对' };

// formatSaleTime 复用全站本地时间格式；无效时间显示短横线。
const formatSaleTime = (ms: number): string => (ms > 0 ? formatLocalDateTime(ms) : '-');

// SaleRow 渲染已售表格中的一行记录。
// SaleRow 渲染一行售出记录；record 是该行的售出记录展示模型。
const SaleRow: React.FC<{ /* record 是该行的售出记录展示模型。 */ record: CardSaleRecord }> = ({ record }) => (
  <tr className="border-b border-gray-50">
    <td className="px-3 py-2 font-mono text-xs break-all">{record.content}</td>
    <td className="px-3 py-2">
      <span className={`rounded-md px-2 py-0.5 text-[11px] font-bold ${record.status === 'sold' ? 'bg-green-50 text-green-600' : 'bg-amber-50 text-amber-600'}`}>
        {statusLabels[record.status]}
      </span>
    </td>
    <td className="px-3 py-2 font-mono text-xs">{record.orderId || '-'}</td>
    <td className="px-3 py-2 text-xs">{record.cookieId || '-'}</td>
    <td className="px-3 py-2 text-xs whitespace-nowrap">{formatSaleTime(record.soldAtMs)}</td>
  </tr>
);

// CardStockDialog 展示单个卡密组的未售库存与售出历史，并提供未售 TXT、已售 CSV 导出。
export const CardStockDialog: React.FC<CardStockDialogProps> = ({ card, onClose }) => {
  // supportsUnsold 表示卡密组有本地库存；API 卡每次发货时远端取卡，只有售出历史。
  const supportsUnsold = card.type === 'data';
  // tab 是当前页签，属于瞬时 UI 状态。
  const [tab, setTab] = useState<'unsold' | 'sold'>(supportsUnsold ? 'unsold' : 'sold');
  // searchInput 是搜索框中尚未提交的文本，提交后才写入查询条件。
  const [searchInput, setSearchInput] = useState('');
  // sales 是售出记录查询状态和操作。
  const sales = useCardSaleRecords(card.id);
  // exporter 是导出请求状态和操作。
  const exporter = useCardExport(card.id);
  // lines 是从卡密组库存正文派生出的未售卡密，不另存状态。
  const lines = unsoldLines(card.data_content);

  return createPortal(
    <div className="modal-overlay-centered">
      <div className="modal-container" role="dialog" aria-label={`库存记录 ${card.name}`}>
        <div className="modal-header">
          <h3 className="text-2xl font-extrabold text-gray-900">库存记录 · {card.name}</h3>
          <button onClick={onClose} aria-label="关闭库存记录" className="p-2 rounded-xl hover:bg-gray-100 transition-colors">
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>
        <div className="modal-body space-y-4">
          <div className="flex gap-2" role="tablist">
            {supportsUnsold && (
              <button role="tab" aria-selected={tab === 'unsold'} onClick={/* 点击切换到未售页签。 */ () => setTab('unsold')}
                className={`px-4 py-2 rounded-xl font-bold ${tab === 'unsold' ? 'bg-black text-white' : 'bg-gray-100 text-gray-600'}`}>
                未售 {lines.length}
              </button>
            )}
            <button role="tab" aria-selected={tab === 'sold'} onClick={/* 点击切换到已售页签。 */ () => setTab('sold')}
              className={`px-4 py-2 rounded-xl font-bold ${tab === 'sold' ? 'bg-black text-white' : 'bg-gray-100 text-gray-600'}`}>
              已售 {card.sold_count ?? 0}
            </button>
          </div>

          {exporter.exportError && <div role="alert" className="text-red-600 text-sm">{exporter.exportError}</div>}

          {tab === 'unsold' && supportsUnsold && (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <p className="text-sm text-gray-500">当前库存 {lines.length} 条，发货时按顺序取用。</p>
                <button onClick={/* 点击导出未售 TXT。 */ () => void exporter.runExport('unsold')} disabled={exporter.exporting !== null || lines.length === 0}
                  className="px-4 py-2 rounded-xl bg-gray-100 hover:bg-gray-200 font-bold text-sm flex items-center gap-2 disabled:opacity-50">
                  <Download className="w-4 h-4" />{exporter.exporting === 'unsold' ? '导出中…' : '导出未售 TXT'}
                </button>
              </div>
              {lines.length === 0 ? (
                <p className="py-8 text-center text-gray-400">暂无未售卡密</p>
              ) : (
                <ul className="max-h-80 overflow-y-auto rounded-xl border border-gray-100 divide-y divide-gray-50">
                  {lines.slice(0, unsoldPreviewLimit).map(/* line 是一条未售卡密，index 是其在库存中的位置。 */ (line, index) => (
                    <li key={index} className="px-3 py-2 font-mono text-xs break-all">{line}</li>
                  ))}
                  {lines.length > unsoldPreviewLimit && (
                    <li className="px-3 py-2 text-xs text-gray-400">仅预览前 {unsoldPreviewLimit} 条，完整内容请导出。</li>
                  )}
                </ul>
              )}
            </div>
          )}

          {tab === 'sold' && (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <select aria-label="状态筛选" value={sales.query.status} onChange={/* e 是状态下拉框的变更事件。 */ e => sales.setStatus(e.target.value as CardSaleStatus | '')}
                  className="ios-input px-3 py-2 rounded-xl text-sm">
                  <option value="">全部状态</option>
                  <option value="sold">已售</option>
                  <option value="reserved">待核对</option>
                </select>
                <form className="flex items-center gap-2" onSubmit={/* e 是搜索表单提交事件，阻止默认刷新后应用搜索词。 */ e => { e.preventDefault(); sales.applySearch(searchInput); }}>
                  <input aria-label="搜索订单号或卡密" value={searchInput} onChange={/* e 是搜索框输入事件。 */ e => setSearchInput(e.target.value)}
                    placeholder="订单号或卡密内容" className="ios-input px-3 py-2 rounded-xl text-sm" />
                  <button type="submit" aria-label="搜索" className="p-2 rounded-xl bg-gray-100 hover:bg-gray-200"><Search className="w-4 h-4" /></button>
                </form>
                <button onClick={/* 点击导出已售 CSV。 */ () => void exporter.runExport('sold')} disabled={exporter.exporting !== null}
                  className="ml-auto px-4 py-2 rounded-xl bg-gray-100 hover:bg-gray-200 font-bold text-sm flex items-center gap-2 disabled:opacity-50">
                  <Download className="w-4 h-4" />{exporter.exporting === 'sold' ? '导出中…' : '导出已售 CSV'}
                </button>
              </div>

              {sales.error && (
                <div role="alert" className="text-red-600 text-sm flex items-center gap-2">
                  {sales.error}
                  <button onClick={sales.reload} className="flex items-center gap-1 underline"><RefreshCw className="w-3 h-3" />重试</button>
                </div>
              )}
              {sales.loading && <p role="status" className="text-gray-500 text-sm">加载中…</p>}

              {sales.page && sales.page.records.length === 0 && !sales.loading && !sales.error && (
                <p className="py-8 text-center text-gray-400">暂无售出记录。升级前已发出的卡密不在记录内。</p>
              )}
              {sales.page && sales.page.records.length > 0 && (
                <div className="overflow-x-auto rounded-xl border border-gray-100">
                  <table className="w-full text-left">
                    <thead>
                      <tr className="text-xs text-gray-400 border-b border-gray-100">
                        <th className="px-3 py-2">卡密</th><th className="px-3 py-2">状态</th><th className="px-3 py-2">订单号</th>
                        <th className="px-3 py-2">账号</th><th className="px-3 py-2">时间</th>
                      </tr>
                    </thead>
                    <tbody>{sales.page.records.map(/* record 是当前页的一条售出记录。 */ record => <SaleRow key={record.id} record={record} />)}</tbody>
                  </table>
                </div>
              )}
              {sales.page && (
                <div className="flex items-center justify-between text-sm text-gray-500">
                  <span>共 {sales.page.total} 条</span>
                  <div className="flex items-center gap-2">
                    <button onClick={/* 点击跳转上一页。 */ () => sales.setPage(sales.query.page - 1)} disabled={sales.query.page <= 1}
                      className="px-3 py-1.5 rounded-lg bg-gray-100 disabled:opacity-50">上一页</button>
                    <span>{sales.query.page} / {sales.page.totalPages}</span>
                    <button onClick={/* 点击跳转下一页。 */ () => sales.setPage(sales.query.page + 1)} disabled={sales.query.page >= sales.page.totalPages}
                      className="px-3 py-1.5 rounded-lg bg-gray-100 disabled:opacity-50">下一页</button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
};
