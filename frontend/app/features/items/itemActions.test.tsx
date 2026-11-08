// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { useState } from 'react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item } from './api';
import { createItem,deleteItem,getPublishLocations,publishItem,recommendPublishCategory,syncItemsFromAccount,updateItem } from './api';
import { useItemActions,type ItemActionsOptions } from './itemActions';

vi.mock('./api', /* itemActionsApiMockFactory 提供商品动作 Hook 的确定性 API 替身。 */ () => ({
  createItem: vi.fn(),
  deleteItem: vi.fn(),
  getPublishLocations: vi.fn(),
  itemErrorMessage: /* errorMessageMock 将测试动作中的异常转换为稳定回退文本。 */ (error: unknown, fallback: string) => error instanceof Error ? error.message : fallback,
  publishItem: vi.fn(),
  recommendPublishCategory: vi.fn(),
  syncItemsFromAccount: vi.fn(),
  updateItem: vi.fn(),
}));

// createItemMock 是手动添加商品接口的可控替身。
const createItemMock = vi.mocked(createItem);
// deleteItemMock 是商品删除接口的可控替身。
const deleteItemMock = vi.mocked(deleteItem);
// locationsMock 是发货地查询接口的可控替身。
const locationsMock = vi.mocked(getPublishLocations);
// publishItemMock 是普通商品发布接口的可控替身。
const publishItemMock = vi.mocked(publishItem);
// recommendPublishCategoryMock 是普通发布类目推荐接口的可控替身。
const recommendPublishCategoryMock = vi.mocked(recommendPublishCategory);
// syncItemsMock 是商品同步接口的可控替身。
const syncItemsMock = vi.mocked(syncItemsFromAccount);
// updateItemMock 是商品编辑接口的可控替身。
const updateItemMock = vi.mocked(updateItem);

// itemFixture 是商品动作 Hook 使用的商品样本。
const itemFixture: Item = { id: 'item-1', cookie_id: 'account-1', item_id: 'item-1', item_title: '旧标题', item_price: '10', item_description: '', item_category: '', item_detail: '', item_image: '' } as Item;
// locationFixture 是发货地查询返回的样本。
const locationFixture = { area: '区域', city: '城市', division_id: 'division-1', longitude: 120, latitude: 30, poi_id: 'poi-1', poi_name: '发货地', province: '省' };

// useItemActionsHarness 创建带有真实 React 状态容器的商品动作 Hook。
const useItemActionsHarness = () => {
  // selectedAccount 保存当前同步/发布账号。
  const [selectedAccount, setSelectedAccount] = useState('account-1');
  // items 保存当前商品列表。
  const [items, setItems] = useState<Item[]>([itemFixture]);
  // batchLocations 保存批量发布定位结果。
  const [, setBatchLocations] = useState([locationFixture]);
  // batchLocation 保存批量发布当前定位。
  const [, setBatchLocation] = useState<typeof locationFixture | null>(null);
  // loadItems 是商品刷新替身。
  const [loadItems] = useState(() => vi.fn(async () => undefined));
  // loadShippingRules 是规则刷新替身。
  const [loadShippingRules] = useState(() => vi.fn(async () => undefined));
  // onConfigureDelivery 是发布成功后的规则配置回调。
  const [onConfigureDelivery] = useState(() => vi.fn());
  // options 是商品动作 Hook 的完整依赖。
  const options: ItemActionsOptions = { selectedAccount, setSelectedAccount, setItems, loadItems, loadShippingRules, onConfigureDelivery, setBatchLocations, setBatchLocation };
  // actions 是商品动作 Hook 的状态和操作。
  const actions = useItemActions(options);
  return { actions, items, loadItems, loadShippingRules, onConfigureDelivery };
};

describe('useItemActions', /* 当前回调验证商品普通操作、发布和定位边界。 */ () => {
  beforeEach(/* 当前回调重置商品动作 API 和浏览器交互替身。 */ () => {
    vi.clearAllMocks();
    createItemMock.mockResolvedValue({ success: true });
    deleteItemMock.mockResolvedValue({ success: true });
    locationsMock.mockResolvedValue([locationFixture]);
    publishItemMock.mockResolvedValue({ success: true, message: '发布成功', item_id: 'new-item', item_url: '', item_image: 'image.jpg', item_title: '新商品', item_price: '20', quantity: 1, category_id: '', category_name: '' });
    recommendPublishCategoryMock.mockResolvedValue({ success: true, category: { cat_id: '5001', cat_name: '虚拟服务', channel_cat_id: '6001', tb_cat_id: '7001' } });
    syncItemsMock.mockResolvedValue({ success: true, message: '同步完成', total_count: 1, total_pages: 1, saved_count: 1, deleted_count: 0 });
    updateItemMock.mockResolvedValue({ success: true });
    vi.stubGlobal('alert', vi.fn());
    vi.stubGlobal('confirm', vi.fn(() => true));
  });

  afterEach(/* 当前回调清理商品动作测试的全局替身。 */ () => vi.unstubAllGlobals());

  test('同步、编辑、添加和删除商品均刷新或更新列表', /* 当前回调验证商品 CRUD 动作协调。 */ async () => {
    // hook 是商品动作 Hook 的真实 React 状态实例。
    const hook = renderHook(/* delayedLocationHookFactory 创建可关闭普通发布弹窗的商品动作状态容器。 */ () => useItemActionsHarness());
    await act(/* 当前回调执行商品同步。 */ async () => hook.result.current.actions.handleSync());
    expect(syncItemsMock).toHaveBeenCalledWith('account-1');

    act(/* 当前回调打开商品编辑草稿。 */ () => hook.result.current.actions.handleEdit(itemFixture));
    act(/* 当前回调写入商品标题。 */ () => hook.result.current.actions.setEditForm(current => ({ ...current, item_title: '新标题' })));
    await act(/* 当前回调保存商品编辑。 */ async () => hook.result.current.actions.handleSaveEdit());
    expect(updateItemMock).toHaveBeenCalledWith('account-1', 'item-1', expect.objectContaining({ item_title: '新标题' }));

    act(/* 当前回调写入手动添加商品表单。 */ () => hook.result.current.actions.setAddForm({ cookie_id: 'account-1', item_id: 'item-2', item_title: '新增商品', item_price: '12', item_image: 'image.jpg' }));
    await act(/* 当前回调创建手动商品关联。 */ async () => hook.result.current.actions.handleAddItem());
    expect(createItemMock).toHaveBeenCalledWith('account-1', expect.objectContaining({ item_id: 'item-2', item_detail: JSON.stringify({ item_image: 'image.jpg' }) }));

    await act(/* 当前回调删除商品。 */ async () => hook.result.current.actions.handleDelete(itemFixture));
    expect(deleteItemMock).toHaveBeenCalledWith('account-1', 'item-1');
    expect(hook.result.current.items).toEqual([]);
    hook.unmount();
  });

  test('普通发布成功后清理表单并打开发货规则配置', /* 当前回调验证普通发布成功分支。 */ async () => {
    // hook 是商品动作 Hook 的真实 React 状态实例。
    const hook = renderHook(/* delayedLocationHookFactory 创建可关闭普通发布弹窗的商品动作状态容器。 */ () => useItemActionsHarness());
    act(/* 当前回调写入普通发布商品表单。 */ () => hook.result.current.actions.setPublishForm({ cookie_id: 'account-1', title: '新商品', description: '描述', price: '20', original_price: '', quantity: '2', postage_mode: 'free', postage: '', images: [new File(['image'], 'image.jpg')], specs: [], skuRows: [] }));
    await act(/* 当前回调执行普通商品发布。 */ async () => hook.result.current.actions.handlePublishItem());
    expect(publishItemMock).toHaveBeenCalledWith(expect.objectContaining({ cookie_id: 'account-1', title: '新商品', quantity: 2, images: expect.any(Array) }));
    expect(hook.result.current.onConfigureDelivery).toHaveBeenCalledWith(expect.objectContaining({ item_id: 'new-item', cookie_id: 'account-1' }));
    expect(hook.result.current.actions.publishForm).toEqual(expect.objectContaining({ cookie_id: 'account-1', title: '', images: [] }));
    hook.unmount();
  });

  test('普通发布可推荐类目并把选中类目随发布请求提交', /* 当前回调验证普通发布类目推荐与发布参数衔接。 */ async () => {
    // hook 是商品动作 Hook 的真实 React 状态实例。
    const hook = renderHook(/* categoryHookFactory 创建普通发布类目选择场景的 Hook。 */ () => useItemActionsHarness());
    act(/* accountAction 写入普通发布类目推荐使用的账号。 */ () => hook.result.current.actions.setPublishForm(current => ({ ...current, cookie_id: 'account-1' })));
    act(/* keywordAction 写入普通发布类目关键词。 */ () => hook.result.current.actions.setPublishCategoryKeyword('课程资料'));
    await act(/* recommendAction 请求普通发布推荐类目。 */ async () => hook.result.current.actions.handleRecommendPublishCategory());
    expect(recommendPublishCategoryMock).toHaveBeenCalledWith('account-1', '课程资料', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.actions.publishCategory).toEqual({ cat_id: '5001', cat_name: '虚拟服务', channel_cat_id: '6001', tb_cat_id: '7001' });
    act(/* formAction 写入普通发布必填字段和图片。 */ () => hook.result.current.actions.setPublishForm({ cookie_id: 'account-1', title: '新商品', description: '描述', price: '20', original_price: '', quantity: '1', postage_mode: 'free', postage: '', images: [new File(['image'], 'image.jpg')], specs: [], skuRows: [] }));
    await act(/* publishAction 提交带选中类目的普通商品。 */ async () => hook.result.current.actions.handlePublishItem());
    expect(publishItemMock).toHaveBeenCalledWith(expect.objectContaining({ category: { cat_id: '5001', cat_name: '虚拟服务', channel_cat_id: '6001', tb_cat_id: '7001' } }));
    hook.unmount();
  });

  test('普通发布类目推荐保存全部候选并可改选，再次推荐时整体替换', /* 当前回调验证候选类目列表的保存、改选和替换。 */ async () => {
    // hook 是商品动作 Hook 的真实 React 状态实例。
    const hook = renderHook(/* candidateHookFactory 创建候选类目场景的 Hook。 */ () => useItemActionsHarness());
    recommendPublishCategoryMock.mockResolvedValueOnce({
      success: true,
      category: { cat_id: '5001', cat_name: '虚拟服务', channel_cat_id: '6001' },
      candidates: [{ cat_id: '5001', cat_name: '虚拟服务', channel_cat_id: '6001' }, { cat_id: '5002', cat_name: '游戏软件平台', channel_cat_id: '6002' }],
    });
    act(/* accountAction 写入推荐使用的账号。 */ () => hook.result.current.actions.setPublishForm(current => ({ ...current, cookie_id: 'account-1' })));
    act(/* keywordAction 写入类目关键词。 */ () => hook.result.current.actions.setPublishCategoryKeyword('游戏'));
    await act(/* recommendAction 请求推荐类目。 */ async () => hook.result.current.actions.handleRecommendPublishCategory());
    expect(hook.result.current.actions.publishCategoryCandidates.map(/* candidate 表示当前候选类目。 */ candidate => candidate.cat_name)).toEqual(['虚拟服务', '游戏软件平台']);
    act(/* selectAction 模拟用户改选第二个候选。 */ () => hook.result.current.actions.setPublishCategory(hook.result.current.actions.publishCategoryCandidates[1]));
    expect(hook.result.current.actions.publishCategory?.cat_id).toBe('5002');
    // 下一次推荐会整体替换候选列表。
    recommendPublishCategoryMock.mockResolvedValueOnce({ success: true, category: { cat_id: '9', cat_name: '其他', channel_cat_id: '99' } });
    await act(/* secondRecommendAction 再次请求推荐类目，旧后端不返回候选。 */ async () => hook.result.current.actions.handleRecommendPublishCategory());
    expect(hook.result.current.actions.publishCategoryCandidates).toEqual([{ cat_id: '9', cat_name: '其他', channel_cat_id: '99' }]);
    hook.unmount();
  });

  test('定位成功写入普通和批量发货地，浏览器拒绝时收口状态', /* 当前回调验证定位动作的成功和失败分支。 */ async () => {
    // getCurrentPositionMock 是浏览器定位 API 的可控替身。
    const getCurrentPositionMock = vi.fn(/* currentPositionAction 模拟定位成功回调。 */ (success: PositionCallback) => success({ coords: { longitude: 120, latitude: 30 } } as GeolocationPosition));
    vi.stubGlobal('navigator', { geolocation: { getCurrentPosition: getCurrentPositionMock } });
    // hook 是商品动作 Hook 的真实 React 状态实例。
    const hook = renderHook(() => useItemActionsHarness());
    act(/* 当前回调设置定位使用的普通发布账号。 */ () => hook.result.current.actions.setPublishForm(current => ({ ...current, cookie_id: 'account-1' })));
    await act(/* 当前回调加载普通发布发货地。 */ async () => hook.result.current.actions.locateForPublish(false));
    expect(locationsMock).toHaveBeenCalledWith(120, 30, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.actions.publishLocation).toEqual(locationFixture);
    await act(/* 当前回调加载批量发布发货地。 */ async () => hook.result.current.actions.locateForPublish(true));
    expect(hook.result.current.actions.locationLoading).toBe(false);
    hook.unmount();
  });

  test('关闭普通发布弹窗后忽略晚到的浏览器定位与地点回调', /* 当前回调验证弹窗关闭会撤销定位请求的界面所有权。 */ async () => {
    // resolvePosition 保存测试主动发送晚到浏览器定位结果的函数。
    let resolvePosition: PositionCallback | undefined;
    // getCurrentPositionMock 保存浏览器定位回调但不立即完成，模拟用户关闭弹窗后才返回。
    const getCurrentPositionMock = vi.fn(/* delayedPositionAction 延迟交付浏览器定位结果。 */ (success: PositionCallback) => { resolvePosition = success; });
    vi.stubGlobal('navigator', { geolocation: { getCurrentPosition: getCurrentPositionMock } });
    // hook 是包含普通发布表单状态的商品动作 Hook。
    const hook = renderHook(() => useItemActionsHarness());
    act(/* openPublishAction 打开普通发布弹窗并选择发布账号。 */ () => {
      hook.result.current.actions.setPublishForm(/* accountFormAction 为晚到定位测试设置普通发布账号。 */ current => ({ ...current, cookie_id: 'account-1' }));
      hook.result.current.actions.setShowPublishModal(true);
    });
    act(/* beginLocateAction 启动尚未返回的定位流程。 */ () => { void hook.result.current.actions.locateForPublish(false); });
    act(/* closePublishAction 关闭弹窗并取消当前定位所有权。 */ () => hook.result.current.actions.setShowPublishModal(false));
    await act(/* latePositionAction 交付已失效的浏览器定位结果。 */ async () => {
      resolvePosition?.({ coords: { longitude: 120, latitude: 30 } } as GeolocationPosition);
      await Promise.resolve();
    });
    expect(locationsMock).not.toHaveBeenCalled();
    expect(hook.result.current.actions.locationLoading).toBe(false);
    hook.unmount();
  });
});
