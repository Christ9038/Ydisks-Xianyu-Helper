// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { useState } from 'react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item } from './api';
import * as api from './api';
import { emptyPublishItemForm,useItemActions,type ItemActionsOptions,type PublishItemForm } from './itemActions';

vi.mock('./api', /* branchApiMockFactory 提供商品动作分支测试的确定性 API 替身。 */ () => ({
  createItem: vi.fn(), deleteItem: vi.fn(), getPublishLocations: vi.fn(),
  itemErrorMessage: /* errorMessageMock 将异常转换为稳定文本。 */ (error: unknown, fallback: string) => error instanceof Error ? error.message : fallback,
  publishItem: vi.fn(), recommendPublishCategory: vi.fn(), syncItemsFromAccount: vi.fn(), updateItem: vi.fn(),
}));

// itemFixture 是商品动作使用的商品样本。
const itemFixture = { id: 'i1', cookie_id: 'account-1', item_id: 'i1', item_title: '旧', item_price: '10', item_detail: 'd' } as Item;
// locationFixture 是发货地样本。
const locationFixture = { area: '区', city: '市', division_id: 'd', longitude: 1, latitude: 2, poi_id: 'p', poi_name: 'n', province: '省' };
// harnessState 保存最近一次渲染暴露的外部替身。
let harnessState: { /** loadItems 是列表刷新替身。 */ loadItems: ReturnType<typeof vi.fn>; /** onConfigureDelivery 是发布后规则配置回调。 */ onConfigureDelivery: ReturnType<typeof vi.fn>; /** items 读取当前商品列表。 */ items: () => Item[]; /** batch 记录批量定位写入。 */ batch: { /** locations 是批量发货地。 */ locations: unknown[] } };

// useHarness 创建带真实状态的商品动作 Hook；account 为空串模拟未选账号。
const useHarness = (account: string) => {
  // selectedAccount 是当前账号。
  const [selectedAccount, setSelectedAccount] = useState(account);
  // items 是商品列表。
  const [items, setItems] = useState<Item[]>([itemFixture]);
  // loadItems 与 onConfigureDelivery 是稳定的外部替身。
  const [loadItems] = useState(/* createLoadItems 创建列表刷新替身。 */ () => vi.fn(/* refreshNothing 模拟刷新完成。 */ async () => undefined));
  const [onConfigureDelivery] = useState(() => vi.fn());
  // batch 记录批量发货地写入。
  const [batch] = useState({ locations: [] as unknown[] });
  harnessState = { loadItems, onConfigureDelivery, items: /* readItems 读取最新商品。 */ () => items, batch };
  // options 是 Hook 的完整依赖。
  const options: ItemActionsOptions = {
    selectedAccount, setSelectedAccount, setItems, loadItems, loadShippingRules: (async () => undefined) as never, onConfigureDelivery,
    setBatchLocations: /* writeBatchLocations 记录批量发货地。 */ (/* value 是批量发货地。 */ (value: unknown) => { batch.locations = value as unknown[]; }) as never, setBatchLocation: (/* ignoreLocation 忽略批量选中的发货地。 */ () => undefined) as never,
  };
  return useItemActions(options);
};

// validForm 构造能通过全部发布校验的表单。
const validForm = (/* patch 覆盖默认字段。 */ patch: Partial<PublishItemForm> = {}): PublishItemForm => ({
  ...emptyPublishItemForm('account-1'), title: 't', price: '9', quantity: '2', images: [new File(['x'], 'a.png')], ...patch,
});

describe('useItemActions 分支', /* 当前套件覆盖校验提示、异常路径、类目推荐竞态与定位分支。 */ () => {
  // alertMock 记录用户可见提示。
  let alertMock: ReturnType<typeof vi.fn>;
  beforeEach(/* 当前回调重置 API 与全局替身。 */ () => {
    vi.clearAllMocks();
    alertMock = vi.fn();
    vi.stubGlobal('alert', alertMock);
    vi.stubGlobal('confirm', vi.fn(/* acceptConfirm 总是确认。 */ () => true));
    vi.spyOn(console, 'error').mockImplementation(/* silenceConsole 屏蔽预期内的错误日志。 */ () => undefined);
    vi.mocked(api.publishItem).mockResolvedValue({ success: true } as never);
    vi.mocked(api.getPublishLocations).mockResolvedValue([locationFixture] as never);
  });
  afterEach(/* 当前回调还原全局状态。 */ () => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

  test('同步：未选账号提示，失败提示，成功使用服务端文案', /* 当前回调覆盖同步三个分支。 */ async () => {
    // empty 是未选账号的 Hook。
    const empty = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness(''));
    await act(/* 当前回调同步无账号。 */ async () => empty.result.current.handleSync());
    expect(alertMock).toHaveBeenLastCalledWith('请先选择账号');
    empty.unmount();
    // hook 是已选账号的 Hook。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    vi.mocked(api.syncItemsFromAccount).mockRejectedValueOnce(new Error('sync-bad'));
    await act(/* 当前回调同步失败。 */ async () => hook.result.current.handleSync());
    expect(alertMock).toHaveBeenLastCalledWith('sync-bad');
    expect(hook.result.current.loading).toBe(false);
    vi.mocked(api.syncItemsFromAccount).mockResolvedValueOnce({ message: '完成X' } as never);
    await act(/* 当前回调同步成功。 */ async () => hook.result.current.handleSync());
    expect(alertMock).toHaveBeenLastCalledWith('完成X');
    vi.mocked(api.syncItemsFromAccount).mockResolvedValueOnce(undefined as never);
    await act(/* 当前回调同步成功但无文案。 */ async () => hook.result.current.handleSync());
    expect(alertMock).toHaveBeenLastCalledWith('商品同步完成');
    hook.unmount();
  });

  test('编辑、删除、添加：无选中项早退、取消确认、异常提示', /* 当前回调覆盖 CRUD 的非主路径。 */ async () => {
    // hook 是商品动作。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    await act(/* 当前回调在无选中商品时保存编辑。 */ async () => hook.result.current.handleSaveEdit());
    expect(api.updateItem).not.toHaveBeenCalled();
    act(/* 当前回调打开编辑弹窗。 */ () => hook.result.current.handleEdit(itemFixture));
    vi.mocked(api.updateItem).mockRejectedValueOnce(new Error('u'));
    await act(/* 当前回调保存失败。 */ async () => hook.result.current.handleSaveEdit());
    expect(alertMock).toHaveBeenLastCalledWith('更新失败，请重试');
    expect(hook.result.current.showEditModal).toBe(true);
    vi.stubGlobal('confirm', vi.fn(/* rejectConfirm 总是取消。 */ () => false));
    await act(/* 当前回调取消删除。 */ async () => hook.result.current.handleDelete(itemFixture));
    expect(api.deleteItem).not.toHaveBeenCalled();
    vi.stubGlobal('confirm', vi.fn(/* acceptConfirm 总是确认。 */ () => true));
    vi.mocked(api.deleteItem).mockRejectedValueOnce(new Error('d'));
    await act(/* 当前回调删除失败。 */ async () => hook.result.current.handleDelete(itemFixture));
    expect(alertMock).toHaveBeenLastCalledWith('删除失败，请重试');
    expect(harnessState.items()).toHaveLength(1);
    await act(/* 当前回调添加缺少商品 ID。 */ async () => hook.result.current.handleAddItem());
    expect(alertMock).toHaveBeenLastCalledWith('请选择账号并填写商品ID');
    act(/* 当前回调填写含图片的添加表单。 */ () => hook.result.current.setAddForm({ cookie_id: 'account-1', item_id: 'n', item_title: 't', item_price: '1', item_image: 'u.jpg' }));
    vi.mocked(api.createItem).mockRejectedValueOnce(new Error('c'));
    await act(/* 当前回调添加失败。 */ async () => hook.result.current.handleAddItem());
    expect(alertMock).toHaveBeenLastCalledWith('添加失败，请重试');
    vi.mocked(api.createItem).mockResolvedValueOnce({ success: true } as never);
    await act(/* 当前回调添加成功。 */ async () => hook.result.current.handleAddItem());
    expect(api.createItem).toHaveBeenLastCalledWith('account-1', expect.objectContaining({ item_detail: JSON.stringify({ item_image: 'u.jpg' }) }));
    hook.unmount();
  });

  test('发布校验按顺序给出提示且不触达接口', /* 当前回调逐项触发发布前校验。 */ async () => {
    // hook 是商品动作。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    // sku 构造 SKU 行。
    const sku = (/* price 是售价文本。 */ price: string, /* quantity 是库存文本。 */ quantity: string) => ({ price, quantity }) as never;
    // cases 是表单补丁与期望提示的对应表。
    const cases: [Partial<PublishItemForm>, string][] = [
      [{ cookie_id: '' }, '请选择发布账号'],
      [{ title: ' ' }, '请填写商品标题'],
      [{ specs: [{}] as never, skuRows: [] }, '请先填写每种规格的规格值'],
      [{ specs: [{}] as never, skuRows: [sku('', '1')] }, '请填写每个 SKU 的售价'],
      [{ specs: [{}] as never, skuRows: [sku('1', '1.5')] }, '请填写每个 SKU 的库存'],
      [{ price: ' ' }, '请填写商品价格'],
      [{ quantity: '0' }, '库存数量必须大于 0'],
      [{ images: [] }, '至少上传 1 张商品图片'],
      [{ postage_mode: 'fixed', postage: ' ' }, '请填写一口价邮费'],
    ];
    // patch 是表单覆盖字段；message 是期望的用户提示。
    for (const /* patch 是表单覆盖字段，message 是期望的用户提示。 */ [patch, message] of cases) {
      act(/* 当前回调写入待校验表单。 */ () => hook.result.current.setPublishForm(validForm(patch)));
      await act(/* 当前回调执行发布。 */ async () => hook.result.current.handlePublishItem());
      expect(alertMock).toHaveBeenLastCalledWith(message);
    }
    expect(api.publishItem).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('发布：规格库存求和、无 item_id 的成功提示、失败后恢复状态', /* 当前回调覆盖发布成功与失败分支。 */ async () => {
    // hook 是商品动作。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    act(/* 当前回调写入多规格表单。 */ () => hook.result.current.setPublishForm(validForm({ specs: [{}] as never, skuRows: [{ price: '1', quantity: '2' }, { price: '1', quantity: '3' }] as never })));
    await act(/* 当前回调发布成功但没有商品 ID。 */ async () => hook.result.current.handlePublishItem());
    expect(api.publishItem).toHaveBeenCalledWith(expect.objectContaining({ quantity: 5 }));
    expect(alertMock).toHaveBeenLastCalledWith('商品发布成功');
    expect(harnessState.onConfigureDelivery).not.toHaveBeenCalled();
    act(/* 当前回调再次写入合法表单。 */ () => hook.result.current.setPublishForm(validForm()));
    vi.mocked(api.publishItem).mockRejectedValueOnce(new Error('pub-bad'));
    await act(/* 当前回调发布失败。 */ async () => hook.result.current.handlePublishItem());
    expect(alertMock).toHaveBeenLastCalledWith('pub-bad');
    expect(hook.result.current.publishing).toBe(false);
    hook.unmount();
  });

  test('类目推荐：缺账号/关键词提示、失败提示、取消后晚到响应被丢弃', /* 当前回调覆盖类目推荐竞态。 */ async () => {
    // hook 是商品动作。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    await act(/* 当前回调缺账号推荐。 */ async () => hook.result.current.handleRecommendPublishCategory());
    expect(alertMock).toHaveBeenLastCalledWith('请先选择发布账号');
    act(/* 当前回调写入账号。 */ () => hook.result.current.setPublishForm(validForm()));
    await act(/* 当前回调缺关键词推荐。 */ async () => hook.result.current.handleRecommendPublishCategory());
    expect(alertMock).toHaveBeenLastCalledWith('请输入类目关键词');
    act(/* 当前回调写入关键词。 */ () => hook.result.current.setPublishCategoryKeyword('关键词'));
    vi.mocked(api.recommendPublishCategory).mockRejectedValueOnce(new Error('rec-bad'));
    await act(/* 当前回调推荐失败。 */ async () => hook.result.current.handleRecommendPublishCategory());
    expect(alertMock).toHaveBeenLastCalledWith('rec-bad');
    expect(hook.result.current.publishCategoryLoading).toBe(false);
    // finishFirst 在测试中延后释放第一次推荐。
    let finishFirst: (value: unknown) => void = () => undefined;
    vi.mocked(api.recommendPublishCategory).mockImplementationOnce(() => new Promise(/* resolve 暴露给测试控制时机。 */ resolve => { finishFirst = resolve; }) as never);
    vi.mocked(api.recommendPublishCategory).mockResolvedValueOnce({ category: { cat_id: 'new' } } as never);
    act(/* 当前回调发起第一次推荐。 */ () => { void hook.result.current.handleRecommendPublishCategory(); });
    await act(/* 当前回调发起第二次推荐使第一次失效。 */ async () => hook.result.current.handleRecommendPublishCategory());
    await act(/* 当前回调释放第一次的晚到响应。 */ async () => { finishFirst({ category: { cat_id: 'stale' } }); await Promise.resolve(); });
    expect(hook.result.current.publishCategory).toEqual({ cat_id: 'new' });
    hook.unmount();
  });

  test('下载模板、打开弹窗回填账号', /* 当前回调覆盖模板下载与弹窗回填。 */ async () => {
    // createObjectURL 记录下载的模板文件，便于读取 CSV 文本。
    const createObjectURL = vi.fn(/* fakeObjectUrl 返回固定临时地址。 */ (_blob: Blob) => 'blob:x');
    URL.createObjectURL = createObjectURL;
    URL.revokeObjectURL = vi.fn();
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(/* silenceConsole 屏蔽预期内的错误日志。 */ () => undefined);
    // hook 是商品动作。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    act(/* 当前回调下载模板。 */ () => hook.result.current.downloadPublishTemplate());
    expect(createObjectURL).toHaveBeenCalled();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:x');
    // csvLines 是下载模板的逐行文本，去掉 Excel 兼容的 BOM。
    const csvLines = (await createObjectURL.mock.calls[0][0].text()).replace(/^﻿/, '').split('\n');
    expect(csvLines[0].startsWith('"账号ID","标题","描述","价格","原价","库存","邮费模式","邮费","图片"')).toBe(true);
    expect(csvLines[0].endsWith('"求评价最多次数","变量.link"')).toBe(true);
    expect(csvLines[3]).toContain('"none"');
    expect(csvLines[3]).toContain('"T1"');
    expect(csvLines[3].endsWith('"https://pan.example.com/s/xxxx"')).toBe(true);
    act(/* 当前回调打开添加与发布弹窗。 */ () => { hook.result.current.openAddModal(); hook.result.current.openPublishModal(); });
    expect(hook.result.current.addForm.cookie_id).toBe('account-1');
    expect(hook.result.current.publishForm.cookie_id).toBe('account-1');
    expect(hook.result.current.showAddModal && hook.result.current.showPublishModal).toBe(true);
    hook.unmount();
  });

  test('图片预览随弹窗创建并释放对象地址', /* 当前回调覆盖预览副作用。 */ () => {
    URL.createObjectURL = vi.fn(/* fakePreviewUrl 返回固定预览地址。 */ () => 'blob:p');
    URL.revokeObjectURL = vi.fn();
    // hook 是商品动作。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    act(/* 当前回调打开弹窗并写入图片。 */ () => { hook.result.current.setShowPublishModal(true); hook.result.current.setPublishForm(validForm()); });
    expect(hook.result.current.publishImagePreviews).toHaveLength(1);
    act(/* 当前回调关闭弹窗。 */ () => hook.result.current.setShowPublishModal(false));
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:p');
    expect(hook.result.current.publishImagePreviews).toEqual([]);
    hook.unmount();
  });

  test('定位：缺账号、浏览器不支持、无结果、权限拒绝、批量写入', /* 当前回调覆盖定位所有提示分支。 */ async () => {
    // hook 是商品动作。
    const hook = renderHook(/* renderActions 渲染商品动作 Hook。 */ () => useHarness('account-1'));
    await act(/* 当前回调普通定位缺账号。 */ async () => hook.result.current.locateForPublish(false));
    expect(alertMock).toHaveBeenLastCalledWith('请先选择发布账号');
    act(/* 当前回调写入账号。 */ () => hook.result.current.setPublishForm(validForm()));
    vi.stubGlobal('navigator', { geolocation: undefined });
    await act(/* 当前回调浏览器不支持。 */ async () => hook.result.current.locateForPublish(false));
    expect(alertMock).toHaveBeenLastCalledWith('当前浏览器不支持定位');
    // locate 构造可控的浏览器定位替身。
    const locate = (/* behavior 决定定位成功或以指定错误失败。 */ behavior: 'ok' | { /** code 是 Geolocation 错误码。 */ code: number }) => vi.stubGlobal('navigator', { geolocation: {
      getCurrentPosition: /* getCurrentPositionMock 同步触发成功或失败回调。 */ (success: PositionCallback, failure: PositionErrorCallback) => behavior === 'ok'
        ? success({ coords: { longitude: 1, latitude: 2 } } as GeolocationPosition)
        : failure({ ...behavior, PERMISSION_DENIED: 1 } as GeolocationPositionError),
    } });
    locate({ code: 1 });
    await act(/* 当前回调权限被拒。 */ async () => hook.result.current.locateForPublish(false));
    expect(alertMock).toHaveBeenLastCalledWith('定位权限被拒绝，请在浏览器设置中允许定位');
    locate({ code: 2 });
    await act(/* 当前回调其他定位错误。 */ async () => hook.result.current.locateForPublish(false));
    expect(alertMock).toHaveBeenLastCalledWith('无法获取当前位置，请稍后重试');
    locate('ok');
    vi.mocked(api.getPublishLocations).mockResolvedValueOnce([]);
    await act(/* 当前回调附近无发货地。 */ async () => hook.result.current.locateForPublish(false));
    expect(alertMock).toHaveBeenLastCalledWith('当前位置附近没有可用的高德发货地，请稍后重试');
    await act(/* 当前回调批量定位成功。 */ async () => hook.result.current.locateForPublish(true));
    expect(harnessState.batch.locations).toEqual([locationFixture]);
    expect(hook.result.current.locationLoading).toBe(false);
    hook.unmount();
  });
});
