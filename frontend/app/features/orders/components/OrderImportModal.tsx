import React from 'react';
import { createPortal } from 'react-dom';
import { Upload, X } from 'lucide-react';
import { failedOrderImportRows } from '../state';
import type { OrderImportState } from '../types';

// OrderImportModalProps 描述订单导入弹窗所需的状态和事件。
export type OrderImportModalProps = OrderImportState;

// OrderImportModal 渲染订单文件选择、结果展示和重试操作。
export const OrderImportModal: React.FC<OrderImportModalProps> = (state) => {
  // handleFileChange 保存用户选择的订单文件并清理旧错误。
  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    state.setImportFile(event.target.files?.[0] || null);
  };
  // handleClose 关闭订单导入弹窗。
  const handleClose = () => state.closeImportModal();
  // handleSubmit 提交当前订单导入文件。
  const handleSubmit = () => void state.handleImportOrders();
  // handleRetry 重试最近一次失败的订单导入。
  const handleRetry = () => void state.handleRetryImport();

  if (!state.showImportModal) return null;

  return createPortal(
    <div className="modal-overlay-centered">
      <div className="modal-container">
        <div className="modal-header">
          <div className="flex items-center justify-between w-full">
            <h3 className="text-2xl font-extrabold text-gray-900">插入订单</h3>
            <button onClick={handleClose} className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors">
              <X className="w-5 h-5 text-gray-600" />
            </button>
          </div>
        </div>
        <div className="modal-body space-y-5">
          <div>
            <label className="block text-sm font-bold text-gray-700 mb-2">选择Excel文件</label>
            <input type="file" accept=".xlsx,.csv,.tsv,.json" onChange={handleFileChange} className="w-full ios-input px-4 py-3 rounded-xl text-sm" />
            <p className="text-xs text-gray-500 mt-2">支持 .xlsx、.csv、.tsv、.json 格式</p>
          </div>
          {state.importFile && (
            <div className="p-3 bg-blue-50 rounded-xl">
              <div className="flex items-center gap-2">
                <Upload className="w-4 h-4 text-blue-600" />
                <span className="text-sm font-medium text-blue-900">{state.importFile.name}</span>
              </div>
            </div>
          )}
          {state.importError && (
            <div className="rounded-xl bg-red-50 border border-red-100 p-3 text-sm text-red-700 flex items-center justify-between gap-3">
              <span>{state.importError}</span>
              <button type="button" onClick={handleRetry} disabled={state.importing || !state.importFile} className="font-bold whitespace-nowrap">重试</button>
            </div>
          )}
          {state.importResult && state.importResult.failed_count > 0 && (
            <div className="space-y-3">
              <div className="rounded-xl bg-amber-50 border border-amber-100 p-3 text-sm font-bold text-amber-800">
                导入完成：成功 {state.importResult.success_count} 条，失败 {state.importResult.failed_count} 条
              </div>
              <div className="max-h-64 overflow-y-auto rounded-xl border border-gray-100">
                <table className="w-full text-left text-xs">
                  <thead className="sticky top-0 bg-gray-50 text-gray-500">
                    <tr><th className="px-3 py-2">订单ID</th><th className="px-3 py-2">失败原因</th></tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100">
                    {failedOrderImportRows(state.importResult).map(
                      // row 是需要展示失败原因的导入结果行。
                      (row, index) => (
                        <tr key={`${row.order_id}-${index}`}>
                          <td className="px-3 py-2 font-mono">{row.order_id}</td>
                          <td className="px-3 py-2 text-red-600">{row.message || '导入失败'}</td>
                        </tr>
                      ),
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
        <div className="modal-footer">
          <div className="flex gap-3 w-full">
            <button onClick={handleClose} className="flex-1 px-6 py-3 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-800 font-bold transition-colors">取消</button>
            <button onClick={handleSubmit} disabled={!state.importFile || state.importing} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold shadow-lg shadow-blue-200 disabled:opacity-50">
              {state.importing ? '正在导入…' : '导入订单'}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
};
