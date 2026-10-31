// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { useState } from 'react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import * as api from './api';
import { useRuleActions,type RuleActionsOptions } from './ruleActions';
import type { Card,DeliveryTemplate,Item,ShippingRule } from './types';

vi.mock('./api', /* branchApiMockFactory 提供规则动作分支测试的确定性 API 替身。 */ () => ({
  clearDefaultReplyRecords: vi.fn(), deleteDefaultReply: vi.fn(), deleteReplyRule: vi.fn(), deleteShippingRule: vi.fn(),
  getCards: vi.fn(), getDefaultReply: vi.fn(), getItems: vi.fn(), getShippingRules: vi.fn(),
  resolveAutomationRun: vi.fn(), resolveDeferredAutomationTask: vi.fn(),
  updateDefaultReply: vi.fn(), updateReplyRule: vi.fn(), updateShippingRule: vi.fn(),
}));

// itemFixture 是普通单规格商品。
const itemFixture = { cookie_id: 'account-1', item_id: 'item-1', item_title: '测试商品', is_multi_spec: false } as Item;
// multiItemFixture 是多规格商品，用于规格校验分支。
const multiItemFixture = { cookie_id: 'account-1', item_id: 'item-2', item_title: '多规格', is_multi_spec: true } as Item;
// templateFixture 是带一个卡密变量与一个自定义变量的发货模板。
const templateFixture = { id: 5, keys: ['card'], custom_keys: ['note'] } as unknown as DeliveryTemplate;

// harnessExtras 收集 Harness 暴露给断言的刷新替身与外部联动状态。
interface HarnessExtras {
  // loadAutomationRules 是自动化规则刷新替身。
  loadAutomationRules: ReturnType<typeof vi.fn>;
  // setActiveTab 记录外部联动切换的页签。
  setActiveTab: ReturnType<typeof vi.fn>;
  // onHandled 记录外部联动完成通知。
  onHandled: ReturnType<typeof vi.fn>;
}

// harnessOptions 允许单个用例覆盖账号、商品、模板和外部联动目标。
interface HarnessOptions {
  // account 是当前选中的账号，空串表示未选择。
  account?: string;
  // deliveryTemplates 是可选发货模板。
  deliveryTemplates?: DeliveryTemplate[];
  // initialDeliveryTarget 是商品页跳转目标。
  initialDeliveryTarget?: RuleActionsOptions['initialDeliveryTarget'];
}

// extras 保存最近一次渲染的 Harness 替身，供用例断言。
let extras: HarnessExtras;

// useHarness 创建带真实 React 状态的规则动作 Hook。
const useHarness = (config: HarnessOptions) => {
  // selectedAccountId 是测试中的当前账号。
  const [selectedAccountId, setSelectedAccountId] = useState(config.account ?? 'account-1');
  // items 是测试中的商品参考数据。
  const [items, setItems] = useState<Item[]>([itemFixture, multiItemFixture]);
  // setters 保存无需断言的外部联动写入函数。
  const [, setRules] = useState<ShippingRule[]>([]);
  // cards 占位保存外部联动写入的卡密。
  const [, setCards] = useState<Card[]>([]);
  // loading 占位保存加载指示器。
  const [, setLoading] = useState(false);
  extras = extras ?? ({} as HarnessExtras);
  // options 是 Hook 的完整依赖。
  const options: RuleActionsOptions = {
    selectedAccountId, setSelectedAccountId, setActiveTab: extras.setActiveTab as never, items, deliveryTemplates: config.deliveryTemplates,
    setAutomationRules: setRules, setCards, setItems, setLoading,
    loadAutomationRules: extras.loadAutomationRules as never, loadReferenceData: (/* refreshReference 模拟参考数据刷新。 */ async () => undefined) as never,
    loadReplyRules: (/* refreshReplies 模拟回复刷新。 */ async () => undefined) as never, loadDefaultReplies: (/* refreshDefaults 模拟默认回复刷新。 */ async () => undefined) as never,
    initialDeliveryTarget: config.initialDeliveryTarget, onDeliveryTargetHandled: extras.onHandled as never,
  };
  return useRuleActions(options);
};

// setup 渲染 Hook 并重置 extras。
const setup = (config: HarnessOptions = {}) => {
  extras = { loadAutomationRules: vi.fn(/* refreshRules 模拟规则刷新。 */ async () => undefined), setActiveTab: vi.fn(), onHandled: vi.fn() };
  return renderHook(/* 当前回调构建隔离的规则动作状态。 */ () => useHarness(config));
};

describe('useRuleActions 分支', /* 当前套件覆盖校验提示、异常路径、联动加载与轻提示计时。 */ () => {
  // alertMock 记录用户可见的原生提示。
  let alertMock: ReturnType<typeof vi.fn>;
  beforeEach(/* 当前回调重置 API 替身与全局对话框。 */ () => {
    vi.clearAllMocks();
    alertMock = vi.fn();
    vi.stubGlobal('alert', alertMock);
    vi.stubGlobal('confirm', vi.fn(/* acceptConfirm 总是确认。 */ () => true));
    vi.mocked(api.updateShippingRule).mockResolvedValue({ success: true } as never);
    vi.mocked(api.getCards).mockResolvedValue([]);
    vi.spyOn(console, 'error').mockImplementation(/* silenceConsole 屏蔽预期内的日志。 */ () => undefined);
    vi.spyOn(console, 'warn').mockImplementation(/* silenceConsole 屏蔽预期内的日志。 */ () => undefined);
  });
  afterEach(/* 当前回调还原全局替身与计时器。 */ () => { vi.unstubAllGlobals(); vi.restoreAllMocks(); vi.useRealTimers(); });

  test('未选账号时不能新建规则，保存缺少账号时提示', /* 当前回调覆盖账号前置校验。 */ async () => {
    // hook 是未选账号的规则动作。
    const hook = setup({ account: '' });
    act(/* 当前回调尝试新建规则。 */ () => hook.result.current.openNewAutomationRule());
    expect(alertMock).toHaveBeenCalledWith('请先选择账号');
    act(/* 当前回调写入无账号草稿。 */ () => hook.result.current.setEditingAutomationRule({ trigger_type: 'order_paid', variants: [] }));
    await act(/* 当前回调保存无账号草稿。 */ async () => hook.result.current.handleSaveAutomationRule());
    expect(alertMock).toHaveBeenCalledWith('请选择账号');
    hook.unmount();
  });

  test('保存前校验卡密、模板绑定、自定义变量和多规格字段', /* 当前回调逐个触发发货规则校验提示。 */ async () => {
    // hook 是带模板与多规格商品的规则动作。
    const hook = setup({ deliveryTemplates: [templateFixture] });
    // save 写入草稿后执行保存。
    const save = async (/* draft 是待保存的规则草稿。 */ draft: Partial<ShippingRule>) => {
      act(/* 当前回调写入草稿。 */ () => hook.result.current.setEditingAutomationRule({ trigger_type: 'order_paid', cookie_id: 'account-1', item_id: 'item-1', ...draft }));
      await act(/* 当前回调保存草稿。 */ async () => hook.result.current.handleSaveAutomationRule());
    };
    await save({ variants: [] });
    expect(alertMock).toHaveBeenLastCalledWith('请选择发货卡密库存或发货模板');
    await save({ variants: [{ id: 'v', spec_name: '', spec_value: '', delivery_mode: 'template', delivery_count: 1, enabled: true } as never] });
    expect(alertMock).toHaveBeenLastCalledWith('请选择发货卡密库存或发货模板');
    await save({ variants: [{ id: 'v', spec_name: '', spec_value: '', delivery_mode: 'template', delivery_template_id: 99, delivery_count: 1, enabled: true } as never] });
    expect(alertMock).toHaveBeenLastCalledWith('请为发货模板的每个变量绑定卡密库存');
    await save({ variants: [{ id: 'v', spec_name: '', spec_value: '', delivery_mode: 'template', delivery_template_id: 5, template_bindings: [{ variable_key: 'card', card_id: 7 }], custom_variables: { note: ' ' }, delivery_count: 1, enabled: true } as never] });
    expect(alertMock).toHaveBeenLastCalledWith('请填写发货模板要求的全部自定义变量');
    await save({ item_id: 'item-2', variants: [{ id: 'v', spec_name: ' ', spec_value: 'x', card_id: 7, delivery_count: 1, enabled: true }] });
    expect(alertMock).toHaveBeenLastCalledWith('多规格商品必须填写每一行的规格名称和规格值');
    expect(api.updateShippingRule).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('求评价规则缺少文案时拒绝，改价目标非法时拒绝', /* 当前回调覆盖评价与改价专属校验。 */ async () => {
    // hook 是规则动作。
    const hook = setup();
    act(/* 当前回调打开评价规则。 */ () => hook.result.current.openNewAutomationRule('review_missing_timeout'));
    act(/* 当前回调清空求评价文案。 */ () => hook.result.current.setEditingAutomationRule(/* draft 是当前草稿。 */ draft => ({ ...draft, actions: [{ action_type: 'send_text', message_template: ' ', enabled: true, sort_order: 1 }] })));
    await act(/* 当前回调保存评价规则。 */ async () => hook.result.current.handleSaveAutomationRule());
    expect(alertMock).toHaveBeenLastCalledWith('请填写求评价文案');
    act(/* 当前回调切换到改价规则。 */ () => hook.result.current.handleTriggerChange('order_created'));
    act(/* 当前回调写入非法价格。 */ () => hook.result.current.updateAdjustPriceTarget('0'));
    await act(/* 当前回调保存非法改价。 */ async () => hook.result.current.handleSaveAutomationRule());
    expect(alertMock).toHaveBeenLastCalledWith('请填写 0.01 到 1000000 元、最多两位小数的目标价格');
    hook.unmount();
  });

  test('保存失败时提示错误并允许再次提交', /* 当前回调覆盖保存异常分支。 */ async () => {
    vi.mocked(api.updateShippingRule).mockRejectedValueOnce(new Error('boom'));
    // hook 是规则动作。
    const hook = setup();
    act(/* 当前回调打开新规则。 */ () => hook.result.current.openNewAutomationRule());
    act(/* 当前回调填入合法规格。 */ () => hook.result.current.updateVariant(0, { card_id: 7 }));
    await act(/* 当前回调保存并遇到失败。 */ async () => hook.result.current.handleSaveAutomationRule());
    expect(alertMock).toHaveBeenLastCalledWith('保存失败：boom');
    expect(hook.result.current.automationSubmitState.submitting).toBe(false);
    hook.unmount();
  });

  test('触发类型切换、商品切换与规格追加更新草稿', /* 当前回调覆盖草稿变换函数。 */ () => {
    // hook 是规则动作。
    const hook = setup();
    act(/* 当前回调在无草稿时调用变换，应被忽略。 */ () => { hook.result.current.handleTriggerChange('order_paid'); hook.result.current.handleAutomationItemChange('x'); hook.result.current.updateVariant(0, {}); hook.result.current.updateAdjustPriceTarget('1'); hook.result.current.updateAdjustPriceNotifyText('t'); hook.result.current.appendDeliveryContent(); });
    expect(hook.result.current.editingAutomationRule).toBeNull();
    act(/* 当前回调打开新规则并切到多规格商品。 */ () => hook.result.current.openNewAutomationRule());
    act(/* 当前回调选择多规格商品。 */ () => hook.result.current.handleAutomationItemChange('item-2'));
    expect(hook.result.current.isMultiSpecRule).toBe(true);
    act(/* 当前回调写入规格后追加一行。 */ () => hook.result.current.updateVariant(0, { spec_name: '颜色', spec_value: '红', card_id: 3 }));
    act(/* 当前回调追加规格行。 */ () => hook.result.current.appendDeliveryContent());
    expect(hook.result.current.displayVariants[1]).toMatchObject({ spec_name: '颜色', spec_value: '红' });
    act(/* 当前回调切换到评价触发。 */ () => hook.result.current.handleTriggerChange('review_missing_timeout'));
    expect(hook.result.current.editingAutomationRule?.variants).toEqual([]);
    act(/* 当前回调为已有提醒动作更新文案并为缺失时追加。 */ () => hook.result.current.handleTriggerChange('order_created'));
    act(/* 当前回调首次写入提醒。 */ () => hook.result.current.updateAdjustPriceNotifyText('a'));
    act(/* 当前回调覆盖已有提醒。 */ () => hook.result.current.updateAdjustPriceNotifyText('b'));
    expect(hook.result.current.editingAutomationRule?.actions?.filter(/* action 是当前动作。 */ action => action.action_type === 'send_text')).toHaveLength(1);
    hook.unmount();
  });

  test('打开已有规则复制动作与规格，并支持切换启用状态', /* 当前回调覆盖 openAutomationRule 与 toggle。 */ async () => {
    // rule 是已有的评价规则。
    const rule = { id: 'r1', cookie_id: 'account-1', trigger_type: 'review_missing_timeout', enabled: true, config_json: '', actions: [], variants: [] } as unknown as ShippingRule;
    // hook 是规则动作。
    const hook = setup();
    act(/* 当前回调打开已有规则。 */ () => hook.result.current.openAutomationRule(rule));
    expect(hook.result.current.showAutomationModal).toBe(true);
    expect(hook.result.current.editingAutomationRule?.actions?.length).toBeGreaterThan(0);
    await act(/* 当前回调切换启用。 */ async () => hook.result.current.handleToggleAutomation(rule));
    expect(api.updateShippingRule).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }));
    vi.mocked(api.updateShippingRule).mockRejectedValueOnce(new Error('x'));
    await act(/* 当前回调切换失败。 */ async () => hook.result.current.handleToggleAutomation(rule));
    expect(alertMock).toHaveBeenLastCalledWith('操作失败：x');
    hook.unmount();
  });

  test('删除与恢复动作：取消确认不请求，失败时提示', /* 当前回调覆盖 confirm 取消与异常分支。 */ async () => {
    // hook 是规则动作。
    const hook = setup();
    // confirmMock 控制确认对话框结果。
    const confirmMock = vi.fn(/* rejectConfirm 总是取消。 */ () => false);
    vi.stubGlobal('confirm', confirmMock);
    await act(/* 当前回调在取消确认下执行全部动作。 */ async () => {
      await hook.result.current.handleDeleteAutomation('r');
      await hook.result.current.handleResolveRunIssue(1, 'continue');
      await hook.result.current.handleResolveDeferredIssue(1, 'dismiss');
      await hook.result.current.handleDeleteReply('r');
      await hook.result.current.handleDeleteDefaultReply('a');
      await hook.result.current.handleClearDefaultReplyRecords('a');
    });
    expect(api.deleteShippingRule).not.toHaveBeenCalled();
    expect(api.resolveAutomationRun).not.toHaveBeenCalled();
    confirmMock.mockReturnValue(true);
    vi.mocked(api.deleteShippingRule).mockRejectedValueOnce(new Error('d'));
    vi.mocked(api.resolveAutomationRun).mockRejectedValue(new Error('r'));
    vi.mocked(api.resolveDeferredAutomationTask).mockRejectedValue(new Error('t'));
    vi.mocked(api.deleteDefaultReply).mockRejectedValue(new Error('dd'));
    vi.mocked(api.clearDefaultReplyRecords).mockRejectedValue(new Error('cc'));
    await act(/* 当前回调在确认下触发各失败分支。 */ async () => {
      await hook.result.current.handleDeleteAutomation('r');
      await hook.result.current.handleResolveRunIssue(1, 'retry');
      await hook.result.current.handleResolveRunIssue(1, 'cancel');
      await hook.result.current.handleResolveDeferredIssue(1, 'retry');
      await hook.result.current.handleDeleteDefaultReply('a');
      await hook.result.current.handleClearDefaultReplyRecords('a');
    });
    // message 是各失败分支期望出现的用户提示。
    for (const /* message 是各失败分支期望出现的用户提示。 */ message of ['删除失败：d', '处理失败：r', '处理失败：t', '删除失败：dd', '清空失败：cc']) expect(alertMock).toHaveBeenCalledWith(message);
    hook.unmount();
  });

  test('关键词回复：删除失败、图片类型校验、轻提示自动消失', /* 当前回调覆盖回复规则剩余分支与计时器。 */ async () => {
    vi.useFakeTimers();
    vi.mocked(api.deleteReplyRule).mockRejectedValueOnce(new Error('del'));
    // hook 是规则动作。
    const hook = setup();
    await act(/* 当前回调删除失败。 */ async () => hook.result.current.handleDeleteReply('r'));
    expect(hook.result.current.toast).toEqual({ type: 'error', text: '删除失败：del' });
    act(/* 当前回调再次提示以重置计时。 */ () => hook.result.current.showReplyToast('success', 'ok'));
    act(/* 当前回调推进到提示消失。 */ () => { vi.advanceTimersByTime(3000); });
    expect(hook.result.current.toast).toBeNull();
    act(/* 当前回调打开图片回复草稿且缺图。 */ () => { hook.result.current.handleAddReplyRule(); });
    act(/* 当前回调写入缺图的图片类型草稿。 */ () => hook.result.current.setEditingReplyRule({ keyword: 'k', type: 'image', image_url: ' ' }));
    await act(/* 当前回调保存缺图草稿。 */ async () => hook.result.current.handleSaveReplyRule());
    expect(api.updateReplyRule).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('默认回复：未选账号、读取失败回退、校验与保存失败', /* 当前回调覆盖默认回复剩余分支。 */ async () => {
    // hook 是未选账号时的规则动作。
    const empty = setup({ account: '' });
    await act(/* 当前回调在未选账号时打开默认回复。 */ async () => empty.result.current.openDefaultReplyModal());
    expect(alertMock).toHaveBeenLastCalledWith('请先选择账号');
    empty.unmount();
    vi.mocked(api.getDefaultReply).mockRejectedValueOnce(new Error('x'));
    // hook 是已选账号的规则动作。
    const hook = setup();
    await act(/* 当前回调读取失败后回退空配置。 */ async () => hook.result.current.openDefaultReplyModal());
    expect(hook.result.current.defaultForm).toMatchObject({ enabled: false, reply_content: '' });
    await act(/* 当前回调保存启用但无内容的配置。 */ async () => { hook.result.current.setDefaultForm({ cookie_id: 'account-1', enabled: true, reply_content: ' ', reply_once: false, reply_image_url: '', reply_library_image_id: 0 }); });
    await act(/* 当前回调保存无内容配置。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(alertMock).toHaveBeenLastCalledWith('启用默认回复时，请填写回复内容或选择/填写图片');
    act(/* 当前回调清空账号。 */ () => hook.result.current.setDefaultForm({ cookie_id: '', enabled: false, reply_content: '', reply_once: false, reply_image_url: '', reply_library_image_id: 0 }));
    await act(/* 当前回调保存无账号配置。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(alertMock).toHaveBeenLastCalledWith('请先选择账号');
    vi.mocked(api.updateDefaultReply).mockRejectedValueOnce(new Error('sv'));
    act(/* 当前回调写入合法配置。 */ () => hook.result.current.setDefaultForm({ cookie_id: 'account-1', enabled: true, reply_content: 'hi', reply_once: false, reply_image_url: '', reply_library_image_id: 0 }));
    await act(/* 当前回调保存失败。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(alertMock).toHaveBeenLastCalledWith(expect.stringContaining('sv'));
    hook.unmount();
  });

  test('商品页联动：已有规则直接打开，缺失时按商品建草稿', /* 当前回调覆盖外部跳转加载的两个分支。 */ async () => {
    // existing 是目标商品已有的订单支付规则。
    const existing = { id: 'e', cookie_id: 'account-1', item_id: 'item-1', trigger_type: 'order_paid', actions: [], variants: [] } as unknown as ShippingRule;
    vi.mocked(api.getShippingRules).mockResolvedValue([existing]);
    vi.mocked(api.getItems).mockResolvedValue([itemFixture]);
    // linked 是命中已有规则的联动渲染。
    const linked = setup({ initialDeliveryTarget: { cookieId: 'account-1', itemId: 'item-1' } as never });
    await act(/* 当前回调等待联动加载完成。 */ async () => { await Promise.resolve(); });
    await vi.waitFor(/* 当前回调等待弹窗打开。 */ () => expect(linked.result.current.showAutomationModal).toBe(true));
    expect(linked.result.current.editingAutomationRule?.id).toBe('e');
    expect(extras.onHandled).toHaveBeenCalled();
    linked.unmount();
    vi.mocked(api.getShippingRules).mockResolvedValue([]);
    vi.mocked(api.getCards).mockRejectedValue(new Error('cards'));
    // draftHook 是目标商品没有规则的联动渲染。
    const draftHook = setup({ initialDeliveryTarget: { cookieId: 'account-1', itemId: 'item-1' } as never });
    await vi.waitFor(/* 当前回调等待草稿弹窗。 */ () => expect(draftHook.result.current.showAutomationModal).toBe(true));
    expect(draftHook.result.current.editingAutomationRule).toMatchObject({ item_id: 'item-1', item_title: '测试商品' });
    draftHook.unmount();
  });

  test('商品页联动加载失败时提示并仍通知父页面', /* 当前回调覆盖联动异常分支。 */ async () => {
    vi.mocked(api.getShippingRules).mockRejectedValue(new Error('net'));
    vi.mocked(api.getItems).mockResolvedValue([]);
    // hook 是联动失败的渲染。
    const hook = setup({ initialDeliveryTarget: { cookieId: 'account-1', itemId: 'item-1' } as never });
    await vi.waitFor(/* 当前回调等待失败提示。 */ () => expect(alertMock).toHaveBeenCalledWith('无法加载该商品的自动化规则'));
    await vi.waitFor(/* 当前回调等待父页面通知。 */ () => expect(extras.onHandled).toHaveBeenCalled());
    hook.unmount();
  });

  test('商品页联动在卸载后不再写入状态', /* 当前回调覆盖 cancelled 早退。 */ async () => {
    // release 在测试中延后释放规则列表请求。
    let release: (value: ShippingRule[]) => void = () => undefined;
    vi.mocked(api.getShippingRules).mockReturnValue(new Promise<ShippingRule[]>(/* resolve 暴露给测试控制释放时机。 */ resolve => { release = resolve; }));
    vi.mocked(api.getItems).mockResolvedValue([]);
    // hook 是即将卸载的联动渲染。
    const hook = setup({ initialDeliveryTarget: { cookieId: 'account-1', itemId: 'item-1' } as never });
    hook.unmount();
    await act(/* 当前回调释放请求。 */ async () => { release([]); await Promise.resolve(); });
    expect(extras.onHandled).not.toHaveBeenCalled();
  });
});
