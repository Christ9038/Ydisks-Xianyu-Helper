import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { Card } from '../types';
import { getCards, createCard, updateCard, deleteCard } from '../services/api';
import { Plus, CreditCard, Clock, FileText, Image as ImageIcon, Code, Edit, Trash2, Save, X, Eye, EyeOff, Package } from 'lucide-react';

type AddCardForm = {
  name: string;
  type: Card['type'];
  content: string;
  description: string;
  enabled: boolean;
  delay_seconds: number;
  api_method: 'GET' | 'POST';
  api_timeout: number;
  api_headers: string;
  api_params: string;
};

const emptyAddForm = (): AddCardForm => ({
  name: '',
  type: 'data',
  content: '',
  description: '',
  enabled: true,
  delay_seconds: 0,
  api_method: 'GET',
  api_timeout: 10,
  api_headers: '',
  api_params: '',
});

const CardList: React.FC = () => {
  const [cards, setCards] = useState<Card[]>([]);
  const [showEditModal, setShowEditModal] = useState(false);
  const [showAddModal, setShowAddModal] = useState(false);
  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  const [editForm, setEditForm] = useState<Partial<Card>>({});
  const [addForm, setAddForm] = useState<AddCardForm>(emptyAddForm);

  useEffect(() => {
    getCards().then(setCards);
  }, []);

  const CardIcon = ({ type }: { type: string }) => {
      switch(type) {
          case 'text': return <FileText className="w-5 h-5 text-blue-500" />;
          case 'image': return <ImageIcon className="w-5 h-5 text-purple-500" />;
          case 'api': return <Code className="w-5 h-5 text-blue-500" />;
          default: return <CreditCard className="w-5 h-5 text-gray-500" />;
      }
  };

  const handleEdit = (card: Card) => {
    setSelectedCard(card);
    setEditForm({
      id: card.id,
      name: card.name || '',
      type: card.type || 'text',
      // API 配置
      api_url: card.api_config?.url || '',
      api_method: card.api_config?.method || 'GET',
      api_timeout: card.api_config?.timeout || 10,
      api_headers: card.api_config?.headers || '',
      api_params: card.api_config?.params || '',
      // 文本配置
      text_content: card.text_content || '',
      // 批量数据配置
      data_content: card.data_content || '',
      // 图片配置
      image_url: card.image_url || '',
      // 通用配置
      delay_seconds: card.delay_seconds || 0,
      description: card.description || '',
      enabled: card.enabled
    });
    setShowEditModal(true);
  };

  const handleSaveEdit = async () => {
    if (!selectedCard) return;

    // 验证必填字段
    if (!editForm.name?.trim()) {
      alert('请输入卡密名称');
      return;
    }
    if (!editForm.type) {
      alert('请选择卡密类型');
      return;
    }

    try {
      const updateData: Partial<Card> = {
        name: editForm.name.trim(),
        type: editForm.type as any,
        description: editForm.description?.trim(),
        delay_seconds: editForm.delay_seconds || 0,
        enabled: editForm.enabled ?? true,
      };

      // 根据类型设置内容
      if (editForm.type === 'api') {
        updateData.api_config = {
          url: editForm.api_url?.trim(),
          method: editForm.api_method as 'GET' | 'POST',
          timeout: editForm.api_timeout || 10,
          headers: editForm.api_headers?.trim() || undefined,
          params: editForm.api_params?.trim() || undefined
        };
      } else if (editForm.type === 'text') {
        updateData.text_content = editForm.text_content?.trim() || '';
      } else if (editForm.type === 'data') {
        updateData.data_content = editForm.data_content?.trim() || '';
      } else if (editForm.type === 'image') {
        updateData.image_url = editForm.image_url?.trim() || '';
      }

      await updateCard(selectedCard.id, updateData);
      setShowEditModal(false);
      getCards().then(setCards);
    } catch (error) {
      console.error('更新卡密失败:', error);
      alert('更新失败，请重试');
    }
  };

  const handleDelete = async (id: string | number) => {
    if (confirm('确认删除该卡密吗？')) {
      try {
        await deleteCard(id);
        getCards().then(setCards);
      } catch (error) {
        console.error('删除卡密失败:', error);
        alert('删除失败，请重试');
      }
    }
  };

  const handleAddCard = async () => {
    if (!addForm.name.trim()) {
      alert('请输入卡密名称');
      return;
    }
    if (!addForm.content.trim()) {
      alert(addForm.type === 'api' ? '请输入 API 地址' : '请输入卡密内容');
      return;
    }
    try {
      const payload: Partial<Card> = {
        name: addForm.name.trim(),
        type: addForm.type,
        description: addForm.description.trim(),
        enabled: addForm.enabled,
        delay_seconds: addForm.delay_seconds,
      };
      if (addForm.type === 'text') payload.text_content = addForm.content.trim();
      if (addForm.type === 'data') payload.data_content = addForm.content.trim();
      if (addForm.type === 'image') payload.image_url = addForm.content.trim();
      if (addForm.type === 'api') {
        payload.api_config = {
          url: addForm.content.trim(),
          method: addForm.api_method,
          timeout: addForm.api_timeout,
          headers: addForm.api_headers.trim() || undefined,
          params: addForm.api_params.trim() || undefined,
        };
      }
      await createCard(payload);
      setShowAddModal(false);
      setAddForm(emptyAddForm());
      getCards().then(setCards);
    } catch (error) {
      console.error('添加卡密失败:', error);
      alert('添加失败，请重试');
    }
  };

  const toggleCardStatus = async (card: Card) => {
    try {
      await updateCard(card.id, { ...card, enabled: !card.enabled });
      getCards().then(setCards);
    } catch (error) {
      console.error('切换状态失败:', error);
    }
  };

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex justify-between items-center">
        <div>
          <h2 className="text-3xl font-bold text-gray-900">卡密库存</h2>
          <p className="text-gray-500 mt-2 text-sm">管理自动发货的卡密、链接或图片资源。</p>
        </div>
        <button
            onClick={() => setShowAddModal(true)}
            className="ios-btn-primary flex items-center gap-2 px-6 py-3 rounded-2xl font-bold shadow-lg shadow-blue-200 transition-transform hover:scale-105 active:scale-95"
        >
          <Plus className="w-5 h-5" />
          添加新卡密
        </button>
      </div>

      <div className="ios-card rounded-[2rem] overflow-hidden shadow-lg border-0 bg-white">
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="bg-white text-gray-400 text-xs font-bold uppercase tracking-wider border-b border-gray-50">
                <th className="px-8 py-5 w-[15%]">卡密名称</th>
                <th className="px-6 py-5 w-[12%]">类型</th>
                <th className="px-6 py-5 w-[25%]">内容/库存</th>
                <th className="px-6 py-5 w-[20%]">描述</th>
                <th className="px-6 py-5 w-[10%]">状态</th>
                <th className="px-6 py-5 w-[10%] text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-50">
              {cards.map((card) => {
                // 计算库存或内容预览
                let stockInfo = '';
                if (card.type === 'data' && card.data_content) {
                  const lines = card.data_content.split('\n').filter(line => line.trim());
                  stockInfo = `库存: ${lines.length} 条`;
                } else if (card.type === 'text' && card.text_content) {
                  stockInfo = card.text_content.substring(0, 20) + (card.text_content.length > 20 ? '...' : '');
                } else if (card.type === 'api' && card.api_config) {
                  stockInfo = card.api_config.url;
                } else if (card.type === 'image' && card.image_url) {
                  stockInfo = '图片链接';
                }

                return (
                  <tr key={card.id} className="hover:bg-[#FFFDE7]/50 transition-colors group">
                    <td className="px-8 py-5">
                      <div className="flex items-center gap-3">
                        <div className="p-2 bg-gray-50 rounded-xl group-hover:bg-white transition-colors">
                          <CardIcon type={card.type} />
                        </div>
                        <span className="font-bold text-gray-900">{card.name}</span>
                      </div>
                    </td>
                    <td className="px-6 py-5">
                      <span className={`px-3 py-1.5 rounded-lg text-xs font-bold ${
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
                    <td className="px-6 py-5">
                      <span className="text-sm text-gray-600 font-mono block truncate" title={stockInfo}>
                        {stockInfo}
                      </span>
                    </td>
                    <td className="px-6 py-5">
                      <span
                        className="text-sm text-gray-500 block max-w-[200px] truncate"
                        title={card.description || '-'}
                      >
                        {card.description || '-'}
                      </span>
                    </td>
                    <td className="px-6 py-5">
                      <button
                        onClick={() => toggleCardStatus(card)}
                        className={`w-12 h-8 rounded-full relative transition-colors ${
                          card.enabled ? 'bg-green-500' : 'bg-gray-300'
                        }`}
                      >
                        <div className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-sm transition-transform ${
                          card.enabled ? 'left-5' : 'left-1'
                        }`}></div>
                      </button>
                    </td>
                    <td className="px-6 py-5">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={() => handleEdit(card)}
                          className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors"
                          title="编辑"
                        >
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleDelete(card.id)}
                          className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors"
                        >
                          <Trash2 className="w-5 h-5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {cards.length === 0 && (
          <div className="py-20 text-center text-gray-400">
            <Package className="w-12 h-12 mx-auto mb-4 opacity-30" />
            <p>暂无卡密配置，请点击右上角添加。</p>
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
                onClick={() => setShowEditModal(false)}
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
                      onChange={(e) => setEditForm({ ...editForm, name: e.target.value })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                      placeholder="例如：游戏点卡、会员卡等"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2">卡券类型</label>
                    <select
                      value={editForm.type || 'text'}
                      onChange={(e) => setEditForm({ ...editForm, type: e.target.value as any })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                    >
                      <option value="">请选择类型</option>
                      <option value="text">固定文字</option>
                      <option value="data">批量数据</option>
                      <option value="api">API接口</option>
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
                        onChange={(e) => setEditForm({ ...editForm, api_url: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl font-mono text-sm"
                        placeholder="https://api.example.com/get-card"
                      />
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <label className="block text-sm font-bold text-gray-700 mb-2">请求方法</label>
                        <select
                          value={editForm.api_method || 'GET'}
                          onChange={(e) => setEditForm({ ...editForm, api_method: e.target.value as 'GET' | 'POST' })}
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
                          onChange={(e) => setEditForm({ ...editForm, api_timeout: parseInt(e.target.value) || 10 })}
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
                        onChange={(e) => setEditForm({ ...editForm, api_headers: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl h-20 resize-none font-mono text-sm"
                        placeholder='{"Authorization": "Bearer token"}'
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-bold text-gray-700 mb-2">请求参数（JSON 格式）</label>
                      <textarea
                        value={editForm.api_params || ''}
                        onChange={(e) => setEditForm({ ...editForm, api_params: e.target.value })}
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
                        onChange={(e) => setEditForm({ ...editForm, text_content: e.target.value })}
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
                        onChange={(e) => setEditForm({ ...editForm, data_content: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl h-80 resize-none font-mono text-sm"
                        placeholder="请输入数据，每行一个：&#10;卡号1:密码1&#10;卡号2:密码2&#10;或者&#10;兑换码1&#10;兑换码2"
                      />
                      <p className="text-xs text-gray-500 mt-2">支持格式：卡号:密码 或 单独的兑换码</p>
                      <p className="text-xs text-gray-500">当前库存：<span className="font-bold text-blue-600">
                        {editForm.data_content ? editForm.data_content.split('\n').filter(line => line.trim()).length : 0}
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
                        onChange={(e) => setEditForm({ ...editForm, image_url: e.target.value })}
                        className="w-full ios-input px-4 py-3 rounded-xl font-mono text-sm"
                        placeholder="https://example.com/image.png"
                      />
                      <p className="text-xs text-gray-500 mt-2">输入图片卡密的 URL 地址</p>
                    </div>
                    {editForm.image_url && (
                      <div className="mt-3">
                        <label className="block text-sm font-bold text-gray-700 mb-2">图片预览</label>
                        <img
                          src={editForm.image_url}
                          alt="预览"
                          className="max-w-full max-h-48 rounded-xl border border-gray-200"
                          onError={(e) => { e.currentTarget.src = 'https://via.placeholder.com/400x200?text=图片加载失败'; }}
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
                      onChange={(e) => setEditForm({ ...editForm, delay_seconds: parseInt(e.target.value) || 0 })}
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
                    onChange={(e) => setEditForm({ ...editForm, description: e.target.value })}
                    className="w-full ios-input px-4 py-3 rounded-xl h-40 resize-none"
                    placeholder="可选的备注信息"
                  />
                </div>

                {/* 启用状态 */}
                <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                  <span className="font-bold text-gray-900">启用状态</span>
                  <button
                    type="button"
                    onClick={() => setEditForm({ ...editForm, enabled: !editForm.enabled })}
                    className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${
                      editForm.enabled ? 'bg-[#0094f7]' : 'bg-gray-300'
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
                  onClick={() => setShowEditModal(false)}
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
                onClick={() => setShowAddModal(false)}
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
                    onChange={(e) => setAddForm({ ...addForm, name: e.target.value })}
                    placeholder="例如：VIP会员卡密"
                    className="w-full ios-input px-4 py-3 rounded-xl"
                  />
                </div>

                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700 mb-2">类型</label>
                  <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                    <button
                      type="button"
                      onClick={() => setAddForm({ ...addForm, type: 'data', content: '' })}
                      className={`p-3 rounded-xl font-bold transition-all ${addForm.type === 'data' ? 'bg-[#0094f7] text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}
                    >
                      <CreditCard className="w-5 h-5 mx-auto mb-1" />
                      批量库存
                    </button>
                    <button
                      type="button"
                      onClick={() => setAddForm({ ...addForm, type: 'text', content: '' })}
                      className={`p-3 rounded-xl font-bold transition-all ${addForm.type === 'text' ? 'bg-[#0094f7] text-white' : 'bg-gray-100 text-gray-600'}`}
                    >
                      <FileText className="w-5 h-5 mx-auto mb-1" />
                      文本
                    </button>
                    <button
                      type="button"
                      onClick={() => setAddForm({ ...addForm, type: 'image', content: '' })}
                      className={`p-3 rounded-xl font-bold transition-all ${addForm.type === 'image' ? 'bg-[#0094f7] text-white' : 'bg-gray-100 text-gray-600'}`}
                    >
                      <ImageIcon className="w-5 h-5 mx-auto mb-1" />
                      图片
                    </button>
                    <button
                      type="button"
                      onClick={() => setAddForm({ ...addForm, type: 'api', content: '' })}
                      className={`p-3 rounded-xl font-bold transition-all ${addForm.type === 'api' ? 'bg-[#0094f7] text-white' : 'bg-gray-100 text-gray-600'}`}
                    >
                      <Code className="w-5 h-5 mx-auto mb-1" />
                      API
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
                      onChange={(e) => setAddForm({ ...addForm, content: e.target.value })}
                      placeholder="https://api.example.com/get-code"
                      className="w-full ios-input px-4 py-3 rounded-xl"
                    />
                  ) : addForm.type === 'image' ? (
                    <input
                      type="url"
                      value={addForm.content}
                      onChange={(e) => setAddForm({ ...addForm, content: e.target.value })}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                      placeholder="https://example.com/card.png"
                    />
                  ) : (
                    <textarea
                      value={addForm.content}
                      onChange={(e) => setAddForm({ ...addForm, content: e.target.value })}
                      className={`w-full ios-input px-4 py-3 rounded-xl resize-none text-sm ${addForm.type === 'data' ? 'h-48 font-mono' : 'h-32'}`}
                      placeholder={addForm.type === 'data' ? 'CODE-123456\nCODE-789012\n...' : '请输入每次发货时发送的固定文字'}
                    />
                  )}
                  {addForm.type === 'data' && (
                    <p className="text-xs text-gray-500">当前库存：<span className="font-bold text-[#0094f7]">{addForm.content.split('\n').filter(line => line.trim()).length}</span> 条</p>
                  )}
                </div>

                {addForm.type === 'api' && (
                  <div className="border-t border-gray-100 pt-5 space-y-4">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">请求方法</label>
                        <select value={addForm.api_method} onChange={e => setAddForm({...addForm, api_method: e.target.value as 'GET' | 'POST'})} className="w-full ios-input px-4 py-3 rounded-xl">
                          <option value="GET">GET</option>
                          <option value="POST">POST</option>
                        </select>
                      </div>
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">超时时间（秒）</label>
                        <input type="number" min="1" max="60" value={addForm.api_timeout} onChange={e => setAddForm({...addForm, api_timeout: parseInt(e.target.value) || 10})} className="w-full ios-input px-4 py-3 rounded-xl" />
                      </div>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">请求头（JSON）</label>
                        <textarea value={addForm.api_headers} onChange={e => setAddForm({...addForm, api_headers: e.target.value})} className="w-full ios-input px-4 py-3 rounded-xl h-24 resize-none font-mono text-xs" placeholder='{"Authorization":"Bearer token"}' />
                      </div>
                      <div className="space-y-2">
                        <label className="block text-sm font-bold text-gray-700">请求参数（JSON）</label>
                        <textarea value={addForm.api_params} onChange={e => setAddForm({...addForm, api_params: e.target.value})} className="w-full ios-input px-4 py-3 rounded-xl h-24 resize-none font-mono text-xs" placeholder='{"order_id":"{order_id}"}' />
                      </div>
                    </div>
                  </div>
                )}

                <div className="grid grid-cols-1 sm:grid-cols-[1fr_180px] gap-4">
                  <div className="space-y-2">
                    <label className="block text-sm font-bold text-gray-700">描述</label>
                    <input value={addForm.description} onChange={e => setAddForm({...addForm, description: e.target.value})} placeholder="卡密用途描述（可选）" className="w-full ios-input px-4 py-3 rounded-xl" />
                  </div>
                  <div className="space-y-2">
                    <label className="block text-sm font-bold text-gray-700">延时发货（秒）</label>
                    <input type="number" value={addForm.delay_seconds} onChange={e => setAddForm({...addForm, delay_seconds: parseInt(e.target.value) || 0})} className="w-full ios-input px-4 py-3 rounded-xl" min="0" max="3600" />
                  </div>
                </div>

              </div>
            </div>

            <div className="modal-footer">
              <div className="flex gap-3 w-full">
                <button
                  onClick={() => setShowAddModal(false)}
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
    </div>
  );
};

export default CardList;
