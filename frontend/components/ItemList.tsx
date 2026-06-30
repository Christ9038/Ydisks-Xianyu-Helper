import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { Item, AccountDetail, ShippingRule } from '../types';
import {
  getItems,
  getAccountDetails,
  syncItemsFromAccount,
  createItem,
  publishItem,
  previewItemPublishBatch,
  startItemPublishBatch,
  getItemPublishBatch,
  cancelItemPublishBatch,
  retryFailedItemPublishBatch,
  updateItem,
  deleteItem,
  getShippingRules
} from '../services/api';
import { ArrowRight, Box, CheckCircle2, CircleDashed, Edit, Link2, PackagePlus, Plus, RefreshCw, Save, ShoppingBag, Trash2, UploadCloud, X } from 'lucide-react';

interface ItemListProps {
  onConfigureDelivery: (item: Item) => void;
}

type BatchPhase = 'upload' | 'preview' | 'running' | 'done';

interface PublishBatchPreviewRow {
  row_no: number;
  valid: boolean;
  errors?: string[];
  cookie_id: string;
  title: string;
  price: string;
  quantity: number;
  images: string[];
}

interface PublishBatchDetailRow {
  id: number;
  row_no: number;
  cookie_id: string;
  title: string;
  price: string;
  quantity: number;
  status: string;
  item_id: string;
  item_url: string;
  error_message: string;
  images?: string[];
}

interface PublishBatchDetail {
  id: string;
  status: string;
  filename: string;
  total: number;
  success: number;
  failed: number;
  pending: number;
  running: number;
  rows: PublishBatchDetailRow[];
}

const formatItemPrice = (price?: string) => {
  const value = String(price || '').trim();
  if (!value) return '-';
  return /^[¥￥]/.test(value) ? value : `¥${value}`;
};

const ItemList: React.FC<ItemListProps> = ({ onConfigureDelivery }) => {
  const [items, setItems] = useState<Item[]>([]);
  const [shippingRules, setShippingRules] = useState<ShippingRule[]>([]);
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [selectedAccount, setSelectedAccount] = useState<string>('');
  const [loading, setLoading] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [batchLoading, setBatchLoading] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showPublishModal, setShowPublishModal] = useState(false);
  const [showBatchModal, setShowBatchModal] = useState(false);
  const [batchPhase, setBatchPhase] = useState<BatchPhase>('upload');
  const [batchFile, setBatchFile] = useState<File | null>(null);
  const [batchImagesZip, setBatchImagesZip] = useState<File | null>(null);
  const [batchPreview, setBatchPreview] = useState<{
    preview_id: string;
    total: number;
    valid: number;
    invalid: number;
    rows: PublishBatchPreviewRow[];
  } | null>(null);
  const [batchDetail, setBatchDetail] = useState<PublishBatchDetail | null>(null);
  const [selectedItem, setSelectedItem] = useState<Item | null>(null);
  const [editForm, setEditForm] = useState<Partial<Item>>({});
  const [addForm, setAddForm] = useState({
    cookie_id: '',
    item_id: '',
    item_title: '',
    item_price: '',
    item_image: ''
  });
  const [publishForm, setPublishForm] = useState({
    cookie_id: '',
    title: '',
    description: '',
    price: '',
    original_price: '',
    quantity: '1',
    postage_mode: 'free',
    postage: '',
    images: [] as File[]
  });

  const loadItems = async () => {
    const itemsList = await getItems();
    setItems(itemsList);
  };

  const loadShippingRules = async () => {
    setShippingRules(await getShippingRules());
  };

  useEffect(() => {
    Promise.all([getAccountDetails(), getItems(), getShippingRules()])
      .then(([accountList, itemList, ruleList]) => {
        setAccounts(accountList);
        setItems(itemList);
        setShippingRules(ruleList);
      })
      .catch((e) => console.error('加载商品配置失败:', e));
  }, []);

  useEffect(() => {
    if (!showBatchModal || !batchDetail?.id || batchDetail.status !== 'running') return;
    const timer = window.setInterval(async () => {
      try {
        const detail = await getItemPublishBatch(batchDetail.id);
        setBatchDetail(detail);
        if (detail.status !== 'running') {
          setBatchPhase('done');
          await loadItems();
          await loadShippingRules();
        }
      } catch (error) {
        console.error('刷新批量铺货进度失败:', error);
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [showBatchModal, batchDetail?.id, batchDetail?.status]);

  const handleSync = async () => {
      if (!selectedAccount) return alert('请先选择账号');
      setLoading(true);
      try {
        const result = await syncItemsFromAccount(selectedAccount);
        await loadItems();
        alert(result?.message || '商品同步完成');
      } catch (error: any) {
        console.error('同步商品失败:', error);
        alert(error?.message || '同步失败，请重试');
      } finally {
        setLoading(false);
      }
  };

  const handleEdit = (item: Item) => {
    setSelectedItem(item);
    setEditForm({ ...item });
    setShowEditModal(true);
  };

  const handleSaveEdit = async () => {
    if (!selectedItem) return;
    try {
      await updateItem(selectedItem.cookie_id, selectedItem.item_id, {
        item_title: editForm.item_title || '',
        item_description: editForm.item_description || '',
        item_category: editForm.item_category || '',
        item_price: editForm.item_price || '',
        item_detail: editForm.item_detail || selectedItem.item_detail || '',
      });
      await loadItems();
      await loadShippingRules();
      setShowEditModal(false);
      setSelectedItem(null);
    } catch (error) {
      console.error('更新商品失败:', error);
      alert('更新失败，请重试');
    }
  };

  const handleDelete = async (item: Item) => {
    if (confirm(`确认删除商品"${item.item_title}"吗？`)) {
      try {
        await deleteItem(item.cookie_id, item.item_id);
        setItems(prev => prev.filter(i => !(i.cookie_id === item.cookie_id && i.item_id === item.item_id)));
      } catch (error) {
        console.error('删除商品失败:', error);
        alert('删除失败，请重试');
      }
    }
  };

  const handleAddItem = async () => {
    try {
      if (!addForm.cookie_id || !addForm.item_id) {
        alert('请选择账号并填写商品ID');
        return;
      }
      await createItem(addForm.cookie_id, {
        item_id: addForm.item_id,
        item_title: addForm.item_title,
        item_price: addForm.item_price,
        item_detail: addForm.item_image ? JSON.stringify({ item_image: addForm.item_image }) : '',
      });
      await loadItems();
      setShowAddModal(false);
      setAddForm({
        cookie_id: '',
        item_id: '',
        item_title: '',
        item_price: '',
        item_image: ''
      });
    } catch (error) {
      console.error('添加商品失败:', error);
      alert('添加失败，请重试');
    }
  };

  const handlePublishItem = async () => {
    if (!publishForm.cookie_id) return alert('请选择发布账号');
    if (!publishForm.title.trim()) return alert('请填写商品标题');
    if (!publishForm.price.trim()) return alert('请填写商品价格');
    if (!publishForm.quantity || Number(publishForm.quantity) <= 0) return alert('库存数量必须大于 0');
    if (publishForm.images.length === 0) return alert('至少上传 1 张商品图片');
    if (publishForm.postage_mode === 'fixed' && !publishForm.postage.trim()) return alert('请填写一口价邮费');

    setPublishing(true);
    try {
      const result = await publishItem(publishForm);
      await loadItems();
      setShowPublishModal(false);
      setPublishForm({
        cookie_id: selectedAccount || '',
        title: '',
        description: '',
        price: '',
        original_price: '',
        quantity: '1',
        postage_mode: 'free',
        postage: '',
        images: []
      });
      if (result?.item_id) {
        const publishedItem: Item = {
          id: result.item_id,
          cookie_id: publishForm.cookie_id,
          item_id: result.item_id,
          item_title: result.item_title || publishForm.title,
          item_price: result.item_price || publishForm.price,
          item_image: result.item_image,
        };
        onConfigureDelivery(publishedItem);
        alert('商品发布成功，ID: ' + result.item_id + '，已为你打开发货规则配置');
      } else {
        alert('商品发布成功');
      }
    } catch (error: any) {
      console.error('发布商品失败:', error);
      const payload = error?.payload as any;
      if (payload?.code === 'stock_permission_missing') {
        alert('发布失败：该账号没有库存发布权限，无法按库存数量发布商品。请换账号或先在闲鱼确认库存能力。');
        return;
      }
      alert(error?.message || '发布失败，请重试');
    } finally {
      setPublishing(false);
    }
  };

  const openBatchModal = () => {
    setBatchPhase('upload');
    setBatchPreview(null);
    setBatchDetail(null);
    setBatchFile(null);
    setBatchImagesZip(null);
    setShowBatchModal(true);
  };

  const handlePreviewBatch = async () => {
    if (!batchFile) return alert('请先上传商品表格');
    if (!selectedAccount) return alert('请先选择默认发布账号');
    setBatchLoading(true);
    try {
      const result = await previewItemPublishBatch({
        file: batchFile,
        imagesZip: batchImagesZip,
        defaultCookieId: selectedAccount,
      });
      setBatchPreview(result);
      setBatchDetail(null);
      setBatchPhase('preview');
    } catch (error: any) {
      console.error('批量铺货预检失败:', error);
      alert(error?.message || '预检失败，请检查表格和图片 zip');
    } finally {
      setBatchLoading(false);
    }
  };

  const handleStartBatch = async () => {
    if (!batchPreview?.preview_id) return;
    if (batchPreview.valid <= 0) return alert('没有可发布的商品行');
    setBatchLoading(true);
    try {
      const started = await startItemPublishBatch(batchPreview.preview_id);
      const detail = await getItemPublishBatch(started.batch_id || batchPreview.preview_id);
      setBatchDetail(detail);
      setBatchPhase(detail.status === 'running' ? 'running' : 'done');
    } catch (error: any) {
      console.error('启动批量铺货失败:', error);
      alert(error?.message || '启动发布任务失败');
    } finally {
      setBatchLoading(false);
    }
  };

  const handleCancelBatch = async () => {
    if (!batchDetail?.id) return;
    if (!confirm('确认取消当前批量铺货任务吗？正在发布的单个商品可能会继续完成。')) return;
    setBatchLoading(true);
    try {
      await cancelItemPublishBatch(batchDetail.id);
      setBatchDetail(await getItemPublishBatch(batchDetail.id));
      setBatchPhase('done');
    } catch (error: any) {
      alert(error?.message || '取消失败');
    } finally {
      setBatchLoading(false);
    }
  };

  const handleRetryBatchFailed = async () => {
    if (!batchDetail?.id) return;
    setBatchLoading(true);
    try {
      await retryFailedItemPublishBatch(batchDetail.id);
      setBatchDetail(await getItemPublishBatch(batchDetail.id));
      setBatchPhase('running');
    } catch (error: any) {
      alert(error?.message || '重试失败');
    } finally {
      setBatchLoading(false);
    }
  };

  const downloadPublishTemplate = () => {
    const headers = [
      '账号ID', '标题', '描述', '价格', '原价', '库存', '邮费模式', '邮费', '图片',
      '付款后自动发货', '付款后发送的卡密', '评价后发送赠品', '评价后发送的卡密',
      '超时未评价时提醒', '发货几小时后提醒', '提醒内容', '最多提醒几次',
    ];
    const rows = [
      ['', '会员组合包自动发货', '下单后发送主卡和附赠卡。', '19.90', '29.90', '10', 'free', '', 'images/bundle-1.jpg;images/bundle-2.jpg', '是', '101:1;102:1', '是', '201:1;202:2', '是', '72', '亲，满意的话麻烦给个评价，谢谢～', '1'],
      ['', '普通商品', '仅发布商品，不创建自动化规则。', '49.90', '', '10', 'fixed', '8.00', 'https://example.com/product.jpg', '否', '', '否', '', '否', '', '', ''],
    ];
    const csv = [headers, ...rows]
      .map(row => row.map(cell => `"${String(cell).replace(/"/g, '""')}"`).join(','))
      .join('\n');
    const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = '批量铺货模板.csv';
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };

  const openAddModal = () => {
    setAddForm(prev => ({ ...prev, cookie_id: selectedAccount || prev.cookie_id }));
    setShowAddModal(true);
  };

  const openPublishModal = () => {
    setPublishForm(prev => ({ ...prev, cookie_id: selectedAccount || prev.cookie_id }));
    setShowPublishModal(true);
  };

  const rulesForItem = (item: Item) => shippingRules.filter(rule =>
    rule.cookie_id === item.cookie_id && rule.item_id === item.item_id
  );

  const batchStatusText = (status?: string) => {
    switch (status) {
      case 'preview': return '待确认';
      case 'pending': return '等待中';
      case 'running': return '发布中';
      case 'success': return '成功';
      case 'failed': return '失败';
      case 'completed': return '已完成';
      case 'canceled': return '已取消';
      default: return status || '-';
    }
  };

  const batchStatusClass = (status?: string) => {
    switch (status) {
      case 'success':
      case 'completed':
        return 'bg-emerald-50 text-emerald-700 border-emerald-100';
      case 'failed':
        return 'bg-red-50 text-red-700 border-red-100';
      case 'running':
        return 'bg-blue-50 text-blue-700 border-blue-100';
      case 'canceled':
        return 'bg-gray-100 text-gray-600 border-gray-200';
      default:
        return 'bg-amber-50 text-amber-700 border-amber-100';
    }
  };

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex justify-between items-center">
        <div>
          <h2 className="text-3xl font-bold text-gray-900">商品管理</h2>
          <p className="text-gray-500 mt-2 text-sm">监控并管理所有账号下的闲鱼商品。</p>
        </div>
        <div className="flex gap-3">
            <select
                className="ios-input px-4 py-3 rounded-xl text-sm"
                value={selectedAccount}
                onChange={e => setSelectedAccount(e.target.value)}
            >
                <option value="">选择账号以同步</option>
                {accounts.map(acc => (
                    <option key={acc.id} value={acc.id}>{acc.nickname}</option>
                ))}
            </select>
            <button
                onClick={handleSync}
                disabled={loading || !selectedAccount}
                className="ios-btn-primary flex items-center gap-2 px-6 py-3 rounded-2xl font-bold shadow-lg shadow-blue-200 disabled:opacity-50"
            >
                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                同步商品
            </button>
            <button
              onClick={openAddModal}
              className="px-5 py-3 rounded-2xl font-bold bg-gray-900 text-white hover:bg-gray-800 transition-colors flex items-center gap-2 shadow-lg"
            >
              <Plus className="w-4 h-4" />
              添加商品
            </button>
            <button
              onClick={openPublishModal}
              className="px-5 py-3 rounded-2xl font-bold bg-emerald-600 text-white hover:bg-emerald-700 transition-colors flex items-center gap-2 shadow-lg shadow-emerald-100"
            >
              <PackagePlus className="w-4 h-4" />
              发布商品
            </button>
            <button
              onClick={openBatchModal}
              className="px-5 py-3 rounded-2xl font-bold bg-blue-600 text-white hover:bg-blue-700 transition-colors flex items-center gap-2 shadow-lg shadow-blue-100"
            >
              <UploadCloud className="w-4 h-4" />
              批量铺货
            </button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
          {items.map(item => {
            const linkedRules = rulesForItem(item);
            const hasRule = linkedRules.length > 0;
            return (
              <div key={`${item.cookie_id}-${item.item_id}`} className="ios-card p-4 rounded-3xl hover:shadow-lg transition-all group relative flex flex-col">
                  <div className="absolute top-3 right-3 flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity z-10">
                      <button
                        onClick={() => handleEdit(item)}
                        className="p-2 bg-white/90 backdrop-blur rounded-lg shadow-md text-gray-600 hover:bg-[#0094f7] hover:text-white transition-colors"
                        title="编辑"
                      >
                        <Edit className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(item)}
                        className="p-2 bg-white/90 backdrop-blur rounded-lg shadow-md hover:bg-red-100 text-red-500 transition-colors"
                        title="删除"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                  </div>
                  <div className="aspect-square bg-gray-100 rounded-2xl mb-4 overflow-hidden relative">
                      {item.item_image ? (
                          <img src={item.item_image} alt="" className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" />
                      ) : (
                          <div className="w-full h-full flex items-center justify-center text-gray-300">
                              <Box className="w-10 h-10" />
                          </div>
                      )}
                      <div className="absolute top-2 left-2 bg-black/50 backdrop-blur-md text-white text-xs font-bold px-2 py-1 rounded-lg">
                          {formatItemPrice(item.item_price)}
                      </div>
                  </div>
                  <h3 className="font-bold text-gray-900 line-clamp-2 text-sm mb-2 h-10">{item.item_title}</h3>
                  <div className="flex justify-between items-center text-xs text-gray-500 mb-3">
                      <span className="bg-gray-100 px-2 py-1 rounded-md truncate max-w-[100px]">ID: {item.item_id}</span>
                      <span className={`inline-flex items-center gap-1 font-bold ${hasRule ? 'text-emerald-600' : 'text-amber-600'}`}>
                        {hasRule ? <CheckCircle2 className="w-3.5 h-3.5" /> : <CircleDashed className="w-3.5 h-3.5" />}
                        {hasRule ? `${linkedRules.length} 条规则` : '未配置规则'}
                      </span>
                  </div>
                  <div className="space-y-2 mt-auto">
                      <button
                        onClick={() => onConfigureDelivery(item)}
                        className={`w-full flex items-center justify-between gap-2 px-3 py-2.5 rounded-xl text-xs font-extrabold transition-all ${hasRule ? 'bg-gray-900 text-white hover:bg-black' : 'bg-[#0094f7] text-white hover:bg-[#0071e3] shadow-md shadow-blue-100'}`}
                      >
                        <span className="flex items-center gap-2"><Link2 className="w-4 h-4" />{hasRule ? '查看并编辑发货规则' : '关联自动发货规则'}</span>
                        <ArrowRight className="w-4 h-4" />
                      </button>
                  </div>
              </div>
            );
          })}
          {items.length === 0 && (
             <div className="col-span-full py-20 text-center text-gray-400">
                 <ShoppingBag className="w-12 h-12 mx-auto mb-4 opacity-30" />
                 暂无商品数据，请选择账号进行同步
             </div>
          )}
      </div>

      {showEditModal && selectedItem && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '560px'}}>
            <div className="modal-header flex items-center justify-between">
              <div>
                <h3 className="text-xl font-extrabold text-gray-900">编辑商品</h3>
                <p className="text-xs text-gray-500 mt-1">ID: {selectedItem.item_id}</p>
              </div>
              <button onClick={() => setShowEditModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>
            <div className="modal-body space-y-4">
              <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="商品标题" value={editForm.item_title || ''} onChange={e => setEditForm({...editForm, item_title: e.target.value})} />
              <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="价格" value={editForm.item_price || ''} onChange={e => setEditForm({...editForm, item_price: e.target.value})} />
              <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="分类" value={editForm.item_category || ''} onChange={e => setEditForm({...editForm, item_category: e.target.value})} />
              <textarea className="w-full ios-input px-4 py-3 rounded-xl h-28 resize-none" placeholder="描述" value={editForm.item_description || ''} onChange={e => setEditForm({...editForm, item_description: e.target.value})} />
            </div>
            <div className="modal-footer">
              <button onClick={handleSaveEdit} className="w-full ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2">
                <Save className="w-4 h-4" />
                保存
              </button>
            </div>
          </div>
        </div>
      , document.body)}

      {showAddModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '720px'}}>
            <div className="modal-header flex items-center justify-between">
              <div>
                <h3 className="text-xl font-extrabold text-gray-900">添加商品</h3>
                <p className="text-xs text-gray-500 mt-1">手动建立商品与自动发货规则的关联</p>
              </div>
              <button onClick={() => setShowAddModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>
            <div className="modal-body space-y-5">
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">所属账号</label>
                <select className="w-full ios-input px-4 py-3 rounded-xl" value={addForm.cookie_id} onChange={e => setAddForm({...addForm, cookie_id: e.target.value})}>
                  <option value="">选择账号</option>
                  {accounts.map(acc => <option key={acc.id} value={acc.id}>{acc.nickname || acc.remark || acc.id}</option>)}
                </select>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">商品 ID</label>
                  <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="输入闲鱼商品 ID" value={addForm.item_id} onChange={e => setAddForm({...addForm, item_id: e.target.value})} />
                </div>
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">商品价格</label>
                  <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="例如 99.00" value={addForm.item_price} onChange={e => setAddForm({...addForm, item_price: e.target.value})} />
                </div>
              </div>
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">商品标题</label>
                <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="输入商品标题" value={addForm.item_title} onChange={e => setAddForm({...addForm, item_title: e.target.value})} />
              </div>
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">图片 URL</label>
                <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="https://..." value={addForm.item_image} onChange={e => setAddForm({...addForm, item_image: e.target.value})} />
              </div>
            </div>
            <div className="modal-footer">
              <button onClick={handleAddItem} className="w-full ios-btn-primary px-6 py-3.5 rounded-xl font-bold flex items-center justify-center gap-2">
                <Plus className="w-4 h-4" />
                添加商品
              </button>
            </div>
          </div>
        </div>
      , document.body)}

      {showPublishModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '820px'}}>
            <div className="modal-header flex items-center justify-between">
              <div>
                <h3 className="text-xl font-extrabold text-gray-900">发布商品到闲鱼</h3>
                <p className="text-xs text-gray-500 mt-1">普通单规格发布；库存数量会写入闲鱼发布参数，用于判断账号库存能力。</p>
              </div>
              <button onClick={() => setShowPublishModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>
            <div className="modal-body space-y-5">
              <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 leading-6">
                发布时必须填写库存。若账号没有库存发布能力，后端会返回明确的“库存权限不足”错误，不会误报为普通发布失败。
              </div>
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">发布账号</label>
                <select className="w-full ios-input px-4 py-3 rounded-xl" value={publishForm.cookie_id} onChange={e => setPublishForm({...publishForm, cookie_id: e.target.value})}>
                  <option value="">选择账号</option>
                  {accounts.map(acc => <option key={acc.id} value={acc.id}>{acc.nickname || acc.remark || acc.id}</option>)}
                </select>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">商品标题</label>
                  <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="例如：会员月卡自动发货" value={publishForm.title} onChange={e => setPublishForm({...publishForm, title: e.target.value})} />
                </div>
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">库存数量</label>
                  <input className="w-full ios-input px-4 py-3 rounded-xl" type="number" min="1" placeholder="必须大于 0" value={publishForm.quantity} onChange={e => setPublishForm({...publishForm, quantity: e.target.value})} />
                </div>
              </div>
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">商品描述</label>
                <textarea className="w-full ios-input px-4 py-3 rounded-xl h-28 resize-none" placeholder="描述会用于自动识别类目；留空时使用标题" value={publishForm.description} onChange={e => setPublishForm({...publishForm, description: e.target.value})} />
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">售价</label>
                  <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="99.00" value={publishForm.price} onChange={e => setPublishForm({...publishForm, price: e.target.value})} />
                </div>
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">原价（可选）</label>
                  <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="129.00" value={publishForm.original_price} onChange={e => setPublishForm({...publishForm, original_price: e.target.value})} />
                </div>
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">运费方式</label>
                  <select className="w-full ios-input px-4 py-3 rounded-xl" value={publishForm.postage_mode} onChange={e => setPublishForm({...publishForm, postage_mode: e.target.value})}>
                    <option value="free">包邮</option>
                    <option value="distance">按距离计费</option>
                    <option value="fixed">一口价邮费</option>
                    <option value="none">无需邮寄</option>
                  </select>
                </div>
              </div>
              {publishForm.postage_mode === 'fixed' && (
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-700">一口价邮费</label>
                  <input className="w-full ios-input px-4 py-3 rounded-xl" placeholder="例如 8.00" value={publishForm.postage} onChange={e => setPublishForm({...publishForm, postage: e.target.value})} />
                </div>
              )}
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-700">商品图片（1-9 张）</label>
                <label className="flex min-h-[120px] cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed border-gray-200 bg-gray-50 px-4 py-6 text-center hover:border-emerald-300 hover:bg-emerald-50/50 transition-colors">
                  <UploadCloud className="w-8 h-8 text-emerald-600 mb-2" />
                  <span className="text-sm font-bold text-gray-800">选择图片</span>
                  <span className="text-xs text-gray-500 mt-1">{publishForm.images.length ? '已选择 ' + publishForm.images.length + ' 张' : '支持 JPG / PNG / GIF'}</span>
                  <input
                    className="hidden"
                    type="file"
                    accept="image/*"
                    multiple
                    onChange={e => setPublishForm({...publishForm, images: Array.from(e.target.files || []).slice(0, 9)})}
                  />
                </label>
                {publishForm.images.length > 0 && (
                  <div className="grid grid-cols-4 sm:grid-cols-6 gap-3">
                    {publishForm.images.map((file, index) => (
                      <div key={file.name + index} className="aspect-square rounded-xl bg-gray-100 overflow-hidden border border-gray-100">
                        <img src={URL.createObjectURL(file)} alt="" className="w-full h-full object-cover" />
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
            <div className="modal-footer">
              <button disabled={publishing} onClick={handlePublishItem} className="w-full bg-emerald-600 hover:bg-emerald-700 disabled:opacity-60 text-white px-6 py-3.5 rounded-xl font-bold flex items-center justify-center gap-2">
                <PackagePlus className="w-4 h-4" />
                {publishing ? '正在发布...' : '发布到闲鱼'}
              </button>
            </div>
          </div>
        </div>
      , document.body)}

      {showBatchModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{maxWidth: '980px'}}>
            <div className="modal-header flex items-center justify-between">
              <div>
                <h3 className="text-xl font-extrabold text-gray-900">批量铺货</h3>
                <p className="text-xs text-gray-500 mt-1">上传商品表格和图片 zip，先预检，再逐条发布到闲鱼。</p>
              </div>
              <button onClick={() => setShowBatchModal(false)} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭">
                <X className="w-5 h-5 text-gray-500" />
              </button>
            </div>

            <div className="modal-body space-y-5">
              <div className="grid grid-cols-4 gap-2">
                {[
                  ['upload', '1 上传'],
                  ['preview', '2 预检'],
                  ['running', '3 发布'],
                  ['done', '4 结果']
                ].map(([phase, label]) => (
                  <div key={phase} className={`rounded-xl px-3 py-2 text-center text-xs font-extrabold border ${batchPhase === phase ? 'bg-blue-600 text-white border-blue-600' : 'bg-gray-50 text-gray-500 border-gray-100'}`}>
                    {label}
                  </div>
                ))}
              </div>

              {batchPhase === 'upload' && (
                <div className="space-y-5">
                  <div className="rounded-2xl border border-blue-100 bg-blue-50 p-4 text-sm text-blue-900 leading-6">
                    <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-3">
                      <div>
                        <div className="font-extrabold">先下载模板，再按字段填写。</div>
                        <div>图片字段写 zip 内相对路径，多个图片用英文分号分隔，例如 <span className="font-mono font-bold">images/a.jpg;images/b.jpg</span>。也支持直接填写图片 URL。</div>
                      </div>
                      <button
                        type="button"
                        onClick={downloadPublishTemplate}
                        className="shrink-0 rounded-xl bg-blue-600 px-4 py-2 text-sm font-extrabold text-white hover:bg-blue-700"
                      >
                        下载CSV模板
                      </button>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <label className="block text-sm font-bold text-gray-700">默认发布账号</label>
                    <select
                      className="w-full ios-input px-4 py-3 rounded-xl"
                      value={selectedAccount}
                      onChange={e => setSelectedAccount(e.target.value)}
                    >
                      <option value="">选择账号</option>
                      {accounts.map(acc => <option key={acc.id} value={acc.id}>{acc.nickname || acc.remark || acc.id}</option>)}
                    </select>
                    <p className="text-xs text-gray-500">表格中“账号ID”为空时，会使用这里选择的账号。</p>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <label className="flex min-h-[150px] cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed border-gray-200 bg-gray-50 px-4 py-6 text-center hover:border-blue-300 hover:bg-blue-50 transition-colors">
                      <UploadCloud className="w-9 h-9 text-blue-600 mb-3" />
                      <span className="text-sm font-extrabold text-gray-900">上传商品表格</span>
                      <span className="text-xs text-gray-500 mt-1">{batchFile ? batchFile.name : '支持 .xlsx / .csv / .tsv'}</span>
                      <input
                        className="hidden"
                        type="file"
                        accept=".xlsx,.csv,.tsv"
                        onChange={e => setBatchFile(e.target.files?.[0] || null)}
                      />
                    </label>
                    <label className="flex min-h-[150px] cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed border-gray-200 bg-gray-50 px-4 py-6 text-center hover:border-emerald-300 hover:bg-emerald-50 transition-colors">
                      <UploadCloud className="w-9 h-9 text-emerald-600 mb-3" />
                      <span className="text-sm font-extrabold text-gray-900">上传图片 zip（可选）</span>
                      <span className="text-xs text-gray-500 mt-1">{batchImagesZip ? batchImagesZip.name : '表格图片字段使用 zip 内相对路径'}</span>
                      <input
                        className="hidden"
                        type="file"
                        accept=".zip"
                        onChange={e => setBatchImagesZip(e.target.files?.[0] || null)}
                      />
                    </label>
                  </div>

                  <div className="rounded-2xl bg-gray-50 border border-gray-100 p-4 space-y-3">
                    <div>
                      <div className="text-sm font-extrabold text-gray-900">字段说明</div>
                      <p className="text-xs text-gray-500 mt-1">照着下面的“什么时候填写”处理即可。预检发现问题时，会指出具体哪一行需要修改。</p>
                    </div>

                    <div className="rounded-xl border border-blue-100 bg-blue-50 p-4 text-xs text-blue-950">
                      <div className="text-sm font-extrabold">“付款后发送的卡密”怎么填</div>
                      <div className="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-3">
                        <div className="rounded-lg bg-white/80 p-3">
                          <code className="font-bold text-blue-700">101</code>
                          <p className="mt-1 leading-5">从卡密组 101 立即发送 1 份。卡密组 ID 可以在“卡密库存”页面查看。</p>
                        </div>
                        <div className="rounded-lg bg-white/80 p-3">
                          <code className="font-bold text-blue-700">101:2</code>
                          <p className="mt-1 leading-5">每购买 1 件，就从卡密组 101 发送 2 份。买家购买 3 件时会发送 6 份。</p>
                        </div>
                        <div className="rounded-lg bg-white/80 p-3">
                          <code className="font-bold text-blue-700">101:1:0;102:2:3</code>
                          <p className="mt-1 leading-5">先立即发送卡密组 101 的 1 份，再等待 3 秒发送卡密组 102 的 2 份。</p>
                        </div>
                      </div>
                      <p className="mt-3 leading-5 text-blue-800">
                        每一组依次写“卡密组 ID : 每件发送几份 : 等待几秒”。份数不写时按 1 份处理，等待时间不写时立即发送。需要发送多种卡密时，用英文分号 <code className="font-bold">;</code> 隔开。
                      </p>
                    </div>
                    <div className="overflow-x-auto rounded-xl border border-gray-100 bg-white">
                      <table className="w-full text-left text-xs">
                        <thead className="bg-gray-50 text-gray-500">
                          <tr>
                            <th className="px-3 py-2">字段</th>
                            <th className="px-3 py-2">什么时候填写</th>
                            <th className="px-3 py-2">填写方法</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-gray-50 text-gray-700">
                          {[
                            ['账号ID', '没有在上方选择默认账号时填写', '填写账号 ID；已经选择默认账号时可以留空'],
                            ['标题', '每个商品都要填', '填写买家能看到的商品标题'],
                            ['描述', '可以留空', '留空时会使用商品标题作为描述'],
                            ['价格', '每个商品都要填', '只填数字，例如 19.90'],
                            ['原价', '可以留空', '需要展示划线原价时填写，例如 29.90'],
                            ['库存', '可以留空', '留空按 1 件处理；填写时必须大于 0'],
                            ['邮费模式', '可以留空', '留空表示包邮；包邮填 free，固定邮费填 fixed'],
                            ['邮费', '邮费模式填 fixed 时填写', '只填数字，例如 8.00'],
                            ['图片', '每个商品都要填', '填写 zip 内图片路径或图片网址；多张图片用英文分号隔开'],
                            ['付款后自动发货', '需要付款后自动发货时填写', '填“是”表示开启；不需要时填“否”或留空'],
                            ['付款后发送的卡密', '“付款后自动发货”填“是”时填写', '从“卡密库存”页面取得卡密组 ID，按上方示例填写'],
                            ['评价后发送赠品', '需要评价赠品时填写', '填“是”表示开启；不需要时填“否”或留空'],
                            ['评价后发送的卡密', '“评价后发送赠品”填“是”时填写', '格式和付款后发送的卡密相同，也可以同时发送多个卡密组'],
                            ['超时未评价时提醒', '需要自动求评价时填写', '填“是”表示开启；不需要时填“否”或留空'],
                            ['发货几小时后提醒', '“超时未评价时提醒”填“是”时填写', '填写等待小时数；留空按 72 小时处理'],
                            ['提醒内容', '“超时未评价时提醒”填“是”时填写', '填写要发送给买家的求评价消息'],
                            ['最多提醒几次', '可以留空', '留空只提醒 1 次'],
                          ].map(([name, when, desc]) => (
                            <tr key={name}>
                              <td className="px-3 py-2 font-bold text-gray-900 whitespace-nowrap">{name}</td>
                              <td className={`px-3 py-2 min-w-[210px] font-bold ${when === '每个商品都要填' ? 'text-red-600' : when === '可以留空' ? 'text-gray-500' : 'text-amber-700'}`}>{when}</td>
                              <td className="px-3 py-2 min-w-[260px]">{desc}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                </div>
              )}

              {batchPhase === 'preview' && batchPreview && (
                <div className="space-y-4">
                  <div className="grid grid-cols-3 gap-3">
                    <div className="rounded-2xl bg-gray-50 p-4 border border-gray-100">
                      <div className="text-xs font-bold text-gray-500">总行数</div>
                      <div className="text-2xl font-extrabold text-gray-900 mt-1">{batchPreview.total}</div>
                    </div>
                    <div className="rounded-2xl bg-emerald-50 p-4 border border-emerald-100">
                      <div className="text-xs font-bold text-emerald-700">可发布</div>
                      <div className="text-2xl font-extrabold text-emerald-700 mt-1">{batchPreview.valid}</div>
                    </div>
                    <div className="rounded-2xl bg-red-50 p-4 border border-red-100">
                      <div className="text-xs font-bold text-red-700">有问题</div>
                      <div className="text-2xl font-extrabold text-red-700 mt-1">{batchPreview.invalid}</div>
                    </div>
                  </div>

                  <div className="max-h-[380px] overflow-y-auto rounded-2xl border border-gray-100">
                    <table className="w-full text-left text-sm">
                      <thead className="sticky top-0 bg-white text-xs text-gray-400 border-b border-gray-100">
                        <tr>
                          <th className="px-4 py-3">行号</th>
                          <th className="px-4 py-3">状态</th>
                          <th className="px-4 py-3">标题</th>
                          <th className="px-4 py-3">价格/库存</th>
                          <th className="px-4 py-3">图片</th>
                          <th className="px-4 py-3">问题</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-gray-50">
                        {batchPreview.rows.map(row => (
                          <tr key={row.row_no} className="hover:bg-gray-50">
                            <td className="px-4 py-3 font-mono text-xs">{row.row_no}</td>
                            <td className="px-4 py-3">
                              <span className={`inline-flex px-2 py-1 rounded-lg border text-xs font-extrabold ${row.valid ? 'bg-emerald-50 text-emerald-700 border-emerald-100' : 'bg-red-50 text-red-700 border-red-100'}`}>
                                {row.valid ? '可发布' : '需修正'}
                              </span>
                            </td>
                            <td className="px-4 py-3 font-bold text-gray-900 max-w-[240px] truncate">{row.title || '-'}</td>
                            <td className="px-4 py-3 text-gray-600">¥{row.price || '-'} / {row.quantity || 1}</td>
                            <td className="px-4 py-3 text-gray-600">{row.images?.length || 0} 张</td>
                            <td className="px-4 py-3 text-red-600 text-xs max-w-[280px]">{row.errors?.join('；') || '-'}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {(batchPhase === 'running' || batchPhase === 'done') && batchDetail && (
                <div className="space-y-4">
                  <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
                    <div className={`rounded-2xl p-4 border ${batchStatusClass(batchDetail.status)}`}>
                      <div className="text-xs font-bold opacity-70">任务状态</div>
                      <div className="text-xl font-extrabold mt-1">{batchStatusText(batchDetail.status)}</div>
                    </div>
                    <div className="rounded-2xl bg-gray-50 p-4 border border-gray-100">
                      <div className="text-xs font-bold text-gray-500">总数</div>
                      <div className="text-xl font-extrabold text-gray-900 mt-1">{batchDetail.total}</div>
                    </div>
                    <div className="rounded-2xl bg-emerald-50 p-4 border border-emerald-100">
                      <div className="text-xs font-bold text-emerald-700">成功</div>
                      <div className="text-xl font-extrabold text-emerald-700 mt-1">{batchDetail.success}</div>
                    </div>
                    <div className="rounded-2xl bg-red-50 p-4 border border-red-100">
                      <div className="text-xs font-bold text-red-700">失败</div>
                      <div className="text-xl font-extrabold text-red-700 mt-1">{batchDetail.failed}</div>
                    </div>
                    <div className="rounded-2xl bg-blue-50 p-4 border border-blue-100">
                      <div className="text-xs font-bold text-blue-700">等待</div>
                      <div className="text-xl font-extrabold text-blue-700 mt-1">{batchDetail.pending}</div>
                    </div>
                  </div>

                  <div className="max-h-[420px] overflow-y-auto rounded-2xl border border-gray-100">
                    <table className="w-full text-left text-sm">
                      <thead className="sticky top-0 bg-white text-xs text-gray-400 border-b border-gray-100">
                        <tr>
                          <th className="px-4 py-3">行号</th>
                          <th className="px-4 py-3">状态</th>
                          <th className="px-4 py-3">标题</th>
                          <th className="px-4 py-3">商品ID</th>
                          <th className="px-4 py-3">错误原因</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-gray-50">
                        {batchDetail.rows.map(row => (
                          <tr key={row.id} className="hover:bg-gray-50">
                            <td className="px-4 py-3 font-mono text-xs">{row.row_no}</td>
                            <td className="px-4 py-3">
                              <span className={`inline-flex px-2 py-1 rounded-lg border text-xs font-extrabold ${batchStatusClass(row.status)}`}>
                                {batchStatusText(row.status)}
                              </span>
                            </td>
                            <td className="px-4 py-3 font-bold text-gray-900 max-w-[260px] truncate">{row.title}</td>
                            <td className="px-4 py-3 text-xs font-mono">
                              {row.item_url ? <a href={row.item_url} target="_blank" rel="noreferrer" className="text-blue-600 hover:underline">{row.item_id}</a> : (row.item_id || '-')}
                            </td>
                            <td className="px-4 py-3 text-red-600 text-xs max-w-[340px]">{row.error_message || '-'}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </div>

            <div className="modal-footer">
              {batchPhase === 'upload' && (
                <button disabled={batchLoading || !batchFile || !selectedAccount} onClick={handlePreviewBatch} className="w-full ios-btn-primary px-6 py-3.5 rounded-xl font-bold flex items-center justify-center gap-2 disabled:opacity-50">
                  <RefreshCw className={`w-4 h-4 ${batchLoading ? 'animate-spin' : ''}`} />
                  {batchLoading ? '正在预检...' : '开始预检'}
                </button>
              )}
              {batchPhase === 'preview' && batchPreview && (
                <div className="flex gap-3 w-full">
                  <button disabled={batchLoading} onClick={() => setBatchPhase('upload')} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-800 font-bold">
                    返回修改
                  </button>
                  <button disabled={batchLoading || batchPreview.valid <= 0} onClick={handleStartBatch} className="flex-1 ios-btn-primary px-6 py-3.5 rounded-xl font-bold flex items-center justify-center gap-2 disabled:opacity-50">
                    <PackagePlus className="w-4 h-4" />
                    {batchLoading ? '启动中...' : `确认发布 ${batchPreview.valid} 个商品`}
                  </button>
                </div>
              )}
              {(batchPhase === 'running' || batchPhase === 'done') && batchDetail && (
                <div className="flex gap-3 w-full">
                  {batchDetail.status === 'running' ? (
                    <button disabled={batchLoading} onClick={handleCancelBatch} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-900 text-white hover:bg-black font-bold">
                      取消任务
                    </button>
                  ) : (
                    <button onClick={() => window.open(`/items/publish-batches/${batchDetail.id}/result.csv`, '_blank')} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-900 text-white hover:bg-black font-bold">
                      下载结果
                    </button>
                  )}
                  {batchDetail.failed > 0 && batchDetail.status !== 'running' && (
                    <button disabled={batchLoading} onClick={handleRetryBatchFailed} className="flex-1 ios-btn-primary px-6 py-3.5 rounded-xl font-bold flex items-center justify-center gap-2">
                      <RefreshCw className={`w-4 h-4 ${batchLoading ? 'animate-spin' : ''}`} />
                      重试失败项
                    </button>
                  )}
                  {batchDetail.status !== 'running' && (
                    <button onClick={() => { setShowBatchModal(false); loadItems(); loadShippingRules(); }} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-800 font-bold">
                      完成
                    </button>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>
      , document.body)}
    </div>
  );
};

export default ItemList;
