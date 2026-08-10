import React, { useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { selectActivePublishBatch } from './itemPublishBatchState';
import { Item, AccountDetail, ShippingRule } from '../types';
import {
  getItems,
  getAccountDetails,
  syncItemsFromAccount,
  createItem,
  publishItem,
  recommendPublishCategory,
  previewItemPublishBatch,
  startItemPublishBatch,
  getItemPublishBatch,
  getItemPublishBatches,
  deleteItemPublishBatch,
  cancelItemPublishBatch,
  retryFailedItemPublishBatch,
  updateItem,
  deleteItem,
  getShippingRules
} from '../services/api';
import { ArrowRight, Box, CheckCircle2, CircleDashed, Edit, Filter, Link2, PackagePlus, Plus, RefreshCw, Save, Search, ShoppingBag, Trash2, UploadCloud, User, X } from 'lucide-react';

interface ItemListProps {
  onConfigureDelivery: (item: Item) => void;
}

type BatchPhase = 'upload' | 'preview' | 'running' | 'done';

interface PublishCategory {
  cat_id: string;
  cat_name: string;
  channel_cat_id?: string;
  tb_cat_id?: string;
}

interface PublishBatchPreviewRow {
  row_no: number;
  valid: boolean;
  errors?: string[];
  cookie_id: string;
  title: string;
  price: string;
  quantity: number;
  images: string[];
  category: PublishCategory;
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
  failure_kind: string;
  images?: string[];
  category: PublishCategory;
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
  retryable: number;
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
  const [accountFilter, setAccountFilter] = useState<string>('');
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
  const [batchCategoryKeyword, setBatchCategoryKeyword] = useState('');
  const [batchCategoryLoading, setBatchCategoryLoading] = useState(false);
  const [batchFallbackCategory, setBatchFallbackCategory] = useState({
    catId: '',
    catName: '',
    channelCatId: '',
    tbCatId: '',
  });
  const [batchPreview, setBatchPreview] = useState<{
    preview_id: string;
    total: number;
    valid: number;
    invalid: number;
    rows: PublishBatchPreviewRow[];
  } | null>(null);
  const [batchDetail, setBatchDetail] = useState<PublishBatchDetail | null>(null);
  const [recentBatch, setRecentBatch] = useState<PublishBatchDetail | null>(null);
  const batchPollInFlight = useRef(false);
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
  const [publishImagePreviews, setPublishImagePreviews] = useState<{ key: string; url: string }[]>([]);

  useEffect(() => {
    if (!showPublishModal || publishForm.images.length === 0) {
      setPublishImagePreviews([]);
      return;
    }
    const previews = publishForm.images.map((file, index) => ({
      key: file.name + index,
      url: URL.createObjectURL(file),
    }));
    setPublishImagePreviews(previews);
    return () => {
      previews.forEach(preview => URL.revokeObjectURL(preview.url));
    };
  }, [showPublishModal, publishForm.images]);

  const loadItems = async () => {
    const itemsList = await getItems();
    setItems(itemsList);
  };

  const loadShippingRules = async () => {
    setShippingRules(await getShippingRules());
  };

  useEffect(() => {
    Promise.all([getAccountDetails(), getItems(), getShippingRules(), getItemPublishBatches(20)])
      .then(([accountList, itemList, ruleList, batches]) => {
        setAccounts(accountList);
        setItems(itemList);
        setShippingRules(ruleList);
        const recoverable = batches.find(batch => ['running', 'canceling'].includes(batch.status))
          || batches.find(batch => batch.status !== 'preview');
        setRecentBatch(recoverable || null);
      })
      .catch((e) => console.error('加载商品配置失败:', e));
  }, []);

  useEffect(() => {
    if (!showBatchModal || !batchDetail?.id || !['running', 'canceling'].includes(batchDetail.status)) return;
    const timer = window.setInterval(async () => {
      if (batchPollInFlight.current) return;
      batchPollInFlight.current = true;
      try {
        const detail = await getItemPublishBatch(batchDetail.id);
        setBatchDetail(detail);
        setRecentBatch(detail);
        if (!['running', 'canceling'].includes(detail.status)) {
          setBatchPhase('done');
          await loadItems();
          await loadShippingRules();
        }
      } catch (error) {
        console.error('刷新批量铺货进度失败:', error);
      } finally {
        batchPollInFlight.current = false;
      }
    }, 3000);
    return () => {
      window.clearInterval(timer);
    };
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

  const openBatchModal = async () => {
    setBatchPhase('upload');
    setBatchPreview(null);
    setBatchDetail(null);
    setBatchFile(null);
    setBatchImagesZip(null);
    setBatchCategoryKeyword('');
    setBatchFallbackCategory({ catId: '', catName: '', channelCatId: '', tbCatId: '' });
    setShowBatchModal(true);
    setBatchLoading(true);
    try {
      const batches = await getItemPublishBatches(20);
      const recoverable = selectActivePublishBatch(batches);
      if (recoverable?.id) {
        const detail = await getItemPublishBatch(recoverable.id);
        setRecentBatch(detail);
        setBatchDetail(detail);
        setBatchPhase(['running', 'canceling'].includes(detail.status) ? 'running' : 'done');
      }
    } catch (error) {
      console.error('恢复最近批量铺货任务失败:', error);
    } finally {
      setBatchLoading(false);
    }
  };

  const handleRecommendBatchCategory = async () => {
    const keyword = batchCategoryKeyword.trim();
    if (!selectedAccount) return alert('请先选择默认发布账号');
    if (!keyword) return alert('请输入类目关键词');
    setBatchCategoryLoading(true);
    try {
      const result = await recommendPublishCategory(selectedAccount, keyword);
      const category = result.category;
      setBatchFallbackCategory({
        catId: category.cat_id,
        catName: category.cat_name,
        channelCatId: category.channel_cat_id,
        tbCatId: category.tb_cat_id || '',
      });
    } catch (error: any) {
      console.error('获取推荐类目失败:', error);
      alert(error?.message || '没有匹配到类目，请换一个更具体的关键词');
    } finally {
      setBatchCategoryLoading(false);
    }
  };

  const openRecentBatchResult = async () => {
    if (!recentBatch?.id) return;
    setBatchLoading(true);
    setShowBatchModal(true);
    try {
      const detail = await getItemPublishBatch(recentBatch.id);
      setBatchDetail(detail);
      setBatchPhase(['running', 'canceling'].includes(detail.status) ? 'running' : 'done');
    } catch (error) {
      console.error('加载最近批量铺货结果失败:', error);
    } finally {
      setBatchLoading(false);
    }
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
        fallbackCategory: batchFallbackCategory,
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
      setRecentBatch(detail);
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
      const result = await cancelItemPublishBatch(batchDetail.id);
      const detail = await getItemPublishBatch(batchDetail.id);
      setBatchDetail(detail);
      setBatchPhase(result?.status === 'canceling' || detail.status === 'canceling' ? 'running' : 'done');
    } catch (error: any) {
      alert(error?.message || '取消失败');
    } finally {
      setBatchLoading(false);
    }
  };

  const abandonBatchPreview = async () => {
    const previewId = batchPreview?.preview_id;
    if (previewId && batchPhase === 'preview') {
      try {
        await deleteItemPublishBatch(previewId);
      } catch (error) {
        console.error('清理批量铺货预检失败:', error);
      }
    }
    setBatchPreview(null);
    setBatchPhase('upload');
  };

  const closeBatchModal = async () => {
    await abandonBatchPreview();
    setShowBatchModal(false);
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
      '类目ID', '类目名称', '频道类目ID', '淘宝类目ID',
      '付款发货启用', '付款发货内容', '评价赠品启用', '评价赠品内容',
      '求评价启用', '求评价等待小时', '求评价文案', '求评价最多次数',
    ];
    const rows = [
      ['', '会员组合包自动发货', '下单后发送主卡和附赠卡。', '19.90', '29.90', '10', 'free', '', 'images/bundle-1.jpg;images/bundle-2.jpg', '', '', '', '', '是', '101:1;102:1', '是', '201:1;202:2', '是', '72', '亲，满意的话麻烦给个评价，谢谢～', '1'],
      ['', '普通商品', '仅发布商品，不创建自动化规则。', '49.90', '', '10', 'fixed', '8.00', 'https://example.com/product.jpg', '', '', '', '', '否', '', '否', '', '否', '', '', ''],
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
  ).length > 0
    ? shippingRules.filter(rule => rule.cookie_id === item.cookie_id && rule.item_id === item.item_id)
    : shippingRules.filter(rule => rule.cookie_id === item.cookie_id && !rule.item_id);

  const batchStatusText = (status?: string) => {
    switch (status) {
      case 'preview': return '待确认';
      case 'pending': return '等待中';
      case 'running': return '发布中';
      case 'canceling': return '正在安全取消';
      case 'success': return '成功';
      case 'failed': return '失败';
      case 'completed': return '已完成';
      case 'partially_failed': return '部分失败';
      case 'canceled': return '已取消';
      default: return status || '-';
    }
  };

  const batchStatusClass = (status?: string) => {
    switch (status) {
      case 'success':
      case 'completed':
        return 'bg-emerald-50 text-emerald-700 border-emerald-100';
      case 'partially_failed':
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

  const accountMap = useMemo(
    () => new Map(accounts.map(account => [account.id, account])),
    [accounts],
  );
  const visibleItems = useMemo(
    () => accountFilter ? items.filter(item => item.cookie_id === accountFilter) : items,
    [accountFilter, items],
  );
  const accountName = (cookieId: string) => {
    const account = accountMap.get(cookieId);
    const name = account?.remark || account?.nickname;
    return name ? `${name} · ${cookieId.slice(0, 6)}` : `账号 ${cookieId.slice(0, 8)}`;
  };
  const accountNickname = (cookieId: string) => {
    const account = accountMap.get(cookieId);
    return account?.remark || account?.nickname || '未命名账号';
  };

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col xl:flex-row xl:justify-between xl:items-center gap-4">
        <div>
          <h2 className="text-3xl font-bold text-gray-900">商品管理</h2>
          <p className="text-gray-500 mt-2 text-sm">监控并管理所有账号下的闲鱼商品。</p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
            <div className="flex min-w-[200px] flex-col gap-1.5">
              <label htmlFor="item-account-filter" className="px-1 text-[11px] font-extrabold tracking-wide text-gray-500">
                商品列表筛选
              </label>
              <div className="relative">
                <Filter className="w-4 h-4 absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none" />
                <select
                  id="item-account-filter"
                  aria-label="按账号筛选商品列表"
                  className="ios-input w-full pl-10 pr-9 py-3 rounded-xl text-sm"
                  value={accountFilter}
                  onChange={event => setAccountFilter(event.target.value)}
                >
                  <option value="">全部账号</option>
                  {accounts.map(account => (
                    <option key={account.id} value={account.id}>{accountName(account.id)}</option>
                  ))}
                </select>
              </div>
            </div>
            <div className="flex min-w-[200px] flex-col gap-1.5">
              <label htmlFor="item-sync-account" className="px-1 text-[11px] font-extrabold tracking-wide text-gray-500">
                同步商品账号
              </label>
              <div className="relative">
                <User className="w-4 h-4 absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none" />
                <select
                  id="item-sync-account"
                  aria-label="选择要同步商品的账号"
                  className="ios-input w-full pl-10 pr-9 py-3 rounded-xl text-sm"
                  value={selectedAccount}
                  onChange={e => setSelectedAccount(e.target.value)}
                >
                  <option value="">请选择账号</option>
                  {accounts.map(acc => (
                      <option key={acc.id} value={acc.id}>{accountName(acc.id)}</option>
                  ))}
                </select>
              </div>
            </div>
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
              className="px-5 py-3 rounded-2xl font-bold bg-emerald-500 text-white hover:bg-emerald-600 transition-colors flex items-center gap-2 shadow-lg shadow-emerald-100"
            >
              <PackagePlus className="w-4 h-4" />
              发布商品
            </button>
            <button
              onClick={() => void openBatchModal()}
              className="px-5 py-3 rounded-2xl font-bold bg-brand text-white hover:bg-brand-highlight transition-colors flex items-center gap-2 shadow-lg shadow-blue-100"
            >
              <UploadCloud className="w-4 h-4" />
              {recentBatch && ['running', 'canceling'].includes(recentBatch.status) ? '继续批量任务' : '批量铺货'}
            </button>
            {recentBatch && !['running', 'canceling'].includes(recentBatch.status) && (
              <button
                onClick={() => void openRecentBatchResult()}
                className="px-4 py-3 rounded-2xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
              >
                最近批次结果
              </button>
            )}
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4">
          {visibleItems.map(item => {
            const linkedRules = rulesForItem(item);
            const hasRule = linkedRules.length > 0;
            return (
              <div key={`${item.cookie_id}-${item.item_id}`} className="ios-card p-3 rounded-2xl hover:shadow-lg transition-all group relative flex flex-col">
                  <div className="absolute top-2 right-2 flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity z-10">
                      <button
                        onClick={() => handleEdit(item)}
                        className="p-1.5 bg-white/90 backdrop-blur rounded-lg shadow-md text-gray-600 hover:bg-brand hover:text-white transition-colors"
                        title="编辑"
                      >
                        <Edit className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => handleDelete(item)}
                        className="p-1.5 bg-white/90 backdrop-blur rounded-lg shadow-md hover:bg-red-100 text-red-500 transition-colors"
                        title="删除"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                  </div>
                  <div className="aspect-square bg-gray-100 rounded-xl mb-2.5 overflow-hidden relative">
                      {item.item_image ? (
                          <img src={item.item_image} alt="" className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" />
                      ) : (
                          <div className="w-full h-full flex items-center justify-center text-gray-300">
                              <Box className="w-8 h-8" />
                          </div>
                      )}
                      <div className="absolute top-1.5 left-1.5 bg-black/50 backdrop-blur-md text-white text-[10px] font-bold px-1.5 py-0.5 rounded-md">
                          {formatItemPrice(item.item_price)}
                      </div>
                  </div>
                  <h3 className="font-bold text-gray-900 line-clamp-2 text-xs mb-1.5 h-8 leading-4">{item.item_title}</h3>
                  <div className="mb-2 inline-flex min-w-0 max-w-full items-center gap-1 self-start rounded-md bg-blue-50 px-2 py-1 text-[10px] font-extrabold text-blue-700" title={accountNickname(item.cookie_id)}>
                    <User className="h-3 w-3 shrink-0" />
                    <span className="min-w-0 truncate whitespace-nowrap">{accountNickname(item.cookie_id)}</span>
                  </div>
                  <div className="flex justify-between items-center text-[10px] text-gray-500 mb-2">
                      <span className="bg-gray-100 px-1.5 py-0.5 rounded truncate max-w-[80px]">ID: {item.item_id}</span>
                      <span className={`inline-flex items-center gap-1 font-bold ${hasRule ? 'text-emerald-600' : 'text-amber-600'}`}>
                        {hasRule ? <CheckCircle2 className="w-3 h-3" /> : <CircleDashed className="w-3 h-3" />}
                        {hasRule ? `${linkedRules.length} 规则` : '未配置'}
                      </span>
                  </div>
                  <div className="space-y-2 mt-auto">
                      <button
                        onClick={() => onConfigureDelivery(item)}
                        className={`w-full flex items-center justify-between gap-1 px-2.5 py-2 rounded-lg text-[11px] font-extrabold transition-all ${hasRule ? 'bg-gray-900 text-white hover:bg-black' : 'bg-brand text-white hover:bg-brand-highlight shadow-md shadow-blue-100'}`}
                      >
                        <span className="flex items-center gap-1.5"><Link2 className="w-3.5 h-3.5" />{hasRule ? '查看发货规则' : '关联发货规则'}</span>
                        <ArrowRight className="w-3.5 h-3.5" />
                      </button>
                  </div>
              </div>
            );
          })}
          {visibleItems.length === 0 && (
             <div className="col-span-full py-20 text-center text-gray-400">
                 <ShoppingBag className="w-12 h-12 mx-auto mb-4 opacity-30" />
                 {accountFilter ? '该账号暂无商品数据' : '暂无商品数据，请选择账号进行同步'}
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
                  {accounts.map(acc => <option key={acc.id} value={acc.id}>{accountName(acc.id)}</option>)}
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
                  {accounts.map(acc => <option key={acc.id} value={acc.id}>{accountName(acc.id)}</option>)}
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
                {publishImagePreviews.length > 0 && (
                  <div className="grid grid-cols-4 sm:grid-cols-6 gap-3">
                    {publishImagePreviews.map((preview) => (
                      <div key={preview.key} className="aspect-square rounded-xl bg-gray-100 overflow-hidden border border-gray-100">
                        <img src={preview.url} alt="" className="w-full h-full object-cover" />
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
            <div className="modal-footer">
              <button disabled={publishing} onClick={handlePublishItem} className="w-full bg-emerald-500 hover:bg-emerald-600 disabled:opacity-60 text-white px-6 py-3.5 rounded-xl font-bold flex items-center justify-center gap-2">
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
              <button onClick={() => void closeBatchModal()} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭">
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
                      {accounts.map(acc => <option key={acc.id} value={acc.id}>{accountName(acc.id)}</option>)}
                    </select>
                    <p className="text-xs text-gray-500">表格中“账号ID”为空时，会使用这里选择的账号。</p>
                  </div>

                  <div className="rounded-xl border border-amber-200 bg-amber-50/70 p-4 space-y-3">
                    <div>
                      <div className="text-sm font-extrabold text-gray-900">默认类目 <span className="font-medium text-gray-500">（可为空）</span></div>
                      <p className="mt-1 text-xs leading-5 text-amber-800">填写后优先使用该类目；留空时由闲鱼根据每件商品自动识别。仍无法识别时，系统最终使用“电子资料”兜底。</p>
                    </div>
                    <div className="flex flex-col gap-2 sm:flex-row">
                      <label className="relative flex-1">
                        <span className="sr-only">类目关键词</span>
                        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
                        <input
                          className="w-full ios-input rounded-xl bg-white py-2.5 pl-10 pr-3"
                          placeholder="输入关键词，例如：课程资料、设计素材"
                          value={batchCategoryKeyword}
                          onChange={e => setBatchCategoryKeyword(e.target.value)}
                          onKeyDown={e => {
                            if (e.key === 'Enter') {
                              e.preventDefault();
                              void handleRecommendBatchCategory();
                            }
                          }}
                        />
                      </label>
                      <button
                        type="button"
                        disabled={!selectedAccount || !batchCategoryKeyword.trim() || batchCategoryLoading}
                        onClick={() => void handleRecommendBatchCategory()}
                        className="ios-btn-primary flex min-h-[42px] items-center justify-center gap-2 rounded-xl px-4 text-sm font-bold disabled:opacity-50"
                      >
                        <Search className="h-4 w-4" />
                        {batchCategoryLoading ? '匹配中...' : '获取类目'}
                      </button>
                    </div>
                    {batchFallbackCategory.catId ? (
                      <div className="flex min-h-[46px] items-center justify-between gap-3 border-t border-amber-200 pt-3">
                        <div className="min-w-0">
                          <div className="flex items-center gap-2 text-sm font-bold text-gray-900">
                            <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600" />
                            <span className="truncate">{batchFallbackCategory.catName}</span>
                          </div>
                          <div className="mt-1 font-mono text-xs text-gray-500">类目 {batchFallbackCategory.catId} · 频道 {batchFallbackCategory.channelCatId}</div>
                        </div>
                        <button
                          type="button"
                          title="清除默认类目"
                          onClick={() => setBatchFallbackCategory({ catId: '', catName: '', channelCatId: '', tbCatId: '' })}
                          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-white hover:text-gray-900"
                        >
                          <X className="h-4 w-4" />
                        </button>
                      </div>
                    ) : null}
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
                            ['账号ID', '需要覆盖默认账号时填写', '上方默认发布账号为必选；本列留空时使用默认账号，填写后仅覆盖当前行'],
                            ['标题', '每个商品都要填', '填写买家能看到的商品标题'],
                            ['描述', '可以留空', '留空时会使用商品标题作为描述'],
                            ['价格', '每个商品都要填', '只填数字，例如 19.90'],
                            ['原价', '可以留空', '需要展示划线原价时填写，例如 29.90'],
                            ['库存', '可以留空', '留空按 1 件处理；填写时必须大于 0'],
                            ['邮费模式', '可以留空', '留空表示包邮；包邮填 free，固定邮费填 fixed'],
                            ['邮费', '邮费模式填 fixed 时填写', '只填数字，例如 8.00'],
                            ['图片', '每个商品都要填', '填写 zip 内图片路径或图片网址；多张图片用英文分号隔开'],
                            ['类目ID', '需要指定当前行默认类目时填写', '必须和“类目名称、频道类目ID”同时填写；优先于自动识别'],
                            ['类目名称', '填写了“类目ID”时必填', '填写该 ID 对应的准确类目名称'],
                            ['频道类目ID', '覆盖类目时必填', '必须填写闲鱼返回的准确频道类目 ID'],
                            ['淘宝类目ID', '按闲鱼返回填写', '“电子资料”无淘宝类目 ID，保持为空'],
                            ['付款发货启用', '需要付款后自动发货时填写', '填“是”表示开启；不需要时填“否”或留空'],
                            ['付款发货内容', '“付款发货启用”填“是”时填写', '从“卡密库存”页面取得卡密组 ID，按上方示例填写'],
                            ['评价赠品启用', '需要评价赠品时填写', '填“是”表示开启；不需要时填“否”或留空'],
                            ['评价赠品内容', '“评价赠品启用”填“是”时填写', '格式和付款发货内容相同，也可以同时发送多个卡密组'],
                            ['求评价启用', '需要自动求评价时填写', '填“是”表示开启；不需要时填“否”或留空'],
                            ['求评价等待小时', '“求评价启用”填“是”时填写', '填写等待小时数；留空按 72 小时处理'],
                            ['求评价文案', '“求评价启用”填“是”时填写', '填写要发送给买家的求评价消息'],
                            ['求评价最多次数', '可以留空', '留空只提醒 1 次'],
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
                          <th className="px-4 py-3">类目策略</th>
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
                            <td className="px-4 py-3 text-xs text-gray-600 min-w-[150px]">
                              <div className="font-bold text-gray-800">{row.category?.cat_name || '自动识别'}</div>
                              <div className="font-mono text-gray-400">{row.category?.cat_id || '失败后使用电子资料'}</div>
                            </td>
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
                          <th className="px-4 py-3">类目策略</th>
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
                            <td className="px-4 py-3 text-xs text-gray-600 min-w-[150px]">
                              <div className="font-bold text-gray-800">{row.category?.cat_name || '自动识别'}</div>
                              <div className="font-mono text-gray-400">{row.category?.cat_id || '失败后使用电子资料'}</div>
                            </td>
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
                  <button disabled={batchLoading} onClick={() => void abandonBatchPreview()} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-800 font-bold">
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
                  ) : batchDetail.status === 'canceling' ? (
                    <button disabled className="flex-1 px-6 py-3.5 rounded-xl bg-amber-100 text-amber-800 font-bold">
                      正在保存远端结果并安全取消…
                    </button>
                  ) : (
                    <button onClick={() => window.open(`/items/publish-batches/${batchDetail.id}/result.csv`, '_blank')} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-900 text-white hover:bg-black font-bold">
                      下载结果
                    </button>
                  )}
                  {batchDetail.retryable > 0 && !['running', 'canceling'].includes(batchDetail.status) && (
                    <button disabled={batchLoading} onClick={handleRetryBatchFailed} className="flex-1 ios-btn-primary px-6 py-3.5 rounded-xl font-bold flex items-center justify-center gap-2">
                      <RefreshCw className={`w-4 h-4 ${batchLoading ? 'animate-spin' : ''}`} />
                      重试失败项
                    </button>
                  )}
                  {!['running', 'canceling'].includes(batchDetail.status) && (
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
