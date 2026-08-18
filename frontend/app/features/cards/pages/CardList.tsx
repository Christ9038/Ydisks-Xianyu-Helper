import { Copy,CreditCard,Edit,FileText,Image as ImageIcon,Package,Plus,Save,Search,SlidersHorizontal,Trash2,Upload,X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import { Card } from '../api';
import { useCardActions } from '../cardActions';
import { BatchCardImportModal } from '../components/BatchCardImportModal';
import { CardIcon } from '../components/CardIcon';
import { useCardBatchActions,useCardsData } from '../hooks';

// CardList 渲染卡密列表组件。
const CardList: React.FC = () => {
  // { 解构得到当前 Hook 返回的状态和操作函数。
  const { cards, loadCards } = useCardsData();
  // cardActions 集中管理卡密编辑、新增、删除、筛选和模板动作。
  const cardActions = useCardActions({ cards, loadCards });
  // actionState 解构得到卡密页面动作协调器状态和方法。
  const {
    dataCards,
    filteredCards,
    typeFilter,
    setTypeFilter,
    nameSearch,
    setNameSearch,
    showEditModal,
    setShowEditModal,
    showAddModal,
    setShowAddModal,
    selectedCard,
    editForm,
    setEditForm,
    addForm,
    setAddForm,
    handleEdit,
    handleSaveEdit,
    handleDelete,
    handleAddCard,
    toggleCardStatus,
    copyCardID,
    downloadCardTemplate,
  } = cardActions;
  // batchState 批量发布状态。
  const batchState = useCardBatchActions({ dataCards, loadCards });

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col md:flex-row justify-between md:items-end gap-4">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">卡密库存</h2>
          <p className="text-gray-500 mt-2 font-medium">管理自动发货的卡密、链接或图片资源。</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={batchState.openBatchModal}
            className="px-4 py-3 bg-gray-100 hover:bg-gray-200 rounded-2xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <Upload className="w-5 h-5" />
            批量导入
          </button>
          <button
            onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowAddModal(true)}
            className="ios-btn-primary flex items-center gap-2 px-6 py-3 rounded-2xl font-bold shadow-lg shadow-blue-200 transition-transform hover:scale-105 active:scale-95"
        >
          <Plus className="w-5 h-5" />
          添加新卡密
        </button>
        </div>
      </div>

      <div className="ios-card rounded-xl overflow-hidden shadow-lg border-0 bg-white">
        <div className="flex flex-col gap-3 border-b border-gray-50 bg-surface-muted p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-1 flex-col gap-3 sm:flex-row">
            <div className="relative sm:w-48">
              <SlidersHorizontal className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
              <select
                aria-label="按卡密类型筛选"
                value={typeFilter}
                onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setTypeFilter(event.target.value as Card['type'] | '')}
                className="ios-input w-full rounded-xl border-none bg-white py-2.5 pl-10 pr-9 text-sm shadow-sm"
              >
                <option value="">全部类型</option>
                <option value="data">批量卡密</option>
                <option value="text">文本</option>
                <option value="api">API</option>
                <option value="image">图片</option>
              </select>
            </div>
            <div className="relative w-full sm:max-w-sm">
              <Search className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
              <input
                type="search"
                aria-label="按卡密名称搜索"
                placeholder="搜索卡密名称..."
                value={nameSearch}
                onChange={/* 当前回调处理用户交互或异步状态变化。 */ event => setNameSearch(event.target.value)}
                className="ios-input w-full rounded-xl border-none bg-white py-2.5 pl-10 pr-4 text-sm shadow-sm"
              />
            </div>
          </div>
          <div className="whitespace-nowrap px-1 text-xs font-bold text-gray-400">
            显示 {filteredCards.length} / {cards.length} 组
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full table-fixed text-left border-collapse">
            <thead>
              <tr className="bg-white text-gray-400 text-xs font-bold uppercase tracking-wider border-b border-gray-50">
                <th className="w-[23%] px-5 py-5">卡密名称</th>
                <th className="w-[8%] px-3 py-5">卡密组ID</th>
                <th className="w-[7%] px-2 py-5">类型</th>
                <th className="w-[27%] px-5 py-5">内容/库存</th>
                <th className="w-[19%] px-5 py-5">描述</th>
                <th className="w-[7%] px-2 py-5">状态</th>
                <th className="w-[9%] px-3 py-5 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-50">
              {filteredCards.map(/* 当前回调处理集合中的单个元素。 */ (card) => {
                // 计算库存或内容预览
                let stockInfo = '';
                if (card.type === 'data' && card.data_content) {
                  // lines 文本行列表。
                  const lines = card.data_content.split('\n').filter(/* 当前回调处理集合中的单个元素。 */ line => line.trim());
                  stockInfo = `库存: ${lines.length} 条`;
                } else if (card.type === 'text' && card.text_content) {
                  stockInfo = card.text_content;
                } else if (card.type === 'api' && card.api_config) {
                  stockInfo = card.api_config.url;
                } else if (card.type === 'image' && card.image_url) {
                  stockInfo = '图片链接';
                }

                return (
                  <tr key={card.id} className="hover:bg-warning-50/50 transition-colors group">
                    <td className="px-5 py-5 align-middle">
                      <div className="flex items-center gap-2.5">
                        <div className="shrink-0 rounded-xl bg-gray-50 p-2 transition-colors group-hover:bg-white">
                          <CardIcon type={card.type} />
                        </div>
                        <span className="line-clamp-3 min-w-0 text-[13px] font-bold leading-5 text-gray-900" title={card.name}>{card.name}</span>
                      </div>
                    </td>
                    <td className="px-3 py-5">
                      <button
                        onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => copyCardID(card.id)}
                        className="inline-flex max-w-full items-center gap-1 rounded-lg bg-gray-100 px-2 py-1.5 font-mono text-[11px] font-extrabold text-gray-700 transition-colors hover:bg-gray-200"
                        title="复制卡密组ID，用于批量铺货表格"
                      >
                        <span className="truncate">{card.id}</span>
                        <Copy className="h-3 w-3 shrink-0" />
                      </button>
                    </td>
                    <td className="px-2 py-5">
                      <span className={`inline-flex rounded-md px-2 py-1 text-[11px] font-bold ${
                        card.type === 'text' ? 'bg-blue-50 text-blue-600' :
                        card.type === 'data' ? 'bg-purple-50 text-purple-600' :
                        card.type === 'api' ? 'bg-blue-50 text-blue-600' :
                        'bg-pink-50 text-pink-600'
                      }`}>
                        {card.type === 'text' ? '文本' :
                         card.type === 'data' ? '批量' :
                         card.type === 'api' ? 'API' : '图片'}
                      </span>
                    </td>
                    <td className="px-5 py-5">
                      <span className="line-clamp-3 break-all font-mono text-sm leading-5 text-gray-600" title={stockInfo}>
                        {stockInfo}
                      </span>
                    </td>
                    <td className="px-5 py-5">
                      <span
                        className="block truncate text-sm text-gray-500"
                        title={card.description || '-'}
                      >
                        {card.description || '-'}
                      </span>
                    </td>
                    <td className="px-2 py-5">
                      <button
                        onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => toggleCardStatus(card)}
                        aria-label={`切换卡密 ${card.name} 状态`}
                        className={`w-12 h-8 rounded-full relative transition-colors ${
                          card.enabled ? 'bg-green-500' : 'bg-gray-300'
                        }`}
                      >
                        <div className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-sm transition-transform ${
                          card.enabled ? 'left-5' : 'left-1'
                        }`}></div>
                      </button>
                    </td>
                    <td className="px-3 py-5">
                      <div className="flex items-center justify-end gap-0.5">
                        <button
                          onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => handleEdit(card)}
                          className="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-black"
                          title="编辑"
                        >
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => handleDelete(card.id)}
                          aria-label={`删除卡密 ${card.name}`}
                          className="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-red-50 hover:text-red-500"
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {filteredCards.length === 0 && (
          <div className="py-20 text-center text-gray-400">
            <Package className="w-12 h-12 mx-auto mb-4 opacity-30" />
            <p>{cards.length === 0 ? '暂无卡密配置，请点击右上角添加。' : '没有符合当前筛选条件的卡密组。'}</p>
          </div>
        )}
      </div>

      {/* 编辑卡密弹窗 - 使用 Portal */}
      {showEditModal && selectedCard && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container">
            <div className="modal-header">
              <h3 className="text-2xl font-extrabold text-gray-900">编辑卡密</h3>
              <button
                onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowEditModal(false)}
                className="p-2 rounded-xl hover:bg-gray-100 transition-colors"
              >
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>

            <div className="modal-body">
              <div className="space-y-5">
                {/* 基本信息 */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">卡密名称 <span className="text-red-500">*</span></label>
                    <input
                      type="text"
                      value={editForm.name || ''}
                      onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, name: e.target.value })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                      placeholder="例如：游戏点卡、会员卡等"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">卡券类型</label>
                    <select
                      value={editForm.type || 'text'}
                      onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, type: e.target.value as any })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                    >
                      <option value="">请选择类型</option>
                      <option value="text">固定文字</option>
                      <option value="data">批量数据</option>
                      {selectedCard?.type === 'api' && <option value="api">API接口（仅保留现有配置）</option>}
                      <option value="image">图片</option>
                    </select>
                  </div>
                </div>

                {/* API 配置 */}
                {editForm.type === 'api' && (
                  <div className="border border-gray-200 rounded-xl p-4 space-y-4 bg-gray-50">
                    <h3 className="font-bold text-gray-900">API 配置</h3>
                    <div>
                      <label className="block text-sm font-bold text-gray-700 mb-2">API 地址</label>
                      <input
                        type="url"
                        value={editForm.api_url || ''}
                        onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, api_url: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl font-mono text-sm"
                        placeholder="https://api.example.com/get-card"
                      />
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <label className="block text-sm font-bold text-gray-700 mb-2">请求方法</label>
                        <select
                          value={editForm.api_method || 'GET'}
                          onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, api_method: e.target.value as 'GET' | 'POST' })}
                          className="w-full ios-input px-4 py-3 rounded-xl"
                        >
                          <option value="GET">GET</option>
                          <option value="POST">POST</option>
                        </select>
                      </div>
                      <div>
                        <label className="block text-sm font-bold text-gray-700 mb-2">超时时间（秒）</label>
                        <input
                          type="number"
                          value={editForm.api_timeout || 10}
                          onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, api_timeout: parseInt(e.target.value) || 10 })}
                          className="w-full ios-input px-4 py-3 rounded-xl"
                          min="1"
                          max="60"
                        />
                      </div>
                    </div>
                    <div>
                      <label className="block text-sm font-bold text-gray-700 mb-2">请求头（JSON 格式）</label>
                      <textarea
                        value={editForm.api_headers || ''}
                        onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, api_headers: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl h-20 resize-none font-mono text-sm"
                        placeholder='{"Authorization": "Bearer token"}'
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-bold text-gray-700 mb-2">请求参数（JSON 格式）</label>
                      <textarea
                        value={editForm.api_params || ''}
                        onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, api_params: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl h-20 resize-none font-mono text-sm"
                        placeholder='{"type": "card", "count": 1}'
                      />
                    </div>
                  </div>
                )}

                {/* 固定文字配置 */}
                {editForm.type === 'text' && (
                  <div className="border border-gray-200 rounded-xl p-4 bg-gray-50">
                    <h3 className="font-bold text-gray-900 mb-3">固定文字配置</h3>
                    <div>
                      <label className="block text-sm font-bold text-gray-700 mb-2">文字内容</label>
                      <textarea
                        value={editForm.text_content || ''}
                        onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, text_content: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none"
                        placeholder="请输入要发送的固定文字内容..."
                      />
                    </div>
                  </div>
                )}

                {/* 批量数据配置 */}
                {editForm.type === 'data' && (
                  <div className="border border-gray-200 rounded-xl p-4 bg-gray-50">
                    <h3 className="font-bold text-gray-900 mb-3">批量数据配置</h3>
                    <div>
                      <label className="block text-sm font-bold text-gray-700 mb-2">数据内容（一行一个）</label>
                      <textarea
                        value={editForm.data_content || ''}
                        onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, data_content: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl h-80 resize-none font-mono text-sm"
                        placeholder="请输入数据，每行一个：&#10;卡号1:密码1&#10;卡号2:密码2&#10;或者&#10;兑换码1&#10;兑换码2"
                      />
                      <p className="text-xs text-gray-500 mt-2">支持格式：卡号:密码 或 单独的兑换码</p>
                      <p className="text-xs text-gray-500">当前库存：<span className="font-bold text-blue-600">
                        {editForm.data_content ? editForm.data_content.split('\n').filter(/* 当前回调处理集合中的单个元素。 */ line => line.trim()).length : 0}
                      </span> 条</p>
                    </div>
                  </div>
                )}

                {/* 图片配置 */}
                {editForm.type === 'image' && (
                  <div className="border border-gray-200 rounded-xl p-4 bg-gray-50">
                    <h3 className="font-bold text-gray-900 mb-3">图片配置</h3>
                    <div>
                      <label className="block text-sm font-bold text-gray-700 mb-2">图片 URL</label>
                      <input
                        type="url"
                        value={editForm.image_url || ''}
                        onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, image_url: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl font-mono text-sm"
                        placeholder="https://example.com/image.png"
                      />
                      <p className="text-xs text-gray-500 mt-2">仅保存图片 URL；发货时会临时下载并上传到闲鱼</p>
                    </div>
                    {editForm.image_url && (
                      <div className="mt-3">
                        <label className="block text-sm font-bold text-gray-700 mb-2">图片预览</label>
                        <img
                          src={editForm.image_url}
                          alt="预览"
                          className="max-w-full max-h-48 rounded-xl border border-gray-200"
                          onError={/* 当前回调处理用户交互或异步状态变化。 */ (e) => { e.currentTarget.src = 'https://via.placeholder.com/400x200?text=图片加载失败'; }}
                        />
                      </div>
                    )}
                  </div>
                )}

                {/* 延时发货时间 */}
                <div>
                  <label className="block text-sm font-bold text-gray-700 mb-2">延时发货时间（秒）</label>
                  <div className="flex items-center gap-2">
                    <input
                      type="number"
                      value={editForm.delay_seconds || 0}
                      onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, delay_seconds: parseInt(e.target.value) || 0 })}
                      className="flex-1 ios-input px-4 py-3 rounded-xl"
                      min="0"
                      max="3600"
                      placeholder="0"
                    />
                    <span className="text-sm text-gray-500 whitespace-nowrap">秒</span>
                  </div>
                  <p className="text-xs text-gray-500 mt-1">0表示立即发货，最大3600秒（1小时）</p>
                </div>

                {/* 备注信息 */}
                <div>
                  <label className="block text-sm font-bold text-gray-700 mb-2">备注信息</label>
                  <textarea
                    value={editForm.description || ''}
                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setEditForm({ ...editForm, description: e.target.value })}
                    className="w-full ios-input px-4 py-3 rounded-xl h-40 resize-none"
                    placeholder="可选的备注信息"
                  />
                </div>

                {/* 启用状态 */}
                <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                  <span className="font-bold text-gray-900">启用状态</span>
                  <button
                    type="button"
                    onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setEditForm({ ...editForm, enabled: !editForm.enabled })}
                    className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${
                      editForm.enabled ? 'bg-brand' : 'bg-gray-300'
                    }`}
                  >
                    <span
                      className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 block ${
                        editForm.enabled ? 'translate-x-7' : 'translate-x-1'
                      }`}
                    />
                  </button>
                </div>
              </div>
            </div>

            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button
                  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowEditModal(false)}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={handleSaveEdit}
                  className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
                >
                  <Save className="w-4 h-4" />
                  保存更改
                </button>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* 添加新卡密弹窗 - 使用 Portal */}
      {showAddModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '780px'}}>
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900">添加新卡密</h3>
                <p className="text-sm text-gray-500 mt-1">选择交付方式并录入自动发货内容</p>
              </div>
              <button
                onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowAddModal(false)}
                className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0"
                title="关闭"
              >
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>

            <div className="modal-body">
              <div className="space-y-6">
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700 mb-2">卡密名称</label>
                  <input
                    type="text"
                    value={addForm.name}
                    onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setAddForm({ ...addForm, name: e.target.value })}
                    placeholder="例如：VIP会员卡密"
                    className="w-full ios-input px-4 py-3 rounded-xl"
                  />
                </div>

                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700 mb-2">类型</label>
                  <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                    <button
                      type="button"
                      onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setAddForm({ ...addForm, type: 'data', content: '' })}
                      className={`p-3 rounded-xl font-bold transition-all ${addForm.type === 'data' ? 'bg-brand text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}
                    >
                      <CreditCard className="w-5 h-5 mx-auto mb-1" />
                      批量库存
                    </button>
                    <button
                      type="button"
                      onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setAddForm({ ...addForm, type: 'text', content: '' })}
                      className={`p-3 rounded-xl font-bold transition-all ${addForm.type === 'text' ? 'bg-brand text-white' : 'bg-gray-100 text-gray-600'}`}
                    >
                      <FileText className="w-5 h-5 mx-auto mb-1" />
                      文本
                    </button>
                    <button
                      type="button"
                      onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setAddForm({ ...addForm, type: 'image', content: '' })}
                      className={`p-3 rounded-xl font-bold transition-all ${addForm.type === 'image' ? 'bg-brand text-white' : 'bg-gray-100 text-gray-600'}`}
                    >
                      <ImageIcon className="w-5 h-5 mx-auto mb-1" />
                      图片
                    </button>
                  </div>
                </div>

                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700 mb-2">
                    {addForm.type === 'data' ? '库存内容（一行一个）' : addForm.type === 'text' ? '固定回复内容' : addForm.type === 'image' ? '图片 URL' : 'API 地址'}
                  </label>
                  {addForm.type === 'api' ? (
                    <input
                      type="url"
                      value={addForm.content}
                      onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setAddForm({ ...addForm, content: e.target.value })}
                      placeholder="https://api.example.com/get-code"
                      className="w-full ios-input px-4 py-3 rounded-xl"
                    />
                  ) : addForm.type === 'image' ? (
                    <div className="space-y-2">
                      <input
                        type="url"
                        value={addForm.content}
                        onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setAddForm({ ...addForm, content: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl"
                        placeholder="https://example.com/card.png"
                      />
                      <p className="text-xs text-gray-500">仅保存图片 URL；发货时会临时下载并上传到闲鱼</p>
                    </div>
                  ) : (
                    <textarea
                      value={addForm.content}
                      onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setAddForm({ ...addForm, content: e.target.value })}
                      className={`w-full ios-input px-4 py-3 rounded-xl resize-none text-sm ${addForm.type === 'data' ? 'h-48 font-mono' : 'h-32'}`}
                      placeholder={addForm.type === 'data' ? 'CODE-123456\nCODE-789012\n...' : '请输入每次发货时发送的固定文字'}
                    />
                  )}
                  {addForm.type === 'data' && (
                    <p className="text-xs text-gray-500">当前库存：<span className="font-bold text-brand">{addForm.content.split('\n').filter(/* 当前回调处理集合中的单个元素。 */ line => line.trim()).length}</span> 条</p>
                  )}
                </div>

                {addForm.type === 'api' && (
                  <div className="border-t border-gray-100 pt-5 space-y-4">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">请求方法</label>
                        <select value={addForm.api_method} onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setAddForm({...addForm, api_method: e.target.value as 'GET' | 'POST'})} className="w-full ios-input px-4 py-3 rounded-xl">
                          <option value="GET">GET</option>
                          <option value="POST">POST</option>
                        </select>
                      </div>
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">超时时间（秒）</label>
                        <input type="number" min="1" max="60" value={addForm.api_timeout} onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setAddForm({...addForm, api_timeout: parseInt(e.target.value) || 10})} className="w-full ios-input px-4 py-3 rounded-xl" />
                      </div>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">请求头（JSON）</label>
                        <textarea value={addForm.api_headers} onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setAddForm({...addForm, api_headers: e.target.value})} className="w-full ios-input px-4 py-3 rounded-xl h-24 resize-none font-mono text-xs" placeholder='{"Authorization":"Bearer token"}' />
                      </div>
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">请求参数（JSON）</label>
                        <textarea value={addForm.api_params} onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setAddForm({...addForm, api_params: e.target.value})} className="w-full ios-input px-4 py-3 rounded-xl h-24 resize-none font-mono text-xs" placeholder='{"order_id":"{order_id}"}' />
                      </div>
                    </div>
                  </div>
                )}

                <div className="grid grid-cols-1 sm:grid-cols-[1fr_180px] gap-4">
                  <div className="space-y-2">
                    <label className="block text-sm font-bold text-gray-700">描述</label>
                    <input value={addForm.description} onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setAddForm({...addForm, description: e.target.value})} placeholder="卡密用途描述（可选）" className="w-full ios-input px-4 py-3 rounded-xl" />
                  </div>
                  <div className="space-y-2">
                    <label className="block text-sm font-bold text-gray-700">延时发货（秒）</label>
                    <input type="number" value={addForm.delay_seconds} onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setAddForm({...addForm, delay_seconds: parseInt(e.target.value) || 0})} className="w-full ios-input px-4 py-3 rounded-xl" min="0" max="3600" />
                  </div>
                </div>

              </div>
            </div>

            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button
                  onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setShowAddModal(false)}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={handleAddCard}
                  className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
                >
                  <Plus className="w-4 h-4" />
                  添加卡密
                </button>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* 批量导入弹窗由 cards feature 组件负责渲染和请求边界。 */}
      <BatchCardImportModal
        dataCards={dataCards}
        downloadCardTemplate={downloadCardTemplate}
        {...batchState}
      />


    </div>
  );
};

export default CardList;
