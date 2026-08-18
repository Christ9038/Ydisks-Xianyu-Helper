import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import type { Item } from '../../../shared/api-contract';
import { createItem, deleteItem, getPublishLocations, publishItem, syncItemsFromAccount, updateItem } from './api';
import type { PublishLocation } from './api';

// AddItemForm 描述手动添加商品弹窗的表单字段。
export interface AddItemForm {
  // cookie_id 是商品所属账号标识。
  cookie_id: string;
  // item_id 是平台商品标识。
  item_id: string;
  // item_title 是商品标题。
  item_title: string;
  // item_price 是商品价格。
  item_price: string;
  // item_image 是商品图片地址。
  item_image: string;
}

// PublishItemForm 描述普通商品发布弹窗的表单字段。
export interface PublishItemForm {
  // cookie_id 是发布商品使用的账号标识。
  cookie_id: string;
  // title 是发布商品标题。
  title: string;
  // description 是发布商品描述。
  description: string;
  // price 是发布商品售价。
  price: string;
  // original_price 是发布商品原价。
  original_price: string;
  // quantity 是发布商品库存数量文本。
  quantity: string;
  // postage_mode 是发布商品运费模式。
  postage_mode: string;
  // postage 是固定运费金额文本。
  postage: string;
  // images 是发布商品上传的图片文件。
  images: File[];
}

// ItemActionsOptions 描述商品动作协调器依赖的列表状态和批量定位回调。
export interface ItemActionsOptions {
  // selectedAccount 是当前同步/发布使用的账号。
  selectedAccount: string;
  // setSelectedAccount 更新当前同步/发布使用的账号。
  setSelectedAccount: Dispatch<SetStateAction<string>>;
  // setItems 更新商品列表。
  setItems: Dispatch<SetStateAction<Item[]>>;
  // loadItems 刷新商品列表。
  loadItems: () => Promise<void>;
  // loadShippingRules 刷新商品关联的自动化规则。
  loadShippingRules: () => Promise<void>;
  // onConfigureDelivery 打开发布成功商品的发货规则配置。
  onConfigureDelivery: (item: Item) => void;
  // setBatchLocations 写入批量发布可选发货地。
  setBatchLocations: Dispatch<SetStateAction<PublishLocation[]>>;
  // setBatchLocation 写入批量发布当前发货地。
  setBatchLocation: Dispatch<SetStateAction<PublishLocation | null>>;
}

// ItemActionsState 暴露商品普通操作、发布表单和定位状态。
export interface ItemActionsState {
  // selectedAccount 是当前同步/发布使用的账号。
  selectedAccount: string;
  // setSelectedAccount 更新当前同步/发布使用的账号。
  setSelectedAccount: Dispatch<SetStateAction<string>>;
  // loading 表示商品同步请求是否正在执行。
  loading: boolean;
  // publishing 表示普通商品发布请求是否正在执行。
  publishing: boolean;
  // showEditModal 表示编辑商品弹窗是否打开。
  showEditModal: boolean;
  // setShowEditModal 更新编辑商品弹窗展示状态。
  setShowEditModal: Dispatch<SetStateAction<boolean>>;
  // showAddModal 表示添加商品弹窗是否打开。
  showAddModal: boolean;
  // setShowAddModal 更新添加商品弹窗展示状态。
  setShowAddModal: Dispatch<SetStateAction<boolean>>;
  // showPublishModal 表示普通发布弹窗是否打开。
  showPublishModal: boolean;
  // setShowPublishModal 更新普通发布弹窗展示状态。
  setShowPublishModal: Dispatch<SetStateAction<boolean>>;
  // locationLoading 表示发货地定位请求是否正在执行。
  locationLoading: boolean;
  // publishLocations 保存普通发布可选发货地。
  publishLocations: PublishLocation[];
  // setPublishLocations 更新普通发布可选发货地。
  setPublishLocations: Dispatch<SetStateAction<PublishLocation[]>>;
  // publishLocation 保存普通发布当前发货地。
  publishLocation: PublishLocation | null;
  // setPublishLocation 更新普通发布当前发货地。
  setPublishLocation: Dispatch<SetStateAction<PublishLocation | null>>;
  // selectedItem 保存当前编辑或删除的商品。
  selectedItem: Item | null;
  // editForm 保存当前商品编辑草稿。
  editForm: Partial<Item>;
  // setEditForm 更新商品编辑草稿。
  setEditForm: Dispatch<SetStateAction<Partial<Item>>>;
  // addForm 保存手动添加商品草稿。
  addForm: AddItemForm;
  // setAddForm 更新手动添加商品草稿。
  setAddForm: Dispatch<SetStateAction<AddItemForm>>;
  // publishForm 保存普通商品发布草稿。
  publishForm: PublishItemForm;
  // setPublishForm 更新普通商品发布草稿。
  setPublishForm: Dispatch<SetStateAction<PublishItemForm>>;
  // publishImagePreviews 保存发布图片预览地址。
  publishImagePreviews: { /** key 是图片文件和索引组合键。 */ key: string; /** url 是图片临时预览地址。 */ url: string }[];
  // handleSync 同步当前账号的商品列表。
  handleSync: () => Promise<void>;
  // handleEdit 打开指定商品编辑弹窗。
  handleEdit: (item: Item) => void;
  // handleSaveEdit 保存当前商品编辑草稿。
  handleSaveEdit: () => Promise<void>;
  // handleDelete 删除指定商品并更新本地列表。
  handleDelete: (item: Item) => Promise<void>;
  // handleAddItem 创建手动添加的商品关联。
  handleAddItem: () => Promise<void>;
  // handlePublishItem 发布普通商品并在成功后打开规则配置。
  handlePublishItem: () => Promise<void>;
  // downloadPublishTemplate 下载普通发布 CSV 模板。
  downloadPublishTemplate: () => void;
  // openAddModal 打开添加商品弹窗并回填当前账号。
  openAddModal: () => void;
  // openPublishModal 打开普通发布弹窗并回填当前账号。
  openPublishModal: () => void;
  // locateForPublish 获取普通或批量发布使用的发货地。
  locateForPublish: (batch: boolean) => Promise<void>;
}

// emptyAddItemForm 创建手动添加商品表单的初始值。
export const emptyAddItemForm = (): AddItemForm => ({ cookie_id: '', item_id: '', item_title: '', item_price: '', item_image: '' });

// emptyPublishItemForm 创建普通发布商品表单的初始值。
export const emptyPublishItemForm = (cookieID = ''): PublishItemForm => ({ cookie_id: cookieID, title: '', description: '', price: '', original_price: '', quantity: '1', postage_mode: 'free', postage: '', images: [] });

// itemErrorMessage 将未知异常转换为稳定的商品操作提示。
const itemErrorMessage = (error: unknown, fallback: string): string => {
  if (error instanceof Error) return error.message;
  // message 保存兼容 API 错误对象中的文本说明。
  const message = typeof error === 'object' && error !== null && 'message' in error ? (error as { /** message 是兼容错误对象中的文本说明。 */ message?: unknown }).message : undefined;
  return typeof message === 'string' ? message : fallback;
};

// useItemActions 集中管理商品同步、编辑、删除、添加、普通发布和定位动作。
export const useItemActions = ({ selectedAccount, setSelectedAccount, setItems, loadItems, loadShippingRules, onConfigureDelivery, setBatchLocations, setBatchLocation }: ItemActionsOptions): ItemActionsState => {
  // loading 表示商品同步请求是否正在执行。
  const [loading, setLoading] = useState(false);
  // publishing 表示普通商品发布请求是否正在执行。
  const [publishing, setPublishing] = useState(false);
  // showEditModal 表示编辑商品弹窗是否打开。
  const [showEditModal, setShowEditModal] = useState(false);
  // showAddModal 表示添加商品弹窗是否打开。
  const [showAddModal, setShowAddModal] = useState(false);
  // showPublishModal 表示普通发布弹窗是否打开。
  const [showPublishModal, setShowPublishModal] = useState(false);
  // locationLoading 表示发货地定位请求是否正在执行。
  const [locationLoading, setLocationLoading] = useState(false);
  // publishLocations 保存普通发布可选发货地。
  const [publishLocations, setPublishLocations] = useState<PublishLocation[]>([]);
  // publishLocation 保存普通发布当前发货地。
  const [publishLocation, setPublishLocation] = useState<PublishLocation | null>(null);
  // selectedItem 保存当前编辑或删除的商品。
  const [selectedItem, setSelectedItem] = useState<Item | null>(null);
  // editForm 保存当前商品编辑草稿。
  const [editForm, setEditForm] = useState<Partial<Item>>({});
  // addForm 保存手动添加商品草稿。
  const [addForm, setAddForm] = useState<AddItemForm>(emptyAddItemForm);
  // publishForm 保存普通商品发布草稿。
  const [publishForm, setPublishForm] = useState<PublishItemForm>(emptyPublishItemForm);
  // publishImagePreviews 保存发布图片预览地址。
  const [publishImagePreviews, setPublishImagePreviews] = useState<{ /** key 是图片文件和索引组合键。 */ key: string; /** url 是图片临时预览地址。 */ url: string }[]>([]);
  // locationController 保存当前定位请求的取消器，新的定位和组件卸载必须终止旧请求。
  const locationController = useRef<AbortController | null>(null);
  // locationGeneration 丢弃已经失去当前界面所有权的定位响应。
  const locationGeneration = useRef(0);

  // useEffect 管理普通发布图片对象地址的创建和释放。
  useEffect(/* 当前回调同步发布图片预览资源生命周期。 */ () => {
    if (!showPublishModal || publishForm.images.length === 0) {
      setPublishImagePreviews([]);
      return;
    }
    // previews 保存当前图片文件对应的临时预览地址。
    const previews = publishForm.images.map(/* 当前回调创建单个图片预览。 */ (file, index) => ({ key: file.name + index, url: URL.createObjectURL(file) }));
    setPublishImagePreviews(previews);
    return /* 当前回调释放当前批次的图片预览地址。 */ () => previews.forEach(/* 当前回调释放单个图片预览。 */ preview => URL.revokeObjectURL(preview.url));
  }, [publishForm.images, showPublishModal]);

  // cancelLocationLookup 结束当前定位状态并使高德、浏览器定位的晚到回调失效。
  const cancelLocationLookup = useCallback(/* cancelLocationAction 中止当前拥有的定位会话并推进代次。 */ () => {
    locationController.current?.abort();
    locationController.current = null;
    locationGeneration.current += 1;
    setLocationLoading(false);
  }, []);

  // 定位请求由商品动作 Hook 拥有，组件卸载时必须释放浏览器和地图查询资源。
  useEffect(/* locationUnmountCleanup 在组件卸载时取消由当前 Hook 拥有的定位请求。 */ () => cancelLocationLookup, [cancelLocationLookup]);

  // 普通发布弹窗关闭时收回其专属定位请求，避免关闭后的浏览器或高德回调写入隐藏表单。
  useEffect(/* publishModalLocationCleanup 监听普通发布弹窗可见性并取消失去界面所有权的定位请求。 */ () => {
    if (!showPublishModal) cancelLocationLookup();
  }, [cancelLocationLookup, showPublishModal]);

  // handleSync 同步当前账号商品并刷新列表。
  const handleSync = useCallback(/* syncAction 执行当前账号商品同步。 */ async () => {
    if (!selectedAccount) return alert('请先选择账号');
    setLoading(true);
    try {
      // result 保存商品同步接口返回的提示。
      const result = await syncItemsFromAccount(selectedAccount);
      await loadItems();
      alert(result?.message || '商品同步完成');
    } catch (/* error 表示商品同步请求异常。 */ error: unknown) {
      console.error('同步商品失败:', error);
      alert(itemErrorMessage(error, '同步失败，请重试'));
    } finally {
      setLoading(false);
    }
  }, [loadItems, selectedAccount]);

  // handleEdit 打开指定商品编辑弹窗并复制草稿。
  const handleEdit = useCallback(/* editAction 打开商品编辑弹窗。 */ (item: Item) => {
    setSelectedItem(item);
    setEditForm({ ...item });
    setShowEditModal(true);
  }, []);

  // handleSaveEdit 保存当前商品字段并刷新关联规则。
  const handleSaveEdit = useCallback(/* saveEditAction 保存商品编辑草稿。 */ async () => {
    if (!selectedItem) return;
    try {
      await updateItem(selectedItem.cookie_id, selectedItem.item_id, {
        item_title: editForm.item_title || '', item_description: editForm.item_description || '', item_category: editForm.item_category || '',
        item_price: editForm.item_price || '', item_detail: editForm.item_detail || selectedItem.item_detail || '',
      });
      await loadItems();
      await loadShippingRules();
      setShowEditModal(false);
      setSelectedItem(null);
    } catch (/* error 表示商品编辑请求异常。 */ error: unknown) {
      console.error('更新商品失败:', error);
      alert('更新失败，请重试');
    }
  }, [editForm, loadItems, loadShippingRules, selectedItem]);

  // handleDelete 删除指定商品并从当前列表移除。
  const handleDelete = useCallback(/* deleteAction 删除商品并更新列表。 */ async (item: Item) => {
    if (!confirm(`确认删除商品"${item.item_title}"吗？`)) return;
    try {
      await deleteItem(item.cookie_id, item.item_id);
      setItems(/* currentItems 更新删除商品后的列表。 */ previous => previous.filter(/* currentItem 保留未删除的商品。 */ currentItem => !(currentItem.cookie_id === item.cookie_id && currentItem.item_id === item.item_id)));
    } catch (/* error 表示商品删除请求异常。 */ error: unknown) {
      console.error('删除商品失败:', error);
      alert('删除失败，请重试');
    }
  }, [setItems]);

  // handleAddItem 校验并创建手动添加商品。
  const handleAddItem = useCallback(/* addAction 创建手动添加商品。 */ async () => {
    try {
      if (!addForm.cookie_id || !addForm.item_id) {
        alert('请选择账号并填写商品ID');
        return;
      }
      await createItem(addForm.cookie_id, { item_id: addForm.item_id, item_title: addForm.item_title, item_price: addForm.item_price, item_detail: addForm.item_image ? JSON.stringify({ item_image: addForm.item_image }) : '' });
      await loadItems();
      setShowAddModal(false);
      setAddForm(emptyAddItemForm());
    } catch (/* error 表示商品创建请求异常。 */ error: unknown) {
      console.error('添加商品失败:', error);
      alert('添加失败，请重试');
    }
  }, [addForm, loadItems]);

  // handlePublishItem 校验并发布普通商品。
  const handlePublishItem = useCallback(/* publishAction 执行普通商品发布。 */ async () => {
    if (!publishForm.cookie_id) return alert('请选择发布账号');
    if (!publishForm.title.trim()) return alert('请填写商品标题');
    if (!publishForm.price.trim()) return alert('请填写商品价格');
    if (!publishForm.quantity || Number(publishForm.quantity) <= 0) return alert('库存数量必须大于 0');
    if (publishForm.images.length === 0) return alert('至少上传 1 张商品图片');
    if (publishForm.postage_mode === 'fixed' && !publishForm.postage.trim()) return alert('请填写一口价邮费');
    setPublishing(true);
    try {
      // result 保存平台发布接口返回的商品信息。
      const result = await publishItem({ ...publishForm, location: publishLocation || undefined });
      await loadItems();
      setShowPublishModal(false);
      setPublishForm(emptyPublishItemForm(selectedAccount || ''));
      setPublishLocations([]);
      setPublishLocation(null);
      if (result?.item_id) {
        // publishedItem 保存发布成功后用于打开规则配置的商品摘要。
        const publishedItem: Item = { id: result.item_id, cookie_id: publishForm.cookie_id, item_id: result.item_id, item_title: result.item_title || publishForm.title, item_price: result.item_price || publishForm.price, item_image: result.item_image };
        onConfigureDelivery(publishedItem);
        alert('商品发布成功，ID: ' + result.item_id + '，已为你打开发货规则配置');
      } else {
        alert('商品发布成功');
      }
    } catch (/* error 表示商品发布请求异常。 */ error: unknown) {
      console.error('发布商品失败:', error);
      // payload 保存服务端返回的结构化错误载荷。
      const payload = (error as { /** payload 是服务端结构化错误载荷。 */ payload?: { /** code 是服务端错误码。 */ code?: unknown } })?.payload;
      if (payload?.code === 'stock_permission_missing') {
        alert('发布失败：该账号没有库存发布权限，无法按库存数量发布商品。请换账号或先在闲鱼确认库存能力。');
        return;
      }
      alert(itemErrorMessage(error, '发布失败，请重试'));
    } finally {
      setPublishing(false);
    }
  }, [loadItems, onConfigureDelivery, publishForm, publishLocation, selectedAccount]);

  // downloadPublishTemplate 生成并下载普通发布 CSV 模板。
  const downloadPublishTemplate = useCallback(/* templateAction 下载普通发布模板。 */ () => {
    // headers 保存模板列名。
    const headers = ['账号ID', '标题', '描述', '价格', '原价', '库存', '邮费模式', '邮费', '图片', '类目ID', '类目名称', '频道类目ID', '淘宝类目ID', '付款发货启用', '付款发货内容', '评价赠品启用', '评价赠品内容', '求评价启用', '求评价等待小时', '求评价文案', '求评价最多次数'];
    // rows 保存模板示例行。
    const rows = [
      ['', '会员组合包自动发货', '下单后发送主卡和附赠卡。', '19.90', '29.90', '10', 'free', '', 'images/bundle-1.jpg;images/bundle-2.jpg', '', '', '', '', '是', '101:1;102:1', '是', '201:1;202:2', '是', '72', '亲，满意的话麻烦给个评价，谢谢～', '1'],
      ['', '普通商品', '仅发布商品，不创建自动化规则。', '49.90', '', '10', 'fixed', '8.00', 'https://example.com/product.jpg', '', '', '', '', '否', '', '否', '', '否', '', '', ''],
    ];
    // csv 保存转义后的模板文本。
    const csv = [headers, ...rows].map(/* 当前回调处理模板行。 */ row => row.map(/* 当前回调处理模板单元格。 */ cell => `"${String(cell).replace(/"/g, '""')}"`).join(',')).join('\n');
    // blob 保存生成的 CSV 文件对象。
    const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' });
    // url 保存模板文件临时地址。
    const url = URL.createObjectURL(blob);
    // link 保存触发下载的临时链接。
    const link = document.createElement('a');
    link.href = url;
    link.download = '批量铺货模板.csv';
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }, []);

  // openAddModal 打开添加商品弹窗并回填当前账号。
  const openAddModal = useCallback(/* openAddAction 打开添加商品弹窗。 */ () => {
    setAddForm(/* currentForm 回填添加商品账号。 */ previous => ({ ...previous, cookie_id: selectedAccount || previous.cookie_id }));
    setShowAddModal(true);
  }, [selectedAccount]);

  // openPublishModal 打开普通发布弹窗并回填当前账号。
  const openPublishModal = useCallback(/* openPublishAction 打开普通发布弹窗。 */ () => {
    setPublishForm(/* currentForm 回填普通发布账号。 */ previous => ({ ...previous, cookie_id: selectedAccount || previous.cookie_id }));
    setShowPublishModal(true);
  }, [selectedAccount]);

  // locateForPublish 使用浏览器定位并加载普通或批量发布发货地。
  const locateForPublish = useCallback(/* locateAction 获取发布发货地。 */ async (batch: boolean) => {
    // cookieId 保存当前定位请求使用的账号。
    const cookieId = batch ? selectedAccount : publishForm.cookie_id;
    if (!cookieId) return alert('请先选择发布账号');
    if (!navigator.geolocation) return alert('当前浏览器不支持定位');
    cancelLocationLookup();
    // controller 是本次定位及地点搜索共同使用的取消器。
    const controller = new AbortController();
    locationController.current = controller;
    // generation 是本次定位请求的所有权版本，晚到响应只能写回同一版本。
    const generation = ++locationGeneration.current;
    setLocationLoading(true);
    try {
      // position 是浏览器返回的当前定位结果；Promise 契约确保调用方会等待它完成。
      const position = await new Promise<GeolocationPosition>(/* positionPromiseExecutor 将回调式浏览器定位转为可等待的 Promise。 */ (resolve, reject) => {
        // successCallback 将浏览器定位成功结果交给当前请求。
        const successCallback: PositionCallback = value => resolve(value);
        // errorCallback 将浏览器定位失败转换为可统一处理的异常。
        const errorCallback: PositionErrorCallback = error => reject(error);
        navigator.geolocation.getCurrentPosition(successCallback, errorCallback, { enableHighAccuracy: true, timeout: 15000, maximumAge: 60000 });
      });
      if (controller.signal.aborted || generation !== locationGeneration.current) return;
      // locations 保存当前位置附近的发货地。
      const locations = await getPublishLocations(position.coords.longitude, position.coords.latitude, { signal: controller.signal });
      if (controller.signal.aborted || generation !== locationGeneration.current) return;
      if (!locations.length) throw new Error('当前位置附近没有可用的高德发货地，请稍后重试');
      if (batch) {
        setBatchLocations(locations);
        setBatchLocation(locations[0]);
      } else {
        setPublishLocations(locations);
        setPublishLocation(locations[0]);
      }
    } catch (/* error 表示发货地查询异常。 */ error: unknown) {
      if (controller.signal.aborted || generation !== locationGeneration.current) return;
      if (typeof error === 'object' && error !== null && 'code' in error) {
        // positionError 是浏览器 Geolocation API 的失败对象，用于区分权限拒绝与其他定位错误。
        const positionError = error as GeolocationPositionError;
        alert(positionError.code === positionError.PERMISSION_DENIED ? '定位权限被拒绝，请在浏览器设置中允许定位' : '无法获取当前位置，请稍后重试');
      } else {
        alert(itemErrorMessage(error, '获取发货地失败'));
      }
    } finally {
      if (generation === locationGeneration.current) {
        locationController.current = null;
        setLocationLoading(false);
      }
    }
  }, [cancelLocationLookup, publishForm.cookie_id, selectedAccount, setBatchLocation, setBatchLocations]);

  return { selectedAccount, setSelectedAccount, loading, publishing, showEditModal, setShowEditModal, showAddModal, setShowAddModal, showPublishModal, setShowPublishModal, locationLoading, publishLocations, setPublishLocations, publishLocation, setPublishLocation, selectedItem, editForm, setEditForm, addForm, setAddForm, publishForm, setPublishForm, publishImagePreviews, handleSync, handleEdit, handleSaveEdit, handleDelete, handleAddItem, handlePublishItem, downloadPublishTemplate, openAddModal, openPublishModal, locateForPublish };
};
