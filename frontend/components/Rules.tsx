import React, { useEffect, useState } from 'react';
import { ShippingRule, ReplyRule, AccountDetail } from '../types';
import { getShippingRules, getReplyRules, updateShippingRule, deleteShippingRule, updateReplyRule, deleteReplyRule, getAccountDetails } from '../services/api';
import { Zap, MessageCircle, Plus, Trash2, Edit, Save, X, AlertCircle, RefreshCw, Package } from 'lucide-react';

const Rules: React.FC = () => {
  const [activeTab, setActiveTab] = useState<'shipping' | 'reply'>('shipping');
  const [shippingRules, setShippingRules] = useState<ShippingRule[]>([]);
  const [replyRules, setReplyRules] = useState<ReplyRule[]>([]);
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [cards, setCards] = useState<any[]>([]); // 需要从卡密API获取
  const [loading, setLoading] = useState(false);
  const [selectedAccountId, setSelectedAccountId] = useState<string>('');

  // 弹窗状态
  const [showShippingModal, setShowShippingModal] = useState(false);
  const [showReplyModal, setShowReplyModal] = useState(false);
  const [editingShippingRule, setEditingShippingRule] = useState<Partial<ShippingRule> | null>(null);
  const [editingReplyRule, setEditingReplyRule] = useState<Partial<ReplyRule> | null>(null);

  // Load data
  const refresh = async () => {
      setLoading(true);
      try {
          if (activeTab === 'shipping') {
              const data = await getShippingRules();
              setShippingRules(data);
          } else {
              // 关键词回复需要选择账号
              if (!selectedAccountId) {
                  setReplyRules([]);
                  return;
              }
              const data = await getReplyRules(selectedAccountId);
              setReplyRules(data);
          }
      } finally {
          setLoading(false);
      }
  };

  useEffect(() => {
      getAccountDetails().then((accounts) => {
        setAccounts(accounts);
        // 自动选择第一个账号
        if (accounts.length > 0 && !selectedAccountId) {
          setSelectedAccountId(accounts[0].id);
        }
      });
  }, []);

  useEffect(() => {
      refresh();
  }, [activeTab, selectedAccountId]);

  // Handlers
  const handleToggleShipping = async (rule: ShippingRule) => {
      await updateShippingRule({ ...rule, enabled: !rule.enabled });
      refresh();
  };
  const handleDeleteShipping = async (id: string) => {
      if(confirm('确定删除该发货规则吗？')) {
          await deleteShippingRule(id);
          refresh();
      }
  };

  const handleToggleReply = async (rule: ReplyRule) => {
      if (!selectedAccountId) return alert('请先选择账号');
      await updateReplyRule({ ...rule, enabled: !rule.enabled }, selectedAccountId);
      refresh();
  };
  const handleDeleteReply = async (id: string) => {
       if (!selectedAccountId) return alert('请先选择账号');
       if(confirm('确定删除该回复规则吗？')) {
          await deleteReplyRule(id, selectedAccountId);
          refresh();
      }
  };

  // 发货规则增删改
  const handleAddShippingRule = () => {
    setEditingShippingRule({
      name: '',
      item_keyword: '',
      card_group_id: 0,
      card_group_name: '',
      priority: 1,
      enabled: true
    });
    setShowShippingModal(true);
  };

  const handleEditShippingRule = (rule: ShippingRule) => {
    setEditingShippingRule({ ...rule });
    setShowShippingModal(true);
  };

  const handleSaveShippingRule = async () => {
    if (!editingShippingRule) return;
    try {
      await updateShippingRule(editingShippingRule);
      setShowShippingModal(false);
      refresh();
    } catch (error) {
      console.error('保存发货规则失败:', error);
      alert('保存失败，请重试');
    }
  };

  // 关键词回复增删改
  const handleAddReplyRule = () => {
    if (!selectedAccountId) return alert('请先选择账号');
    setEditingReplyRule({
      keyword: '',
      reply_content: '',
      match_type: 'exact',
      enabled: true
    });
    setShowReplyModal(true);
  };

  const handleEditReplyRule = (rule: ReplyRule) => {
    setEditingReplyRule({ ...rule });
    setShowReplyModal(true);
  };

  const handleSaveReplyRule = async () => {
    if (!editingReplyRule || !selectedAccountId) return;
    try {
      await updateReplyRule(editingReplyRule, selectedAccountId);
      setShowReplyModal(false);
      refresh();
    } catch (error) {
      console.error('保存回复规则失败:', error);
      alert('保存失败，请重试');
    }
  };

  return (
    <div className="space-y-6 animate-fade-in pb-20">
      <div className="flex justify-between items-center">
        <div>
          <h2 className="text-3xl font-extrabold text-gray-900">智能策略</h2>
          <p className="text-gray-500 mt-2 font-medium">配置自动发货逻辑与关键词自动回复规则。</p>
        </div>
        <button onClick={refresh} className="p-3 bg-white rounded-xl shadow-sm hover:shadow-md transition-shadow">
            <RefreshCw className={`w-5 h-5 text-gray-500 ${loading ? 'animate-spin' : ''}`} />
        </button>
      </div>

      {/* Tabs */}
      <div className="flex p-1.5 bg-gray-200/50 rounded-2xl w-fit">
          <button 
            onClick={() => setActiveTab('shipping')}
            className={`flex items-center gap-2 px-6 py-3 rounded-xl text-sm font-bold transition-all ${activeTab === 'shipping' ? 'bg-white shadow-sm text-black' : 'text-gray-500 hover:text-gray-700'}`}
          >
              <Zap className="w-4 h-4" /> 自动发货规则
          </button>
          <button 
            onClick={() => setActiveTab('reply')}
            className={`flex items-center gap-2 px-6 py-3 rounded-xl text-sm font-bold transition-all ${activeTab === 'reply' ? 'bg-white shadow-sm text-black' : 'text-gray-500 hover:text-gray-700'}`}
          >
              <MessageCircle className="w-4 h-4" /> 关键词回复
          </button>
      </div>

      {/* Content Area */}
      <div className="ios-card bg-white rounded-[2rem] p-6 min-h-[500px]">
          
          {/* SHIPPING RULES */}
          {activeTab === 'shipping' && (
              <div className="space-y-4">
                  <div className="flex justify-between items-center mb-6">
                      <div className="flex items-center gap-2 text-sm text-blue-700 bg-blue-50 px-4 py-2 rounded-xl">
                          <AlertCircle className="w-4 h-4" />
                          当订单商品标题包含关键词时，自动发送对应卡密。
                      </div>
                      <button onClick={handleAddShippingRule} className="ios-btn-primary px-5 py-3 rounded-2xl text-sm font-bold flex items-center gap-2 shadow-lg shadow-blue-200">
                          <Plus className="w-4 h-4" /> 新增发货规则
                      </button>
                  </div>
                  
                  <div className="space-y-3">
                      {shippingRules.map(rule => (
                          <div key={rule.id} className="flex items-center justify-between p-5 rounded-2xl border border-gray-100 bg-[#F7F8FA] hover:bg-white hover:shadow-lg transition-all duration-300">
                              <div className="flex items-center gap-4">
                                  <div className={`w-12 h-12 rounded-2xl flex items-center justify-center font-bold text-lg ${rule.enabled ? 'bg-black text-[#0094f7]' : 'bg-gray-200 text-gray-400'}`}>
                                      {rule.priority}
                                  </div>
                                  <div>
                                      <h3 className="font-bold text-gray-900 text-lg">{rule.name}</h3>
                                      <div className="flex items-center gap-3 mt-1 text-sm text-gray-500 font-medium">
                                          <span className="bg-blue-50 text-blue-600 px-2 py-0.5 rounded-lg">关键词: {rule.item_keyword}</span>
                                          <span>→</span>
                                          <span className="bg-purple-50 text-purple-600 px-2 py-0.5 rounded-lg">卡密组: {rule.card_group_name || `ID:${rule.card_group_id}`}</span>
                                      </div>
                                  </div>
                              </div>
                              <div className="flex items-center gap-3">
                                  <button
                                    onClick={() => handleEditShippingRule(rule)}
                                    className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors"
                                    title="编辑"
                                  >
                                    <Edit className="w-4 h-4" />
                                  </button>
                                  <button
                                    onClick={() => handleToggleShipping(rule)}
                                    className={`w-12 h-8 rounded-full relative transition-colors ${rule.enabled ? 'bg-green-500' : 'bg-gray-300'}`}
                                  >
                                      <div className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-sm transition-transform ${rule.enabled ? 'left-5' : 'left-1'}`}></div>
                                  </button>
                                  <button onClick={() => handleDeleteShipping(rule.id)} className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors">
                                      <Trash2 className="w-5 h-5" />
                                  </button>
                              </div>
                          </div>
                      ))}
                      {shippingRules.length === 0 && <div className="text-center py-20 text-gray-400">暂无规则</div>}
                  </div>
              </div>
          )}

          {/* REPLY RULES */}
          {activeTab === 'reply' && (
              <div className="space-y-4">
                  <div className="flex justify-between items-center mb-6">
                      <div className="flex items-center gap-4">
                          <div className="flex items-center gap-2 text-sm text-blue-700 bg-blue-50 px-4 py-2 rounded-xl">
                              <AlertCircle className="w-4 h-4" />
                              当买家发送包含关键词的消息时，优先触发此回复。
                          </div>
                          <select
                            value={selectedAccountId}
                            onChange={(e) => setSelectedAccountId(e.target.value)}
                            className="ios-input px-4 py-3 rounded-xl text-sm"
                          >
                              <option value="">选择账号查看关键词</option>
                              {accounts.map(acc => (
                                <option key={acc.id} value={acc.id}>{acc.nickname || acc.id}</option>
                              ))}
                          </select>
                      </div>
                      <button
                        onClick={handleAddReplyRule}
                        className="ios-btn-primary px-5 py-3 rounded-2xl text-sm font-bold flex items-center gap-2 shadow-lg shadow-blue-200"
                      >
                          <Plus className="w-4 h-4" /> 新增回复规则
                      </button>
                  </div>

                  <div className="space-y-3">
                      {replyRules.map(rule => (
                          <div key={rule.id} className="flex flex-col md:flex-row md:items-center justify-between p-5 rounded-2xl border border-gray-100 bg-[#F7F8FA] hover:bg-white hover:shadow-lg transition-all duration-300 gap-4">
                              <div className="flex-1">
                                  <div className="flex items-center gap-3 mb-2">
                                      <span className="px-3 py-1 bg-black text-white rounded-lg text-xs font-bold">{rule.match_type === 'exact' ? '精确匹配' : '模糊包含'}</span>
                                      <h3 className="font-bold text-gray-900">"{rule.keyword}"</h3>
                                  </div>
                                  <div className="bg-white p-3 rounded-xl border border-gray-100 text-sm text-gray-600 leading-relaxed">
                                      {rule.reply_content}
                                  </div>
                              </div>
                              <div className="flex items-center gap-4 border-t md:border-t-0 md:border-l border-gray-200 pt-4 md:pt-0 md:pl-6">
                                  <button
                                    onClick={() => handleEditReplyRule(rule)}
                                    className="p-2 text-gray-400 hover:text-black hover:bg-gray-100 rounded-xl transition-colors"
                                    title="编辑"
                                  >
                                    <Edit className="w-4 h-4" />
                                  </button>
                                  <button
                                    onClick={() => handleToggleReply(rule)}
                                    className={`w-12 h-8 rounded-full relative transition-colors ${rule.enabled ? 'bg-green-500' : 'bg-gray-300'}`}
                                  >
                                      <div className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-sm transition-transform ${rule.enabled ? 'left-5' : 'left-1'}`}></div>
                                  </button>
                                  <button onClick={() => handleDeleteReply(rule.id)} className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-xl transition-colors">
                                      <Trash2 className="w-5 h-5" />
                                  </button>
                              </div>
                          </div>
                      ))}
                      {replyRules.length === 0 && <div className="text-center py-20 text-gray-400">暂无规则</div>}
                  </div>
              </div>
          )}
      </div>

      {/* Shipping Rule Modal */}
      {showShippingModal && (
        <div className="modal-overlay">
          <div className="modal-container">
            <div className="modal-header">
              <div className="flex items-center justify-between w-full">
                <h3 className="text-2xl font-extrabold text-gray-900">
                  {editingShippingRule?.id ? '编辑发货规则' : '新增发货规则'}
                </h3>
                <button
                  onClick={() => setShowShippingModal(false)}
                  className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors"
                >
                  <X className="w-5 h-5 text-gray-600" />
                </button>
              </div>
            </div>

            <div className="modal-body space-y-5">
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">规则名称</label>
                <input
                  type="text"
                  value={editingShippingRule?.name || ''}
                  onChange={(e) => setEditingShippingRule({ ...editingShippingRule, name: e.target.value })}
                  placeholder="例如：VIP会员发货"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">商品关键词</label>
                <input
                  type="text"
                  value={editingShippingRule?.item_keyword || ''}
                  onChange={(e) => setEditingShippingRule({ ...editingShippingRule, item_keyword: e.target.value })}
                  placeholder="商品标题中包含的关键词"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">卡密组ID</label>
                <input
                  type="number"
                  value={editingShippingRule?.card_group_id || 0}
                  onChange={(e) => setEditingShippingRule({ ...editingShippingRule, card_group_id: parseInt(e.target.value) || 0 })}
                  placeholder="输入卡密组ID"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">优先级</label>
                <input
                  type="number"
                  value={editingShippingRule?.priority || 1}
                  onChange={(e) => setEditingShippingRule({ ...editingShippingRule, priority: parseInt(e.target.value) || 1 })}
                  min="1"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
                <p className="text-xs text-gray-500 mt-1">数字越小优先级越高</p>
              </div>

              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <span className="font-bold text-gray-900">启用状态</span>
                <button
                  type="button"
                  onClick={() => setEditingShippingRule({ ...editingShippingRule, enabled: !editingShippingRule?.enabled })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${
                    editingShippingRule?.enabled ? 'bg-[#0094f7]' : 'bg-gray-300'
                  }`}
                >
                  <span
                    className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 block ${
                      editingShippingRule?.enabled ? 'translate-x-7' : 'translate-x-1'
                    }`}
                  />
                </button>
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  onClick={() => setShowShippingModal(false)}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={handleSaveShippingRule}
                  className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
                >
                  <Save className="w-4 h-4" />
                  保存规则
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Reply Rule Modal */}
      {showReplyModal && (
        <div className="modal-overlay">
          <div className="modal-container">
            <div className="modal-header">
              <div className="flex items-center justify-between w-full">
                <h3 className="text-2xl font-extrabold text-gray-900">
                  {editingReplyRule?.id ? '编辑回复规则' : '新增回复规则'}
                </h3>
                <button
                  onClick={() => setShowReplyModal(false)}
                  className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors"
                >
                  <X className="w-5 h-5 text-gray-600" />
                </button>
              </div>
            </div>

            <div className="modal-body space-y-5">
              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">关键词</label>
                <input
                  type="text"
                  value={editingReplyRule?.keyword || ''}
                  onChange={(e) => setEditingReplyRule({ ...editingReplyRule, keyword: e.target.value })}
                  placeholder="买家发送的关键词"
                  className="w-full ios-input px-4 py-3 rounded-xl"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">回复内容</label>
                <textarea
                  value={editingReplyRule?.reply_content || ''}
                  onChange={(e) => setEditingReplyRule({ ...editingReplyRule, reply_content: e.target.value })}
                  placeholder="自动回复的内容"
                  className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none"
                />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">匹配类型</label>
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => setEditingReplyRule({ ...editingReplyRule, match_type: 'exact' })}
                    className={`p-3 rounded-xl font-bold transition-all ${
                      editingReplyRule?.match_type === 'exact' ? 'bg-[#0094f7] text-white' : 'bg-gray-100 text-gray-600'
                    }`}
                  >
                    精确匹配
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditingReplyRule({ ...editingReplyRule, match_type: 'fuzzy' })}
                    className={`p-3 rounded-xl font-bold transition-all ${
                      editingReplyRule?.match_type === 'fuzzy' ? 'bg-[#0094f7] text-white' : 'bg-gray-100 text-gray-600'
                    }`}
                  >
                    模糊包含
                  </button>
                </div>
              </div>

              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <span className="font-bold text-gray-900">启用状态</span>
                <button
                  type="button"
                  onClick={() => setEditingReplyRule({ ...editingReplyRule, enabled: !editingReplyRule?.enabled })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${
                    editingReplyRule?.enabled ? 'bg-[#0094f7]' : 'bg-gray-300'
                  }`}
                >
                  <span
                    className={`absolute top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 block ${
                      editingReplyRule?.enabled ? 'translate-x-7' : 'translate-x-1'
                    }`}
                  />
                </button>
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  onClick={() => setShowReplyModal(false)}
                  className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={handleSaveReplyRule}
                  className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
                >
                  <Save className="w-4 h-4" />
                  保存规则
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default Rules;
