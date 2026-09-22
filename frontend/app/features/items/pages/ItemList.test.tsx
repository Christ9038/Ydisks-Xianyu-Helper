// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item } from '../api';

// itemListMocks 保存商品列表页面使用的 API 和 Hook 替身。
const itemListMocks = vi.hoisted(/* itemListMockFactory 创建列表加载与 AI 配置替身。 */ () => ({
  getAccountDetails: vi.fn(),
  getItemPublishBatches: vi.fn(),
  getItems: vi.fn(),
  getShippingRules: vi.fn(),
  getItemAISettings: vi.fn(),
  updateItemAISettings: vi.fn(),
}));

vi.mock('../api', /* itemListAPIMockFactory 提供页面和真实 AI 弹窗共享的 API 替身。 */ () => ({
  getAccountDetails: itemListMocks.getAccountDetails,
  getItemPublishBatches: itemListMocks.getItemPublishBatches,
  getItems: itemListMocks.getItems,
  getShippingRules: itemListMocks.getShippingRules,
  getItemAISettings: itemListMocks.getItemAISettings,
  updateItemAISettings: itemListMocks.updateItemAISettings,
  itemErrorMessage: /* itemErrorMessageMock 将 Error 转换为用户可见文本。 */ (/* error 是 API 替身抛出的失败原因。 */ error: unknown, fallback: string) => error instanceof Error ? error.message : fallback,
}));

vi.mock('../hooks', /* itemPublishBatchHookMockFactory 提供关闭状态的批量发布流程。 */ () => ({
  useItemPublishBatch: /* itemPublishBatchHookMock 返回页面解构所需的稳定批量状态。 */ () => ({
    showBatchModal: false, batchLoading: false, batchPhase: 'upload', batchFile: null, setBatchFile: vi.fn(), batchImagesZip: null, setBatchImagesZip: vi.fn(),
    batchCategoryKeyword: '', setBatchCategoryKeyword: vi.fn(), batchCategoryLoading: false,
    batchFallbackCategory: { catId: '', catName: '', channelCatId: '', tbCatId: '' }, setBatchFallbackCategory: vi.fn(),
    batchPreview: null, batchDetail: null, recentBatch: null, setRecentBatch: vi.fn(), batchLocations: [], batchLocation: null,
    batchPublishIntervalSeconds: 5, setBatchPublishIntervalSeconds: vi.fn(), setBatchLocations: vi.fn(), setBatchLocation: vi.fn(),
    openBatchModal: vi.fn(), handleRecommendBatchCategory: vi.fn(), openRecentBatchResult: vi.fn(), handlePreviewBatch: vi.fn(), handleStartBatch: vi.fn(),
    handleCancelBatch: vi.fn(), abandonBatchPreview: vi.fn(), closeBatchModal: vi.fn(), handleRetryBatchFailed: vi.fn(),
  }),
}));

vi.mock('../itemActions', /* itemActionsMockFactory 提供关闭状态的商品编辑与发布动作。 */ () => ({
  useItemActions: /* itemActionsHookMock 返回页面解构所需的稳定普通商品状态。 */ () => ({
    loading: false, publishing: false, showEditModal: false, setShowEditModal: vi.fn(), showAddModal: false, setShowAddModal: vi.fn(),
    showPublishModal: false, setShowPublishModal: vi.fn(), locationLoading: false, publishLocations: [], setPublishLocations: vi.fn(),
    publishLocation: null, setPublishLocation: vi.fn(), publishCategoryKeyword: '', setPublishCategoryKeyword: vi.fn(), publishCategoryLoading: false,
    publishCategory: null, setPublishCategory: vi.fn(), selectedItem: null, editForm: {}, setEditForm: vi.fn(), addForm: {}, setAddForm: vi.fn(),
    publishForm: { cookie_id: '', title: '', description: '', price: '', original_price: '', quantity: '1', postage_mode: 'free', postage: '', images: [], specs: [], skuRows: [] },
    setPublishForm: vi.fn(), publishImagePreviews: [], handleSync: vi.fn(), handleEdit: vi.fn(), handleSaveEdit: vi.fn(), handleDelete: vi.fn(),
    handleAddItem: vi.fn(), handlePublishItem: vi.fn(), handleRecommendPublishCategory: vi.fn(), downloadPublishTemplate: vi.fn(), openAddModal: vi.fn(),
    openPublishModal: vi.fn(), locateForPublish: vi.fn(),
  }),
}));

vi.mock('../components/BatchPhaseIndicator', /* batchIndicatorMockFactory 隐藏本用例无关的批量阶段组件。 */ () => ({ BatchPhaseIndicator: /* BatchPhaseIndicatorMock 不渲染批量阶段。 */ () => null }));
vi.mock('../components/ManualLocationPicker', /* locationPickerMockFactory 隐藏本用例无关的地点弹窗。 */ () => ({ ManualLocationPicker: /* ManualLocationPickerMock 不渲染地点弹窗。 */ () => null }));

import ItemList from './ItemList';

// itemFixture 是列表 DTO 直接提供 AI 摘要状态的商品样本。
const itemFixture: Item = { id: 'item-1', cookie_id: 'account-1', item_id: 'item-1', item_title: '测试商品', item_price: '99', ai_override: 'enabled' };

describe('ItemList 商品级 AI 入口', /* itemListAISuite 验证列表摘要、按需读取和保存后局部更新。 */ () => {
  beforeEach(/* itemListMockReset 恢复商品列表和 AI 配置默认响应。 */ () => {
    vi.clearAllMocks();
    itemListMocks.getAccountDetails.mockResolvedValue([{ id: 'account-1', enabled: true, auto_confirm: false, remark: '测试账号' }]);
    itemListMocks.getItems.mockResolvedValue([itemFixture]);
    itemListMocks.getShippingRules.mockResolvedValue([]);
    itemListMocks.getItemPublishBatches.mockResolvedValue([]);
    itemListMocks.getItemAISettings.mockResolvedValue({ ai_override: 'enabled', item_context: '材质：全新' });
    itemListMocks.updateItemAISettings.mockResolvedValue({ ai_override: 'disabled', item_context: '材质：全新' });
  });

  afterEach(/* itemListCleanup 清理页面 DOM。 */ () => cleanup());

  test('列表不逐商品读取，点击 Bot 入口后才加载并更新卡片状态', /* lazyItemAISettingsCase 验证按需请求边界。 */ async () => {
    render(<ItemList onConfigureDelivery={vi.fn()} />);
    await waitFor(/* listLoadedAssertion 等待列表 DTO 状态展示。 */ () => expect(screen.getByText('已启用')).toBeTruthy());
    expect(itemListMocks.getItemAISettings).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '配置商品 AI 客服：测试商品' }));
    await waitFor(/* detailRequestAssertion 等待用户点击后发起单商品配置读取。 */ () => expect(itemListMocks.getItemAISettings).toHaveBeenCalledWith('account-1', 'item-1', expect.objectContaining({ signal: expect.any(AbortSignal) })));
    await waitFor(/* contextLoadedAssertion 等待商品专属资料写入弹窗。 */ () => expect(screen.getByDisplayValue('材质：全新')).toBeTruthy());
    fireEvent.click(screen.getByRole('radio', { name: /强制停用/ }));
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
    await waitFor(/* cardStateAssertion 等待保存后的列表摘要局部更新。 */ () => expect(screen.getByText('已停用')).toBeTruthy());
    expect(itemListMocks.getItems).toHaveBeenCalledTimes(1);
  });
});
