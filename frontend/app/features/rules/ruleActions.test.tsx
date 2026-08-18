// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { useState } from 'react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { clearDefaultReplyRecords,deleteDefaultReply,deleteReplyRule,deleteShippingRule,getDefaultReply,resolveAutomationRun,resolveDeferredAutomationTask,updateDefaultReply,updateReplyRule,updateShippingRule } from './api';
import { useRuleActions,type RuleActionsOptions } from './ruleActions';
import type { Card,DefaultReply,Item,ShippingRule } from './types';

vi.mock('./api', /* ruleActionsApiMockFactory 提供规则动作 Hook 的确定性 API 替身。 */ () => ({
  clearDefaultReplyRecords: vi.fn(),
  deleteDefaultReply: vi.fn(),
  deleteReplyRule: vi.fn(),
  deleteShippingRule: vi.fn(),
  getCards: vi.fn(),
  getDefaultReply: vi.fn(),
  getItems: vi.fn(),
  getShippingRules: vi.fn(),
  resolveAutomationRun: vi.fn(),
  resolveDeferredAutomationTask: vi.fn(),
  updateDefaultReply: vi.fn(),
  updateReplyRule: vi.fn(),
  updateShippingRule: vi.fn(),
}));

// defaultReplyMock 是默认回复读取接口的可控替身。
const defaultReplyMock = vi.mocked(getDefaultReply);
// updateDefaultMock 是默认回复保存接口的可控替身。
const updateDefaultMock = vi.mocked(updateDefaultReply);
// updateReplyMock 是关键词回复保存接口的可控替身。
const updateReplyMock = vi.mocked(updateReplyRule);
// updateShippingMock 是自动化规则保存接口的可控替身。
const updateShippingMock = vi.mocked(updateShippingRule);
// deleteShippingMock 是自动化规则删除接口的可控替身。
const deleteShippingMock = vi.mocked(deleteShippingRule);
// resolveRunMock 是自动化运行恢复接口的可控替身。
const resolveRunMock = vi.mocked(resolveAutomationRun);
// resolveDeferredMock 是延迟任务恢复接口的可控替身。
const resolveDeferredMock = vi.mocked(resolveDeferredAutomationTask);
// deleteReplyMock 是关键词回复删除接口的可控替身。
const deleteReplyMock = vi.mocked(deleteReplyRule);
// deleteDefaultMock 是默认回复删除接口的可控替身。
const deleteDefaultMock = vi.mocked(deleteDefaultReply);
// clearRecordsMock 是默认回复记录清理接口的可控替身。
const clearRecordsMock = vi.mocked(clearDefaultReplyRecords);

// itemFixture 是自动化规则草稿使用的商品。
const itemFixture = { cookie_id: 'account-1', item_id: 'item-1', item_title: '测试商品', is_multi_spec: false } as Item;
// cardFixture 是规则动作依赖的卡密库存。
const cardFixture = { id: 7, name: '测试卡密', type: 'data' } as Card;
// defaultReplyFixture 是默认回复读取结果。
const defaultReplyFixture = { cookie_id: 'account-1', enabled: true, reply_content: '欢迎', reply_once: false, reply_image_url: '' } as DefaultReply;

// useRuleActionsHarness 创建带有真实 React 状态容器的规则动作 Hook。
const useRuleActionsHarness = () => {
  // selectedAccountId 保存测试中的当前账号。
  const [selectedAccountId, setSelectedAccountId] = useState('account-1');
  // activeTab 保存测试中的当前页签。
  const [, setActiveTab] = useState<'automation' | 'reply' | 'default'>('automation');
  // items 保存测试中的商品参考数据。
  const [items] = useState<Item[]>([itemFixture]);
  // automationRules 保存外部联动写入的规则列表。
  const [, setAutomationRules] = useState<ShippingRule[]>([]);
  // cards 保存外部联动写入的卡密列表。
  const [, setCards] = useState<Card[]>([cardFixture]);
  // linkedItems 保存外部联动写入的商品列表。
  const [, setItems] = useState<Item[]>([itemFixture]);
  // loading 保存测试中的加载指示器。
  const [, setLoading] = useState(false);
  // loadAutomationRules 是自动化规则刷新替身。
  const loadAutomationRules = vi.fn(async () => undefined);
  // loadReferenceData 是规则参考数据刷新替身。
  const loadReferenceData = vi.fn(async () => undefined);
  // loadReplyRules 是关键词回复刷新替身。
  const loadReplyRules = vi.fn(async () => undefined);
  // loadDefaultReplies 是默认回复刷新替身。
  const loadDefaultReplies = vi.fn(async () => undefined);
  // options 是规则动作 Hook 的完整依赖。
  const options: RuleActionsOptions = { selectedAccountId, setSelectedAccountId, setActiveTab, items, setAutomationRules, setCards, setItems: /* setItemsAction 写入外部联动商品列表。 */ linkedItems => setItems(linkedItems), setLoading, loadAutomationRules, loadReferenceData, loadReplyRules, loadDefaultReplies };
  return useRuleActions(options);
};

describe('useRuleActions', /* 当前回调验证规则页面动作协调器的核心状态和副作用。 */ () => {
  beforeEach(/* 当前回调重置规则动作 API 替身。 */ () => {
    vi.clearAllMocks();
    defaultReplyMock.mockResolvedValue(defaultReplyFixture);
    updateDefaultMock.mockResolvedValue({ success: true });
    updateReplyMock.mockResolvedValue({ success: true });
    updateShippingMock.mockResolvedValue({ success: true });
    deleteShippingMock.mockResolvedValue({ success: true });
    resolveRunMock.mockResolvedValue({ success: true });
    resolveDeferredMock.mockResolvedValue({ success: true });
    deleteReplyMock.mockResolvedValue({ success: true });
    deleteDefaultMock.mockResolvedValue({ success: true });
    clearRecordsMock.mockResolvedValue({ success: true });
    vi.stubGlobal('alert', vi.fn());
    vi.stubGlobal('confirm', vi.fn(() => true));
  });

  afterEach(/* 当前回调清理规则动作测试中的全局替身。 */ () => vi.unstubAllGlobals());

  test('创建并保存自动化规则时归一化规格和刷新数据', /* 当前回调验证自动化规则草稿与保存边界。 */ async () => {
    // hook 是规则动作 Hook 的真实 React 状态实例。
    const hook = renderHook(() => useRuleActionsHarness());
    act(/* 当前回调打开新建自动化规则弹窗。 */ () => hook.result.current.openNewAutomationRule());
    expect(hook.result.current.showAutomationModal).toBe(true);
    act(/* 当前回调写入需要保存的卡密规格。 */ () => hook.result.current.setEditingAutomationRule(/* currentDraft 更新自动化规则草稿。 */ current => ({ ...current, cookie_id: 'account-1', variants: [{ id: 'variant-1', spec_name: '', spec_value: '', card_id: 7, delivery_count: 0, enabled: true }] })));
    await act(/* 当前回调执行自动化规则保存。 */ async () => hook.result.current.handleSaveAutomationRule());
    expect(updateShippingMock).toHaveBeenCalledWith(expect.objectContaining({ cookie_id: 'account-1', variants: [expect.objectContaining({ card_id: 7, delivery_count: 1 })] }));
    expect(hook.result.current.showAutomationModal).toBe(false);
    hook.unmount();
  });

  test('关键词和默认回复动作分别读取、保存并刷新', /* 当前回调验证两个回复页签的动作边界。 */ async () => {
    // hook 是规则动作 Hook 的真实 React 状态实例。
    const hook = renderHook(() => useRuleActionsHarness());
    act(/* 当前回调打开关键词新增弹窗。 */ () => hook.result.current.handleAddReplyRule());
    act(/* 当前回调填写关键词回复草稿。 */ () => hook.result.current.setEditingReplyRule(/* currentDraft 更新关键词回复草稿。 */ current => ({ ...current, keyword: '你好', reply_content: '您好' })));
    await act(/* 当前回调保存关键词回复规则。 */ async () => hook.result.current.handleSaveReplyRule());
    expect(updateReplyMock).toHaveBeenCalledWith(expect.objectContaining({ keyword: '你好', match_type: 'fuzzy', enabled: true }), 'account-1');

    await act(/* 当前回调打开默认回复弹窗并加载服务端数据。 */ async () => hook.result.current.openDefaultReplyModal());
    expect(hook.result.current.defaultForm).toEqual(expect.objectContaining({ reply_content: '欢迎', enabled: true }));
    await act(/* 当前回调保存默认回复配置。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateDefaultMock).toHaveBeenCalledWith('account-1', expect.objectContaining({ reply_content: '欢迎', enabled: true }));
    hook.unmount();
  });

  test('规则编辑、异常恢复和删除动作均通过统一协调器', /* 当前回调覆盖规则动作 Hook 的剩余公开方法。 */ async () => {
    // hook 是规则动作 Hook 的真实 React 状态实例。
    const hook = renderHook(() => useRuleActionsHarness());
    act(/* 当前回调打开自动化规则草稿。 */ () => hook.result.current.openNewAutomationRule());
    act(/* 当前回调切换规则触发类型。 */ () => hook.result.current.handleTriggerChange('buyer_reviewed'));
    act(/* 当前回调更新第一行规格。 */ () => hook.result.current.updateVariant(0, { card_id: 7 }));
    act(/* 当前回调追加第二行规格。 */ () => hook.result.current.appendDeliveryContent());
    expect(hook.result.current.displayVariants).toHaveLength(2);
    act(/* 当前回调切换规则绑定商品。 */ () => hook.result.current.handleAutomationItemChange('item-1'));

    // rule 是用于切换和删除动作的自动化规则样本。
    const rule = { id: 'rule-1', cookie_id: 'account-1', trigger_type: 'order_paid', enabled: true } as ShippingRule;
    await act(/* 当前回调切换规则启用状态。 */ async () => hook.result.current.handleToggleAutomation(rule));
    await act(/* 当前回调删除自动化规则。 */ async () => hook.result.current.handleDeleteAutomation('rule-1'));
    await act(/* 当前回调恢复暂停中的自动化运行。 */ async () => hook.result.current.handleResolveRunIssue(1, 'retry'));
    await act(/* 当前回调恢复延迟自动化任务。 */ async () => hook.result.current.handleResolveDeferredIssue(2, 'dismiss'));
    await act(/* 当前回调删除关键词回复规则。 */ async () => hook.result.current.handleDeleteReply('reply-1'));
    await act(/* 当前回调删除默认回复配置。 */ async () => hook.result.current.handleDeleteDefaultReply('account-1'));
    await act(/* 当前回调清空默认回复记录。 */ async () => hook.result.current.handleClearDefaultReplyRecords('account-1'));

    expect(updateShippingMock).toHaveBeenCalledWith(expect.objectContaining({ id: 'rule-1', enabled: false }));
    expect(deleteShippingMock).toHaveBeenCalledWith('rule-1');
    expect(resolveRunMock).toHaveBeenCalledWith(1, 'retry');
    expect(resolveDeferredMock).toHaveBeenCalledWith(2, 'dismiss');
    expect(deleteReplyMock).toHaveBeenCalledWith('reply-1', 'account-1');
    expect(deleteDefaultMock).toHaveBeenCalledWith('account-1');
    expect(clearRecordsMock).toHaveBeenCalledWith('account-1');
    hook.unmount();
  });
});
