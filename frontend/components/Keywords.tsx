import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AccountDetail, Card, Item, ShippingRule, ShippingVariant, ReplyRule, DefaultReply } from '../types';
import { getAccountDetails, getItems, getReplyRules, updateReplyRule, deleteReplyRule, getShippingRules, updateShippingRule, deleteShippingRule, getCards, getDefaultReplies, getDefaultReply, updateDefaultReply, deleteDefaultReply, clearDefaultReplyRecords } from '../services/api';
import { Plus, Trash2, MessageSquare, X, Save, Loader2, Key, Truck, Power, PowerOff, Edit2, RefreshCw, Sparkles, Bot, Layers3 } from 'lucide-react';

type TabType = 'reply' | 'delivery' | 'default';

interface Keyword {
  id: string;
  keyword: string;
  reply_content: string;
  match_type: 'exact' | 'fuzzy';
  enabled: boolean;
}

interface DeliveryRuleForm {
  cookie_id: string;
  item_id: string;
  keyword: string;
  description: string;
  enabled: boolean;
  variants: ShippingVariant[];
}

interface KeywordsProps {
  initialDeliveryTarget?: {
    cookieId: string;
    itemId: string;
    requestId: number;
  };
  onDeliveryTargetHandled?: () => void;
}

const emptyVariant = (): ShippingVariant => ({
  spec_name: '',
  spec_value: '',
  card_id: 0,
  delivery_count: 1,
  enabled: true,
});

interface DefaultReplyForm {
  cookie_id: string;
  enabled: boolean;
  reply_content: string;
  reply_once: boolean;
  reply_image_url: string;
}

const Keywords: React.FC<KeywordsProps> = ({ initialDeliveryTarget, onDeliveryTargetHandled }) => {
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [selectedAccount, setSelectedAccount] = useState<string>('');
  const [activeTab, setActiveTab] = useState<TabType>('reply');

  // 关键词回复相关状态
  const [keywords, setKeywords] = useState<Keyword[]>([]);
  const [showReplyModal, setShowReplyModal] = useState(false);
  const [editingKeyword, setEditingKeyword] = useState<Keyword | null>(null);
  const [replyForm, setReplyForm] = useState({
    keyword: '',
    reply_content: ''
  });

  // 关键词发货相关状态
  const [shippingRules, setShippingRules] = useState<ShippingRule[]>([]);
  const [cards, setCards] = useState<Card[]>([]);
  const [items, setItems] = useState<Item[]>([]);
  const [showDeliveryModal, setShowDeliveryModal] = useState(false);
  const [editingDeliveryRule, setEditingDeliveryRule] = useState<ShippingRule | null>(null);
  const [deliveryForm, setDeliveryForm] = useState<DeliveryRuleForm>({
    cookie_id: '',
    item_id: '',
    keyword: '',
    description: '',
    enabled: true,
    variants: [emptyVariant()],
  });

  // 账号默认回复相关状态
  const [defaultReplies, setDefaultReplies] = useState<Record<string, DefaultReply>>({});
  const [showDefaultModal, setShowDefaultModal] = useState(false);
  const [editingDefaultReply, setEditingDefaultReply] = useState<DefaultReply | null>(null);
  const [defaultForm, setDefaultForm] = useState<DefaultReplyForm>({
    cookie_id: '',
    enabled: false,
    reply_content: '',
    reply_once: false,
    reply_image_url: ''
  });

  const [loading, setLoading] = useState(false);
  const handledDeliveryTarget = useRef<number | undefined>(undefined);

  useEffect(() => {
    getAccountDetails().then((data) => {
      setAccounts(data);
      // 默认选择第一个账号
      setSelectedAccount(current => current || data?.[0]?.id || '');
    });
  }, []);

  useEffect(() => {
    if (selectedAccount) {
      loadKeywords();
      loadShippingRules();
      loadCards();
      loadItems();
      loadDefaultReplies();
    }
  }, [selectedAccount]);

  useEffect(() => {
    if (!initialDeliveryTarget || handledDeliveryTarget.current === initialDeliveryTarget.requestId) return;
    handledDeliveryTarget.current = initialDeliveryTarget.requestId;
    let cancelled = false;

    const openLinkedRule = async () => {
      setSelectedAccount(initialDeliveryTarget.cookieId);
      setActiveTab('delivery');
      try {
        const [ruleList, itemList, cardList] = await Promise.all([getShippingRules(), getItems(), getCards()]);
        if (cancelled) return;
        setShippingRules(ruleList);
        setItems(itemList);
        setCards(cardList);
        const rule = ruleList.find(candidate =>
          candidate.cookie_id === initialDeliveryTarget.cookieId && candidate.item_id === initialDeliveryTarget.itemId
        );
        if (rule) {
          setEditingDeliveryRule(rule);
          setDeliveryForm({
            cookie_id: rule.cookie_id || initialDeliveryTarget.cookieId,
            item_id: rule.item_id || initialDeliveryTarget.itemId,
            keyword: rule.item_keyword,
            description: rule.name,
            enabled: rule.enabled,
            variants: rule.variants?.length ? rule.variants.map(variant => ({ ...variant })) : [{ ...emptyVariant(), card_id: rule.card_group_id }],
          });
        } else {
          const item = itemList.find(candidate =>
            candidate.cookie_id === initialDeliveryTarget.cookieId && candidate.item_id === initialDeliveryTarget.itemId
          );
          setEditingDeliveryRule(null);
          setDeliveryForm({
            cookie_id: initialDeliveryTarget.cookieId,
            item_id: initialDeliveryTarget.itemId,
            keyword: item?.item_title || initialDeliveryTarget.itemId,
            description: '',
            enabled: true,
            variants: [emptyVariant()],
          });
        }
        setShowDeliveryModal(true);
        onDeliveryTargetHandled?.();
      } catch (error) {
        console.error('打开商品发货规则失败', error);
        alert('无法加载该商品的发货规则');
        onDeliveryTargetHandled?.();
      }
    };

    void openLinkedRule();
    return () => { cancelled = true; };
  }, [initialDeliveryTarget]);

  const loadDefaultReplies = async () => {
    try {
      const data = await getDefaultReplies();
      setDefaultReplies(data);
    } catch (e) {
      console.error('加载默认回复失败', e);
    }
  };

  const loadShippingRules = async () => {
    try {
      const data = await getShippingRules();
      setShippingRules(data);
    } catch (e) {
      console.error('加载发货规则失败', e);
    }
  };

  const loadCards = async () => {
    try {
      const data = await getCards();
      setCards(data);
    } catch (e) {
      console.error('加载卡券失败', e);
    }
  };

  const loadItems = async () => {
    try {
      setItems(await getItems());
    } catch (e) {
      console.error('加载商品失败', e);
    }
  };

  const loadKeywords = async () => {
    if (!selectedAccount) return;
    setLoading(true);
    try {
      const data = await getReplyRules(selectedAccount);
      setKeywords(data as Keyword[]);
    } catch (e) {
      console.error('加载关键词失败', e);
    } finally {
      setLoading(false);
    }
  };

  const handleAdd = () => {
    if (activeTab === 'reply') {
      setEditingKeyword(null);
      setReplyForm({ keyword: '', reply_content: '' });
      setShowReplyModal(true);
    } else if (activeTab === 'delivery') {
      setEditingDeliveryRule(null);
      setDeliveryForm({
        cookie_id: selectedAccount,
        item_id: '',
        keyword: '',
        description: '',
        enabled: true,
        variants: [emptyVariant()],
      });
      setShowDeliveryModal(true);
    } else {
      // default tab - 编辑选中账号的默认回复
      if (!selectedAccount) return;
      loadDefaultReplyForEdit(selectedAccount);
    }
  };

  const loadDefaultReplyForEdit = async (cookieId: string) => {
    try {
      const data = await getDefaultReply(cookieId);
      setEditingDefaultReply(data);
      setDefaultForm({
        cookie_id: cookieId,
        enabled: data.enabled,
        reply_content: data.reply_content,
        reply_once: data.reply_once,
        reply_image_url: data.reply_image_url || ''
      });
      setShowDefaultModal(true);
    } catch (e) {
      console.error('加载默认回复失败', e);
      // 如果没有设置，创建新的
      setEditingDefaultReply(null);
      setDefaultForm({
        cookie_id: cookieId,
        enabled: false,
        reply_content: '',
        reply_once: false,
        reply_image_url: ''
      });
      setShowDefaultModal(true);
    }
  };

  const handleEdit = (keyword: Keyword) => {
    if (activeTab === 'reply') {
      setEditingKeyword(keyword);
      setReplyForm({
        keyword: keyword.keyword,
        reply_content: keyword.reply_content
      });
      setShowReplyModal(true);
    }
  };

  const handleEditDelivery = (rule: ShippingRule) => {
    setEditingDeliveryRule(rule);
    setDeliveryForm({
      cookie_id: rule.cookie_id || selectedAccount,
      item_id: rule.item_id || '',
      keyword: rule.item_keyword,
      description: rule.name,
      enabled: rule.enabled,
      variants: rule.variants?.length ? rule.variants.map(variant => ({...variant})) : [{...emptyVariant(), card_id: rule.card_group_id}],
    });
    setShowDeliveryModal(true);
  };

  const handleSave = async () => {
    if (!selectedAccount) {
      alert('请先选择账号');
      return;
    }
    if (!replyForm.keyword.trim() || !replyForm.reply_content.trim()) {
      alert('请填写关键词和回复内容');
      return;
    }

    try {
      await updateReplyRule(
        {
          id: editingKeyword?.id,
          keyword: replyForm.keyword,
          reply_content: replyForm.reply_content,
          match_type: 'exact',
          enabled: true
        },
        selectedAccount
      );
      setShowReplyModal(false);
      loadKeywords();
      alert('保存成功！');
    } catch (e) {
      alert('保存失败：' + (e as Error).message);
    }
  };

  const handleSaveDelivery = async () => {
    if (!deliveryForm.cookie_id || !deliveryForm.item_id) {
	  alert('请选择需要自动发货的商品');
      return;
    }
    const item = items.find(candidate => candidate.cookie_id === deliveryForm.cookie_id && candidate.item_id === deliveryForm.item_id);
    const isMultiSpec = Boolean(item?.is_multi_spec);
    if (deliveryForm.variants.length === 0 || deliveryForm.variants.some(variant => !variant.card_id)) {
	  alert('请为每条映射选择卡密库存');
      return;
    }
    if (isMultiSpec && deliveryForm.variants.some(variant => !variant.spec_name.trim() || !variant.spec_value.trim())) {
      alert('多规格商品必须填写每一行的规格名称和规格值');
      return;
    }

    try {
      await updateShippingRule({
        id: editingDeliveryRule?.id,
        cookie_id: deliveryForm.cookie_id,
        item_id: deliveryForm.item_id,
        item_keyword: deliveryForm.keyword,
        card_group_id: deliveryForm.variants[0].card_id,
        name: deliveryForm.description,
        priority: deliveryForm.variants[0].delivery_count,
        enabled: deliveryForm.enabled,
        variants: deliveryForm.variants.map(variant => ({
          ...variant,
          spec_name: isMultiSpec ? variant.spec_name.trim() : '',
          spec_value: isMultiSpec ? variant.spec_value.trim() : '',
        })),
      });
      setShowDeliveryModal(false);
      loadShippingRules();
      alert('保存成功！');
    } catch (e) {
      alert('保存失败：' + (e as Error).message);
    }
  };

  const handleDelete = async (id: string) => {
    if (!selectedAccount || !confirm('确认删除该关键词吗？')) return;
    try {
      await deleteReplyRule(id, selectedAccount);
      loadKeywords();
      alert('删除成功！');
    } catch (e) {
      alert('删除失败：' + (e as Error).message);
    }
  };

  const handleDeleteDelivery = async (id: string) => {
    if (!confirm('确认删除该发货规则吗？')) return;
    try {
      await deleteShippingRule(id);
      loadShippingRules();
      alert('删除成功！');
    } catch (e) {
      alert('删除失败：' + (e as Error).message);
    }
  };

  const handleToggleDelivery = async (rule: ShippingRule) => {
    try {
      await updateShippingRule({
        ...rule,
        enabled: !rule.enabled,
      });
      loadShippingRules();
    } catch (e) {
      alert('操作失败：' + (e as Error).message);
    }
  };

  const selectedDeliveryItem = items.find(item =>
    item.cookie_id === deliveryForm.cookie_id && item.item_id === deliveryForm.item_id
  );
  const deliveryItems = items.filter(item => item.cookie_id === (deliveryForm.cookie_id || selectedAccount));

  const selectDeliveryItem = (itemID: string) => {
    const item = items.find(candidate => candidate.cookie_id === (deliveryForm.cookie_id || selectedAccount) && candidate.item_id === itemID);
    setDeliveryForm({
      ...deliveryForm,
      cookie_id: item?.cookie_id || selectedAccount,
      item_id: itemID,
      keyword: item?.item_title || itemID,
      variants: [emptyVariant()],
    });
  };

  const updateDeliveryVariant = (index: number, patch: Partial<ShippingVariant>) => {
    setDeliveryForm({
      ...deliveryForm,
      variants: deliveryForm.variants.map((variant, variantIndex) =>
        variantIndex === index ? {...variant, ...patch} : variant
      ),
    });
  };

  const handleSaveDefault = async () => {
    if (!defaultForm.cookie_id) {
      alert('请先选择账号');
      return;
    }

    try {
      await updateDefaultReply(defaultForm.cookie_id, {
        enabled: defaultForm.enabled,
        reply_content: defaultForm.reply_content,
        reply_once: defaultForm.reply_once,
        reply_image_url: defaultForm.reply_image_url
      });
      setShowDefaultModal(false);
      loadDefaultReplies();
      alert('保存成功！');
    } catch (e) {
      alert('保存失败：' + (e as Error).message);
    }
  };

  const handleDeleteDefault = async (cookieId: string) => {
    if (!confirm('确认删除该默认回复吗？')) return;
    try {
      await deleteDefaultReply(cookieId);
      loadDefaultReplies();
      alert('删除成功！');
    } catch (e) {
      alert('删除失败：' + (e as Error).message);
    }
  };

  const handleClearRecords = async (cookieId: string) => {
    if (!confirm('确认清空该账号的回复记录吗？清空后可以重新对所有对话使用默认回复。')) return;
    try {
      await clearDefaultReplyRecords(cookieId);
      alert('清空成功！');
    } catch (e) {
      alert('清空失败：' + (e as Error).message);
    }
  };

  return (
    <div className="space-y-8 animate-fade-in">
      {/* Header */}
      <div className="flex justify-between items-end">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">自动化规则</h2>
          <p className="text-gray-500 mt-2 font-medium">配置关键词回复、商品发货和账号默认回复</p>
        </div>
      </div>

      {/* Tab 切换 */}
      <div className="flex justify-center">
        <div className="inline-flex items-center bg-gradient-to-br from-gray-100 to-gray-200 p-2 rounded-3xl shadow-xl gap-1">
          <button
            onClick={() => setActiveTab('reply')}
            className={`flex items-center gap-2 px-6 py-3.5 rounded-2xl font-bold text-base transition-all duration-300 ${
              activeTab === 'reply'
                ? 'bg-gradient-to-r from-[#0094f7] to-[#0071e3] text-white shadow-lg'
                : 'text-gray-500 hover:text-gray-700 hover:bg-white/50'
            }`}
          >
            <MessageSquare className="w-5 h-5 shrink-0" />
            <span>关键词回复</span>
            <span className={`ml-1 px-2.5 py-0.5 rounded-full text-xs font-semibold tabular-nums ${activeTab === 'reply' ? 'bg-white/25 text-white' : 'bg-gray-200 text-gray-600'}`}>{keywords.length}</span>
          </button>
          <button
            onClick={() => setActiveTab('delivery')}
            className={`flex items-center gap-2 px-6 py-3.5 rounded-2xl font-bold text-base transition-all duration-300 ${
              activeTab === 'delivery'
                ? 'bg-gradient-to-r from-[#0094f7] to-[#0071e3] text-white shadow-lg'
                : 'text-gray-500 hover:text-gray-700 hover:bg-white/50'
            }`}
          >
            <Truck className="w-5 h-5 shrink-0" />
            <span>商品发货</span>
            <span className={`ml-1 px-2.5 py-0.5 rounded-full text-xs font-semibold tabular-nums ${activeTab === 'delivery' ? 'bg-white/25 text-white' : 'bg-gray-200 text-gray-600'}`}>{shippingRules.length}</span>
          </button>
          <button
            onClick={() => setActiveTab('default')}
            className={`flex items-center gap-2 px-6 py-3.5 rounded-2xl font-bold text-base transition-all duration-300 ${
              activeTab === 'default'
                ? 'bg-gradient-to-r from-[#0094f7] to-[#0071e3] text-white shadow-lg'
                : 'text-gray-500 hover:text-gray-700 hover:bg-white/50'
            }`}
          >
            <Bot className="w-5 h-5 shrink-0" />
            <span>账号默认回复</span>
            <span className={`ml-1 px-2.5 py-0.5 rounded-full text-xs font-semibold tabular-nums ${activeTab === 'default' ? 'bg-white/25 text-white' : 'bg-gray-200 text-gray-600'}`}>{(Object.values(defaultReplies) as DefaultReply[]).filter(reply => reply.enabled).length}</span>
          </button>
        </div>
      </div>

      {/* 操作栏 */}
      <div className="bg-white rounded-3xl shadow-xl p-6">
        <div className="flex flex-col sm:flex-row gap-4 items-center justify-between">
          <div className="flex items-center gap-4 w-full sm:w-auto">
            <label className="text-sm font-bold text-gray-700 whitespace-nowrap">选择账号</label>
            <select
              className="flex-1 sm:w-64 ios-input px-5 py-3 rounded-2xl font-medium border-2 border-gray-200 focus:border-[#0094f7] focus:ring-4 focus:ring-[#0094f7]/20 transition-all"
              value={selectedAccount}
              onChange={(e) => setSelectedAccount(e.target.value)}
            >
              <option value="">请选择账号</option>
              {accounts.map((acc) => (
                <option key={acc.id} value={acc.id}>
                  {acc.nickname}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-3 w-full sm:w-auto">
            <button
              onClick={() => {
                if (activeTab === 'reply') loadKeywords();
                else if (activeTab === 'delivery') loadShippingRules();
                else loadDefaultReplies();
              }}
              className="flex-1 sm:flex-none flex items-center justify-center gap-2 px-6 py-3 rounded-2xl font-bold bg-gradient-to-br from-gray-100 to-gray-200 hover:from-gray-200 hover:to-gray-300 transition-all shadow-lg"
            >
              <RefreshCw className="w-5 h-5" />
              刷新
            </button>
            <button
              onClick={handleAdd}
              disabled={!selectedAccount}
              className="flex-1 sm:flex-none flex items-center justify-center gap-2 px-8 py-3 rounded-2xl font-bold bg-gradient-to-r from-[#0094f7] to-[#0071e3] hover:from-[#0071e3] hover:to-[#0071e3] text-white shadow-xl hover:shadow-2xl hover:scale-105 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Plus className="w-5 h-5" />
              {activeTab === 'reply' ? '添加关键词' : activeTab === 'delivery' ? '添加发货规则' : '编辑默认回复'}
            </button>
          </div>
        </div>
      </div>

      {/* 内容区域 */}
      {!selectedAccount ? (
        <div className="py-24 text-center bg-gradient-to-br from-white to-gray-50 rounded-[2.5rem] border-3 border-dashed border-gray-300 shadow-xl">
          <div className="w-24 h-24 bg-gradient-to-br from-[#0094f7]/20 to-[#0071e3]/20 rounded-full flex items-center justify-center mx-auto mb-6 shadow-inner">
            <MessageSquare className="w-12 h-12 text-[#0094f7]" />
          </div>
          <h3 className="text-2xl font-bold text-gray-900 mb-2">请选择账号</h3>
          <p className="text-gray-500 text-lg">选择一个账号以管理其关键词规则</p>
        </div>
      ) : activeTab === 'reply' ? (
        // 关键词回复列表
        loading ? (
          <div className="py-24 flex justify-center">
            <div className="flex flex-col items-center gap-4">
              <Loader2 className="w-16 h-16 text-[#0094f7] animate-spin" />
              <p className="text-gray-500 font-medium">加载中...</p>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            {keywords.map((keyword, index) => (
              <div
                key={keyword.id}
                className="group relative bg-gradient-to-br from-white to-gray-50 rounded-3xl p-6 shadow-lg hover:shadow-2xl transition-all duration-300 border-2 border-transparent hover:border-[#0094f7]/30 overflow-hidden"
              >
                {/* 背景装饰 */}
                <div className="absolute top-0 right-0 w-32 h-32 bg-gradient-to-br from-[#0094f7]/10 to-transparent rounded-full -translate-y-1/2 translate-x-1/2 group-hover:scale-150 transition-transform duration-500"></div>

                <div className="relative flex items-center gap-6">
                  {/* 图标 */}
                  <div className="flex-shrink-0">
                    <div className="w-16 h-16 bg-gradient-to-br from-[#287efe] to-[#0094f7] rounded-2xl flex items-center justify-center shadow-lg group-hover:scale-110 group-hover:rotate-12 transition-all duration-300">
                      <Key className="w-8 h-8 text-blue-800" />
                    </div>
                  </div>

                  {/* 内容 */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-3 mb-3">
                      <h3 className="text-xl font-black text-gray-900">{keyword.keyword}</h3>
                      <span className="px-3 py-1.5 rounded-xl bg-gradient-to-r from-green-400 to-green-500 text-white text-xs font-bold shadow-md">
                        精确匹配
                      </span>
                    </div>
                    <p className="text-gray-600 bg-white/70 backdrop-blur-sm rounded-2xl px-4 py-3 line-clamp-2 shadow-inner border border-gray-100">
                      💬 {keyword.reply_content || '无回复内容'}
                    </p>
                  </div>

                  {/* 操作按钮 */}
                  <div className="flex gap-2 flex-shrink-0">
                    <button
                      onClick={() => handleEdit(keyword)}
                      className="p-3.5 bg-gradient-to-br from-blue-50 to-blue-100 text-blue-600 rounded-2xl hover:from-blue-100 hover:to-blue-200 transition-all shadow-md hover:shadow-lg hover:scale-110"
                      title="编辑"
                    >
                      <Edit2 className="w-5 h-5" />
                    </button>
                    <button
                      onClick={() => handleDelete(keyword.id)}
                      className="p-3.5 bg-gradient-to-br from-red-50 to-red-100 text-red-500 rounded-2xl hover:from-red-100 hover:to-red-200 transition-all shadow-md hover:shadow-lg hover:scale-110"
                      title="删除"
                    >
                      <Trash2 className="w-5 h-5" />
                    </button>
                  </div>
                </div>
              </div>
            ))}

            {keywords.length === 0 && (
              <div className="py-24 text-center bg-gradient-to-br from-white to-gray-50 rounded-[2.5rem] border-3 border-dashed border-gray-300 shadow-xl">
                <div className="w-24 h-24 bg-gradient-to-br from-[#0094f7]/20 to-[#0071e3]/20 rounded-full flex items-center justify-center mx-auto mb-6 shadow-inner">
                  <MessageSquare className="w-12 h-12 text-[#0094f7]" />
                </div>
                <h3 className="text-2xl font-bold text-gray-900 mb-2">暂无关键词</h3>
                <p className="text-gray-500 text-lg">点击右上角添加新的关键词规则</p>
              </div>
            )}
          </div>
        )
      ) : activeTab === 'delivery' ? (
        // 关键词发货列表
        <div className="space-y-4">
          {shippingRules.filter(rule => !rule.cookie_id || rule.cookie_id === selectedAccount).map((rule) => (
            <div
              key={rule.id}
              className={`group relative bg-gradient-to-br ${rule.enabled ? 'from-white to-blue-50/30' : 'from-gray-100 to-gray-150'} rounded-3xl p-6 shadow-lg hover:shadow-2xl transition-all duration-300 border-2 ${rule.enabled ? 'border-transparent hover:border-blue-400/30' : 'border-gray-200'} overflow-hidden`}
            >
              {/* 背景装饰 */}
              {rule.enabled && (
                <div className="absolute top-0 right-0 w-32 h-32 bg-gradient-to-br from-blue-400/10 to-transparent rounded-full -translate-y-1/2 translate-x-1/2 group-hover:scale-150 transition-transform duration-500"></div>
              )}

              <div className="relative flex items-center gap-6">
                {/* 图标 */}
                <div className="flex-shrink-0">
                  <div className={`w-16 h-16 rounded-2xl flex items-center justify-center shadow-lg group-hover:scale-110 transition-all duration-300 ${
                    rule.enabled
                      ? 'bg-gradient-to-br from-blue-400 to-blue-500 group-hover:rotate-12'
                      : 'bg-gradient-to-br from-gray-300 to-gray-400'
                  }`}>
                    <Truck className="w-8 h-8 text-white" />
                  </div>
                </div>

                {/* 内容 */}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-3">
                    <h3 className="text-xl font-black text-gray-900">{rule.item_title || rule.item_keyword}</h3>
                    <span className={`px-3 py-1.5 rounded-xl text-xs font-bold shadow-md ${
                      rule.enabled
                        ? 'bg-gradient-to-r from-green-400 to-green-500 text-white'
                        : 'bg-gradient-to-r from-gray-400 to-gray-500 text-white'
                    }`}>
                      {rule.enabled ? '已启用' : '已禁用'}
                    </span>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {(rule.variants || []).map((variant, index) => (
                      <span key={variant.id || index} className="text-xs bg-white border border-gray-200 text-gray-700 px-3 py-1.5 rounded-lg font-bold">
                        {variant.spec_value ? `${variant.spec_name}: ${variant.spec_value} → ` : '默认 → '}
                        {variant.card_name || `卡密 ${variant.card_id}`}
                      </span>
                    ))}
                  </div>
                  {rule.name && <p className="text-sm text-gray-500 mt-2">{rule.name}</p>}
                </div>

                {/* 操作按钮 */}
                <div className="flex gap-2 flex-shrink-0">
                  <button
                    onClick={() => handleToggleDelivery(rule)}
                    className={`p-3.5 rounded-2xl transition-all shadow-md hover:shadow-lg hover:scale-110 ${
                      rule.enabled
                        ? 'bg-gradient-to-br from-blue-50 to-blue-100 text-blue-600 hover:from-blue-100 hover:to-blue-200'
                        : 'bg-gradient-to-br from-green-50 to-green-100 text-green-600 hover:from-green-100 hover:to-green-200'
                    }`}
                    title={rule.enabled ? '禁用' : '启用'}
                  >
                    {rule.enabled ? <PowerOff className="w-5 h-5" /> : <Power className="w-5 h-5" />}
                  </button>
                  <button
                    onClick={() => handleEditDelivery(rule)}
                    className="p-3.5 bg-gradient-to-br from-blue-50 to-blue-100 text-blue-600 rounded-2xl hover:from-blue-100 hover:to-blue-200 transition-all shadow-md hover:shadow-lg hover:scale-110"
                    title="编辑"
                  >
                    <Edit2 className="w-5 h-5" />
                  </button>
                  <button
                    onClick={() => handleDeleteDelivery(rule.id)}
                    className="p-3.5 bg-gradient-to-br from-red-50 to-red-100 text-red-500 rounded-2xl hover:from-red-100 hover:to-red-200 transition-all shadow-md hover:shadow-lg hover:scale-110"
                    title="删除"
                  >
                    <Trash2 className="w-5 h-5" />
                  </button>
                </div>
              </div>
            </div>
          ))}

            {shippingRules.filter(rule => !rule.cookie_id || rule.cookie_id === selectedAccount).length === 0 && (
            <div className="py-24 text-center bg-gradient-to-br from-white to-gray-50 rounded-[2.5rem] border-3 border-dashed border-gray-300 shadow-xl">
              <div className="w-24 h-24 bg-gradient-to-br from-blue-400/20 to-blue-500/20 rounded-full flex items-center justify-center mx-auto mb-6 shadow-inner">
                <Truck className="w-12 h-12 text-blue-400" />
              </div>
              <h3 className="text-2xl font-bold text-gray-900 mb-2">暂无发货规则</h3>
              <p className="text-gray-500 text-lg">点击右上角添加新的发货规则</p>
            </div>
          )}
        </div>
      ) : activeTab === 'default' ? (
        // 账号默认回复列表
        <div className="space-y-4">
          {accounts.map((account) => {
            const defaultReply = defaultReplies[account.id];
            const hasDefaultReply = defaultReply && defaultReply.enabled;
            return (
              <div
                key={account.id}
                className={`group relative bg-gradient-to-br ${hasDefaultReply ? 'from-white to-purple-50/30' : 'from-gray-100 to-gray-150'} rounded-3xl p-6 shadow-lg hover:shadow-2xl transition-all duration-300 border-2 ${hasDefaultReply ? 'border-transparent hover:border-purple-400/30' : 'border-gray-200'} overflow-hidden`}
              >
                {/* 背景装饰 */}
                {hasDefaultReply && (
                  <div className="absolute top-0 right-0 w-32 h-32 bg-gradient-to-br from-purple-400/10 to-transparent rounded-full -translate-y-1/2 translate-x-1/2 group-hover:scale-150 transition-transform duration-500"></div>
                )}

                <div className="relative flex items-center gap-6">
                  {/* 图标 */}
                  <div className="flex-shrink-0">
                    <div className={`w-16 h-16 rounded-2xl flex items-center justify-center shadow-lg group-hover:scale-110 transition-all duration-300 ${
                      hasDefaultReply
                        ? 'bg-gradient-to-br from-purple-400 to-purple-500 group-hover:rotate-12'
                        : 'bg-gradient-to-br from-gray-300 to-gray-400'
                    }`}>
                      <Bot className="w-8 h-8 text-white" />
                    </div>
                  </div>

                  {/* 内容 */}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-3 mb-3">
                      <h3 className="text-xl font-black text-gray-900">{account.nickname}</h3>
                      <span className={`px-3 py-1.5 rounded-xl text-xs font-bold shadow-md ${
                        hasDefaultReply
                          ? 'bg-gradient-to-r from-green-400 to-green-500 text-white'
                          : 'bg-gradient-to-r from-gray-400 to-gray-500 text-white'
                      }`}>
                        {hasDefaultReply ? '已启用' : '未设置'}
                      </span>
                      {defaultReply?.reply_once && (
                        <span className="px-3 py-1.5 rounded-xl bg-purple-100 text-purple-700 text-xs font-bold shadow-md">
                          只回复一次
                        </span>
                      )}
                    </div>
                    {hasDefaultReply && (
                      <p className="text-gray-600 bg-white/70 backdrop-blur-sm rounded-2xl px-4 py-3 line-clamp-2 shadow-inner border border-gray-100">
                        💬 {defaultReply.reply_content || '无回复内容'}
                      </p>
                    )}
                  </div>

                  {/* 操作按钮 */}
                  <div className="flex gap-2 flex-shrink-0">
                    <button
                      onClick={() => loadDefaultReplyForEdit(account.id)}
                      className="p-3.5 bg-gradient-to-br from-purple-50 to-purple-100 text-purple-600 rounded-2xl hover:from-purple-100 hover:to-purple-200 transition-all shadow-md hover:shadow-lg hover:scale-110"
                      title="编辑"
                    >
                      <Edit2 className="w-5 h-5" />
                    </button>
                    {hasDefaultReply && (
                      <>
                        <button
                          onClick={() => handleClearRecords(account.id)}
                          className="p-3.5 bg-gradient-to-br from-blue-50 to-blue-100 text-blue-600 rounded-2xl hover:from-blue-100 hover:to-blue-200 transition-all shadow-md hover:shadow-lg hover:scale-110"
                          title="清空回复记录"
                        >
                          <RefreshCw className="w-5 h-5" />
                        </button>
                        <button
                          onClick={() => handleDeleteDefault(account.id)}
                          className="p-3.5 bg-gradient-to-br from-red-50 to-red-100 text-red-500 rounded-2xl hover:from-red-100 hover:to-red-200 transition-all shadow-md hover:shadow-lg hover:scale-110"
                          title="删除"
                        >
                          <Trash2 className="w-5 h-5" />
                        </button>
                      </>
                    )}
                  </div>
                </div>
              </div>
            );
          })}

          {accounts.length === 0 && (
            <div className="py-24 text-center bg-gradient-to-br from-white to-gray-50 rounded-[2.5rem] border-3 border-dashed border-gray-300 shadow-xl">
              <div className="w-24 h-24 bg-gradient-to-br from-purple-400/20 to-purple-500/20 rounded-full flex items-center justify-center mx-auto mb-6 shadow-inner">
                <Bot className="w-12 h-12 text-purple-400" />
              </div>
              <h3 className="text-2xl font-bold text-gray-900 mb-2">暂无账号</h3>
              <p className="text-gray-500 text-lg">请先添加账号</p>
            </div>
          )}
        </div>
      ) : null}

      {/* 关键词回复弹窗 */}
      {showReplyModal && createPortal(
        <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4 animate-fade-in">
          <div className="bg-white rounded-[2.5rem] shadow-2xl max-w-2xl w-full max-h-[90vh] overflow-hidden animate-scale-in">
            {/* Header */}
            <div className="bg-gradient-to-r from-[#0094f7] to-[#0071e3] p-8">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className="w-14 h-14 bg-white/30 backdrop-blur-sm rounded-2xl flex items-center justify-center">
                    <MessageSquare className="w-7 h-7 text-gray-900" />
                  </div>
                  <h3 className="text-3xl font-black text-gray-900">
                    {editingKeyword ? '编辑关键词' : '添加关键词'}
                  </h3>
                </div>
                <button
                  onClick={() => setShowReplyModal(false)}
                  className="p-3 bg-white/30 backdrop-blur-sm rounded-2xl hover:bg-white/40 transition-colors"
                >
                  <X className="w-6 h-6 text-gray-900" />
                </button>
              </div>
            </div>

            {/* Body */}
            <div className="p-8 space-y-6 overflow-y-auto max-h-[60vh]">
              <div>
                <label className="flex items-center gap-2 text-sm font-black text-gray-900 mb-3">
                  <Key className="w-5 h-5 text-[#0094f7]" />
                  触发关键词
                </label>
                <input
                  type="text"
                  value={replyForm.keyword}
                  onChange={(e) => setReplyForm({ ...replyForm, keyword: e.target.value })}
                  placeholder="例如：价格、包邮、怎么样"
                  className="w-full px-6 py-4 rounded-2xl font-medium border-2 border-gray-200 focus:border-[#0094f7] focus:ring-4 focus:ring-[#0094f7]/20 transition-all bg-gray-50"
                />
                <p className="text-sm text-gray-500 mt-2 ml-1">💡 买家消息中包含此关键词时自动回复</p>
              </div>

              <div>
                <label className="flex items-center gap-2 text-sm font-black text-gray-900 mb-3">
                  <MessageSquare className="w-5 h-5 text-[#0094f7]" />
                  回复内容
                </label>
                <textarea
                  value={replyForm.reply_content}
                  onChange={(e) => setReplyForm({ ...replyForm, reply_content: e.target.value })}
                  placeholder="输入自动回复的内容..."
                  rows={6}
                  className="w-full px-6 py-4 rounded-2xl font-medium border-2 border-gray-200 focus:border-[#0094f7] focus:ring-4 focus:ring-[#0094f7]/20 transition-all bg-gray-50 resize-none"
                />
                <p className="text-sm text-gray-500 mt-2 ml-1">💬 支持换行，系统将自动发送此内容给买家</p>
              </div>
            </div>

            {/* Footer */}
            <div className="p-8 bg-gray-50 border-t border-gray-100">
              <div className="flex gap-4">
                <button
                  onClick={() => setShowReplyModal(false)}
                  className="flex-1 px-8 py-4 rounded-2xl font-bold bg-white border-2 border-gray-200 hover:bg-gray-50 hover:border-gray-300 text-gray-700 transition-all shadow-lg hover:shadow-xl"
                >
                  取消
                </button>
                <button
                  onClick={handleSave}
                  className="flex-1 px-8 py-4 rounded-2xl font-bold bg-gradient-to-r from-[#0094f7] to-[#0071e3] hover:from-[#0071e3] hover:to-[#0071e3] text-white shadow-xl hover:shadow-2xl hover:scale-105 transition-all flex items-center justify-center gap-2"
                >
                  <Save className="w-5 h-5" />
                  保存关键词
                </button>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* 关键词发货弹窗 */}
      {showDeliveryModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '920px'}}>
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 className="text-2xl font-extrabold text-gray-900">{editingDeliveryRule ? '编辑商品发货规则' : '添加商品发货规则'}</h3>
                <p className="text-sm text-gray-500 mt-1">按订单真实规格选择对应的卡密库存</p>
              </div>
              <button onClick={() => setShowDeliveryModal(false)} className="p-2 rounded-xl hover:bg-gray-100" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>

            <div className="modal-body space-y-6">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">闲鱼账号</label>
                  <select
                    value={deliveryForm.cookie_id}
                    onChange={event => setDeliveryForm({...deliveryForm, cookie_id: event.target.value, item_id: '', keyword: '', variants: [emptyVariant()]})}
                    className="w-full ios-input px-4 py-3 rounded-xl"
                  >
                    {accounts.map(account => <option key={account.id} value={account.id}>{account.nickname || account.remark || account.id}</option>)}
                  </select>
                </div>
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">关联商品</label>
                  <select value={deliveryForm.item_id} onChange={event => selectDeliveryItem(event.target.value)} className="w-full ios-input px-4 py-3 rounded-xl">
                    <option value="">请选择已同步商品</option>
                    {deliveryItems.map(item => <option key={`${item.cookie_id}-${item.item_id}`} value={item.item_id}>{item.item_title || item.item_id}</option>)}
                  </select>
                </div>
              </div>

              {selectedDeliveryItem && (
                <div className="flex items-center justify-between gap-4 border-y border-gray-100 py-4">
                  <div className="min-w-0">
                    <div className="font-bold text-gray-900 truncate">{selectedDeliveryItem.item_title || selectedDeliveryItem.item_id}</div>
                    <div className="text-xs text-gray-500 mt-1 font-mono">{selectedDeliveryItem.item_id}</div>
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <span className={`flex-shrink-0 px-3 py-1.5 rounded-lg text-xs font-bold ${selectedDeliveryItem.is_multi_spec ? 'bg-blue-50 text-blue-700' : 'bg-gray-100 text-gray-600'}`}>
                      {selectedDeliveryItem.is_multi_spec ? '多规格已开启' : '普通商品'}
                    </span>
                    <span className={`flex-shrink-0 px-3 py-1.5 rounded-lg text-xs font-bold ${selectedDeliveryItem.is_multi_qty_ship ? 'bg-emerald-50 text-emerald-700' : 'bg-gray-100 text-gray-600'}`}>
                      {selectedDeliveryItem.is_multi_qty_ship ? '按购买数量发货' : '每单执行一次'}
                    </span>
                  </div>
                </div>
              )}

              {selectedDeliveryItem?.is_multi_qty_ship && (
                <div className="rounded-2xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800">
                  <span className="font-extrabold">数量计算：</span>最终发货份数 = 买家购买件数 × 下方“每件份数”。例如购买 3 件、每件 2 份，将发送 6 份。
                </div>
              )}

              <div className="space-y-3">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <h4 className="font-extrabold text-gray-900 flex items-center gap-2"><Layers3 className="w-4 h-4 text-[#0094f7]" />规格与库存映射</h4>
                    <p className="text-xs text-gray-500 mt-1">规格名称和值必须与订单详情完全一致</p>
                  </div>
                  {selectedDeliveryItem?.is_multi_spec && (
                    <button type="button" onClick={() => setDeliveryForm({...deliveryForm, variants: [...deliveryForm.variants, emptyVariant()]})} className="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg bg-gray-900 text-white text-xs font-bold hover:bg-black">
                      <Plus className="w-3.5 h-3.5" /> 添加规格
                    </button>
                  )}
                </div>

                {deliveryForm.variants.map((variant, index) => (
                  <div key={variant.id || index} className="grid grid-cols-1 md:grid-cols-[1fr_1fr_1.4fr_90px_40px] gap-3 items-end border border-gray-200 rounded-xl p-4">
                    {selectedDeliveryItem?.is_multi_spec ? (
                      <>
                        <div className="space-y-2">
                          <label className="block text-xs font-bold text-gray-600">规格名称</label>
                          <input value={variant.spec_name} onChange={event => updateDeliveryVariant(index, {spec_name: event.target.value})} className="w-full ios-input px-3 py-2.5 rounded-lg" placeholder="例如：套餐" />
                        </div>
                        <div className="space-y-2">
                          <label className="block text-xs font-bold text-gray-600">规格值</label>
                          <input value={variant.spec_value} onChange={event => updateDeliveryVariant(index, {spec_value: event.target.value})} className="w-full ios-input px-3 py-2.5 rounded-lg" placeholder="例如：30天" />
                        </div>
                      </>
                    ) : (
                      <div className="md:col-span-2 space-y-2">
                        <label className="block text-xs font-bold text-gray-600">匹配方式</label>
                        <div className="h-[42px] flex items-center text-sm text-gray-600 bg-gray-50 px-3 rounded-lg">默认库存</div>
                      </div>
                    )}
                    <div className="space-y-2">
                      <label className="block text-xs font-bold text-gray-600">卡密库存</label>
                      <select value={variant.card_id || ''} onChange={event => updateDeliveryVariant(index, {card_id: Number(event.target.value)})} className="w-full ios-input px-3 py-2.5 rounded-lg">
                        <option value="">请选择卡密</option>
                        {cards.filter(card => card.enabled).map(card => <option key={card.id} value={card.id}>{card.name}</option>)}
                      </select>
                    </div>
                    <div className="space-y-2">
                      <label className="block text-xs font-bold text-gray-600">{selectedDeliveryItem?.is_multi_qty_ship ? '每件份数' : '每单份数'}</label>
                      <input type="number" min="1" max="100" value={variant.delivery_count} onChange={event => updateDeliveryVariant(index, {delivery_count: Math.max(1, Number(event.target.value) || 1)})} className="w-full ios-input px-3 py-2.5 rounded-lg" />
                    </div>
                    <button type="button" disabled={deliveryForm.variants.length === 1} onClick={() => setDeliveryForm({...deliveryForm, variants: deliveryForm.variants.filter((_, variantIndex) => variantIndex !== index)})} className="w-10 h-10 flex items-center justify-center rounded-lg text-red-500 hover:bg-red-50 disabled:opacity-25" title="删除规格">
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                ))}
              </div>

              <div className="grid grid-cols-1 md:grid-cols-[1fr_auto] gap-4 items-end">
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">规则备注</label>
                  <input value={deliveryForm.description} onChange={event => setDeliveryForm({...deliveryForm, description: event.target.value})} className="w-full ios-input px-4 py-3 rounded-xl" placeholder="方便识别这条规则（可选）" />
                </div>
                <label className="h-[48px] flex items-center gap-3 px-4 bg-gray-50 rounded-xl text-sm font-bold text-gray-800">
                  <input type="checkbox" checked={deliveryForm.enabled} onChange={event => setDeliveryForm({...deliveryForm, enabled: event.target.checked})} className="w-4 h-4 rounded" />
                  启用规则
                </label>
              </div>
            </div>

            <div className="modal-footer flex gap-3">
              <button onClick={() => setShowDeliveryModal(false)} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200">取消</button>
              <button onClick={handleSaveDelivery} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2">
                <Save className="w-4 h-4" />保存发货规则
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* 账号默认回复弹窗 */}
      {showDefaultModal && createPortal(
        <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4 animate-fade-in">
          <div className="bg-white rounded-[2.5rem] shadow-2xl max-w-2xl w-full max-h-[90vh] overflow-hidden animate-scale-in">
            {/* Header */}
            <div className="bg-gradient-to-r from-purple-400 to-purple-500 p-8">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className="w-14 h-14 bg-white/30 backdrop-blur-sm rounded-2xl flex items-center justify-center">
                    <Bot className="w-7 h-7 text-white" />
                  </div>
                  <h3 className="text-3xl font-black text-white">
                    账号默认回复
                  </h3>
                </div>
                <button
                  onClick={() => setShowDefaultModal(false)}
                  className="p-3 bg-white/30 backdrop-blur-sm rounded-2xl hover:bg-white/40 transition-colors"
                >
                  <X className="w-6 h-6 text-white" />
                </button>
              </div>
            </div>

            {/* Body */}
            <div className="p-8 space-y-6 overflow-y-auto max-h-[60vh]">
              <div>
                <label className="flex items-center gap-2 text-sm font-black text-gray-900 mb-3">
                  <Bot className="w-5 h-5 text-purple-500" />
                  账号
                </label>
                <select
                  value={defaultForm.cookie_id}
                  onChange={(e) => setDefaultForm({ ...defaultForm, cookie_id: e.target.value })}
                  className="w-full px-6 py-4 rounded-2xl font-medium border-2 border-gray-200 focus:border-purple-400 focus:ring-4 focus:ring-purple-400/20 transition-all bg-gray-50"
                >
                  <option value="">请选择账号</option>
                  {accounts.map((acc) => (
                    <option key={acc.id} value={acc.id}>
                      {acc.nickname}
                    </option>
                  ))}
                </select>
                <p className="text-sm text-gray-500 mt-2 ml-1">🤖 为此账号设置默认回复内容</p>
              </div>

              <div className="flex items-center justify-between p-5 bg-gradient-to-r from-purple-50 to-purple-100/50 rounded-2xl border-2 border-purple-200">
                <div className="flex items-center gap-3">
                  <Power className="w-6 h-6 text-purple-500" />
                  <span className="text-base font-black text-gray-900">启用默认回复</span>
                </div>
                <button
                  type="button"
                  onClick={() => setDefaultForm({ ...defaultForm, enabled: !defaultForm.enabled })}
                  className={`relative inline-flex h-7 w-14 items-center rounded-full transition-all duration-300 ${
                    defaultForm.enabled ? 'bg-purple-500' : 'bg-gray-300'
                  }`}
                >
                  <span
                    className={`inline-block h-5 w-5 transform rounded-full bg-white shadow-lg transition-transform duration-300 ${
                      defaultForm.enabled ? 'translate-x-8' : 'translate-x-1'
                    }`}
                  />
                </button>
              </div>

              <div>
                <label className="flex items-center gap-2 text-sm font-black text-gray-900 mb-3">
                  <MessageSquare className="w-5 h-5 text-purple-500" />
                  回复内容
                </label>
                <textarea
                  value={defaultForm.reply_content}
                  onChange={(e) => setDefaultForm({ ...defaultForm, reply_content: e.target.value })}
                  placeholder="输入默认回复的内容..."
                  rows={6}
                  className="w-full px-6 py-4 rounded-2xl font-medium border-2 border-gray-200 focus:border-purple-400 focus:ring-4 focus:ring-purple-400/20 transition-all bg-gray-50 resize-none"
                />
                <p className="text-sm text-gray-500 mt-2 ml-1">💬 当没有匹配的关键词时，系统将自动发送此内容</p>
              </div>

              <div className="flex items-center justify-between p-5 bg-gradient-to-r from-blue-50 to-blue-100/50 rounded-2xl border-2 border-blue-200">
                <div className="flex items-center gap-3">
                  <span className="text-base font-black text-gray-900">🔁 只回复一次</span>
                  <span className="text-xs text-gray-500">启用后，每个对话只使用一次默认回复</span>
                </div>
                <button
                  type="button"
                  onClick={() => setDefaultForm({ ...defaultForm, reply_once: !defaultForm.reply_once })}
                  className={`relative inline-flex h-7 w-14 items-center rounded-full transition-all duration-300 ${
                    defaultForm.reply_once ? 'bg-blue-500' : 'bg-gray-300'
                  }`}
                >
                  <span
                    className={`inline-block h-5 w-5 transform rounded-full bg-white shadow-lg transition-transform duration-300 ${
                      defaultForm.reply_once ? 'translate-x-8' : 'translate-x-1'
                    }`}
                  />
                </button>
              </div>

              <div>
                <label className="flex items-center gap-2 text-sm font-black text-gray-900 mb-3">
                  <Sparkles className="w-5 h-5 text-purple-500" />
                  回复图片URL（可选）
                </label>
                <input
                  type="text"
                  value={defaultForm.reply_image_url}
                  onChange={(e) => setDefaultForm({ ...defaultForm, reply_image_url: e.target.value })}
                  placeholder="https://example.com/image.jpg"
                  className="w-full px-6 py-4 rounded-2xl font-medium border-2 border-gray-200 focus:border-purple-400 focus:ring-4 focus:ring-purple-400/20 transition-all bg-gray-50"
                />
                <p className="text-sm text-gray-500 mt-2 ml-1">🖼️ 可选：添加图片URL一起发送</p>
              </div>
            </div>

            {/* Footer */}
            <div className="p-8 bg-gray-50 border-t border-gray-100">
              <div className="flex gap-4">
                <button
                  onClick={() => setShowDefaultModal(false)}
                  className="flex-1 px-8 py-4 rounded-2xl font-bold bg-white border-2 border-gray-200 hover:bg-gray-50 hover:border-gray-300 text-gray-700 transition-all shadow-lg hover:shadow-xl"
                >
                  取消
                </button>
                <button
                  onClick={handleSaveDefault}
                  className="flex-1 px-8 py-4 rounded-2xl font-bold bg-gradient-to-r from-purple-400 to-purple-500 hover:from-purple-500 hover:to-purple-600 text-white shadow-xl hover:shadow-2xl hover:scale-105 transition-all flex items-center justify-center gap-2"
                >
                  <Save className="w-5 h-5" />
                  保存默认回复
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

export default Keywords;
