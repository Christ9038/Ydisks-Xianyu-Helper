import React from 'react';
import { createPortal } from 'react-dom';
import { FileDown, ListPlus, Loader2, Plus, Upload, X } from 'lucide-react';
import type { Card } from '../../../../shared/api-contract';
import type { CardBatchModalProps } from '../types';

// BatchCardImportModal 负责卡密批量创建和单组库存追加的交互界面。
export const BatchCardImportModal: React.FC<CardBatchModalProps> = ({
  dataCards,
  downloadCardTemplate,
  ...state
}) => {
  // handleFileChange 保存用户选择的批量导入文件。
  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    state.setBatchFile(event.target.files?.[0] || null);
  };
  // handleAppendContentChange 更新追加文本并保留原始换行，预览由 Hook 派生。
  const handleAppendContentChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => {
    state.setAppendContent(event.target.value);
  };
  // handleTargetChange 切换追加库存目标卡密组。
  const handleTargetChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    state.setAppendTargetId(event.target.value);
  };
  // handleClose 关闭弹窗并取消当前批量请求。
  const handleClose = () => state.closeBatchModal();
  // handleCreateTab 切换到批量创建页签。
  const handleCreateTab = () => state.setBatchTab('create');
  // handleAppendTab 切换到库存追加页签。
  const handleAppendTab = () => state.setBatchTab('append');
  // handleRetryCreate 重试最近一次批量创建。
  const handleRetryCreate = () => void state.handleRetryBatchCreate();
  // handleRetryAppend 重试最近一次库存追加。
  const handleRetryAppend = () => void state.handleRetryBatchAppend();
  // completedBatchResult 只保留包含逐行统计的成功批量结果。
  const completedBatchResult = state.batchResult && 'rows' in state.batchResult ? state.batchResult : null;
  // renderCardOption 渲染追加目标卡密组的下拉选项。
  const renderCardOption = (card: Card) => <option key={card.id} value={String(card.id)}>{card.name}（ID: {card.id}）</option>;
  // handleSubmitCreate 提交批量创建请求。
  const handleSubmitCreate = () => void state.handleBatchCreate();
  // handleSubmitAppend 提交批量追加请求。
  const handleSubmitAppend = () => void state.handleBatchAppend();

  if (!state.showBatchModal) return null;

  return createPortal(
    <div className="modal-overlay">
      <div className="modal-container" style={{ maxWidth: '40rem' }}>
        <div className="px-6 py-5 border-b border-gray-100 flex items-center justify-between">
          <h3 className="text-xl font-extrabold text-gray-900">批量导入卡密</h3>
          <button onClick={handleClose} className="w-10 h-10 rounded-2xl bg-gray-100 hover:bg-gray-200 flex items-center justify-center">
            <X className="w-5 h-5 text-gray-600" />
          </button>
        </div>

        <div className="px-6 py-5 space-y-5">
          <div className="flex flex-wrap gap-2 p-2 bg-gray-100/50 rounded-2xl">
            <button onClick={handleCreateTab} className={`flex-1 inline-flex items-center justify-center gap-2 px-4 py-2.5 rounded-xl text-sm font-bold transition-all ${state.batchTab === 'create' ? 'bg-brand text-white shadow-md' : 'bg-white text-gray-600 hover:bg-gray-50'}`}>
              <ListPlus className="w-4 h-4" />批量创建卡密组
            </button>
            <button onClick={handleAppendTab} className={`flex-1 inline-flex items-center justify-center gap-2 px-4 py-2.5 rounded-xl text-sm font-bold transition-all ${state.batchTab === 'append' ? 'bg-brand text-white shadow-md' : 'bg-white text-gray-600 hover:bg-gray-50'}`}>
              <Upload className="w-4 h-4" />往单个组充卡密
            </button>
          </div>

          {state.batchTab === 'create' ? (
            <div className="space-y-4">
              <div className="rounded-xl bg-blue-50 border border-blue-100 p-4 text-xs text-blue-900 leading-5">
                上传表格，每行一个卡密组。表头：<code className="bg-blue-100/70 px-1.5 py-0.5 rounded">名称,类型,内容,描述,启用,延迟秒,多规格,规格名,规格值</code>。
                类型填 text/data/image；data 类型的“内容”按行存卡密（CSV 单元格内换行需用引号包裹）。
              </div>
              <div className="flex items-center gap-3">
                <button onClick={downloadCardTemplate} className="px-4 py-2.5 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 text-sm transition-colors">
                  <FileDown className="w-4 h-4" />下载模板
                </button>
                <label className="flex-1 px-4 py-2.5 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 text-sm cursor-pointer transition-colors">
                  <Upload className="w-4 h-4" />{state.batchFile ? state.batchFile.name : '选择 .xlsx / .csv / .tsv'}
                  <input type="file" accept=".xlsx,.csv,.tsv" className="hidden" onChange={handleFileChange} />
                </label>
              </div>
              {state.batchResult?.error && (
                <div className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 flex items-center justify-between gap-3">
                  <span>{state.batchResult.error}</span>
                  <button type="button" onClick={handleRetryCreate} disabled={!state.batchFile || state.batchBusy} className="font-bold whitespace-nowrap">重试</button>
                </div>
              )}
              {completedBatchResult && (
                <div className="rounded-xl border border-gray-200 p-4 space-y-2">
                  <div className="text-sm font-bold text-gray-900">共 {completedBatchResult.total} 行 · 成功 {completedBatchResult.created} · 失败 {completedBatchResult.failed}</div>
                  {completedBatchResult.failed > 0 && (
                    <div className="max-h-48 overflow-y-auto space-y-1">
                      {completedBatchResult.rows.filter(
                        // row 是批量结果中的单行处理记录。
                        row => !row.success,
                      ).map(
                        // row 是待展示失败说明的单行结果。
                        row => (
                        <div key={row.row_no} className="text-xs text-red-600">第 {row.row_no} 行「{row.name}」：{row.error}</div>
                        ),
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-4">
              {dataCards.length === 0 ? (
                <div className="rounded-xl bg-amber-50 border border-amber-200 p-4 text-sm text-amber-800">暂无 data（批量卡密）类型的卡密组，请先创建一个再充卡密。</div>
              ) : (
                <>
                  <div className="space-y-2">
                    <label className="block text-sm font-bold text-gray-800">选择卡密组</label>
                    <select value={state.appendTargetId} onChange={handleTargetChange} className="w-full ios-input px-4 py-3 rounded-xl text-sm">
                      {dataCards.map(renderCardOption)}
                    </select>
                  </div>
                  <div className="space-y-2">
                    <label className="block text-sm font-bold text-gray-800">卡密号（每行一个）</label>
                    <textarea value={state.appendContent} onChange={handleAppendContentChange} placeholder={'VIP-001\nVIP-002\nVIP-003'} className="w-full ios-input px-4 py-3 rounded-xl text-sm font-mono h-40 resize-y" />
                    <p className="text-xs text-gray-500">预览：{state.appendPreview.length} 个有效卡密，空行自动忽略。</p>
                  </div>
                  {state.appendError && (
                    <div className="rounded-xl bg-red-50 border border-red-200 p-3 text-sm text-red-700 flex items-center justify-between gap-3">
                      <span>{state.appendError}</span>
                      <button type="button" onClick={handleRetryAppend} disabled={state.batchBusy} className="font-bold whitespace-nowrap">重试</button>
                    </div>
                  )}
                  {state.appendResult && <div className="rounded-xl bg-green-50 border border-green-200 p-3 text-sm text-green-700 font-bold">已追加 {state.appendResult.added} 个卡密</div>}
                </>
              )}
            </div>
          )}
        </div>

        <div className="px-6 py-4 border-t border-gray-100 flex items-center justify-end gap-3">
          <button onClick={handleClose} className="px-5 py-2.5 rounded-xl bg-gray-100 hover:bg-gray-200 font-bold text-gray-700 transition-colors">关闭</button>
          {state.batchTab === 'create' ? (
            <button onClick={handleSubmitCreate} disabled={!state.batchFile || state.batchBusy} className="ios-btn-primary px-6 py-2.5 rounded-xl font-bold flex items-center gap-2 disabled:opacity-50">
              {state.batchBusy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Upload className="w-4 h-4" />}{state.batchBusy ? '处理中...' : '上传创建'}
            </button>
          ) : (
            <button onClick={handleSubmitAppend} disabled={!state.appendTargetId || state.appendPreview.length === 0 || state.batchBusy || dataCards.length === 0} className="ios-btn-primary px-6 py-2.5 rounded-xl font-bold flex items-center gap-2 disabled:opacity-50">
              {state.batchBusy ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}{state.batchBusy ? '处理中...' : '追加卡密'}
            </button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
};
