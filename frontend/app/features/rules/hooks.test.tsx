// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { AccountDetail, Card, DefaultReply, Item, ReplyRule, ShippingRule } from '../../../shared/api-contract';
import {
  getAccountDetails,
  getAutomationIssues,
  getCards,
  getDefaultReplies,
  getItems,
  getReplyRules,
  getShippingRulesPage,
} from './api';
import { useRulesData, useRulesDataWithPageChange } from './hooks';

vi.mock('./api', /* rulesApiMockFactory 提供规则数据 Hook 的确定性 API 替身。 */ () => ({
  getAccountDetails: vi.fn(),
  getAutomationIssues: vi.fn(),
  getCards: vi.fn(),
  getDefaultReplies: vi.fn(),
  getItems: vi.fn(),
  getReplyRules: vi.fn(),
  getShippingRulesPage: vi.fn(),
}));

// accountsMock 是账号参考数据请求的可控替身。
const accountsMock = vi.mocked(getAccountDetails);
// issuesMock 是自动化异常请求的可控替身。
const issuesMock = vi.mocked(getAutomationIssues);
// cardsMock 是卡密参考数据请求的可控替身。
const cardsMock = vi.mocked(getCards);
// defaultsMock 是默认回复请求的可控替身。
const defaultsMock = vi.mocked(getDefaultReplies);
// itemsMock 是商品参考数据请求的可控替身。
const itemsMock = vi.mocked(getItems);
// repliesMock 是关键词回复规则请求的可控替身。
const repliesMock = vi.mocked(getReplyRules);
// shippingPageMock 是自动化规则分页请求的可控替身。
const shippingPageMock = vi.mocked(getShippingRulesPage);

// accountFixture 是规则筛选使用的账号摘要。
const accountFixture = { id: 'account-1', enabled: true, auto_confirm: false, nickname: '账号一' } as AccountDetail;
// cardFixture 是动作配置使用的卡密摘要。
const cardFixture = { id: 1, group_id: 2, content: '卡密内容', status: 'available' } as never as Card;
// itemFixture 是规则编辑器使用的商品摘要。
const itemFixture = { id: 'item-1', cookie_id: 'account-1', title: '测试商品' } as never as Item;
// defaultReplyFixture 是账号默认回复配置。
const defaultReplyFixture = { cookie_id: 'account-1', enabled: true, reply_content: '欢迎', reply_once: false, reply_image_url: '' } as DefaultReply;
// replyFixture 是关键词回复规则。
const replyFixture = { id: 'reply-1', keyword: '你好', reply_content: '您好', match_type: 'fuzzy', enabled: true, item_id: '', type: 'text', image_url: '' } as ReplyRule;
// shippingFixture 是自动化规则分页中的一条规则。
const shippingFixture = { id: 1, cookie_id: 'account-1', name: '付款发货', trigger_type: 'order_paid', enabled: true, actions: [], variants: [], config_json: '{}' } as never as ShippingRule;

describe('useRulesData', /* 当前回调处理规则页参考数据、分页和页签刷新。 */ () => {
  beforeEach(/* 当前回调重置规则 API 替身为成功默认值。 */ () => {
    vi.clearAllMocks();
    accountsMock.mockResolvedValue([accountFixture]);
    cardsMock.mockResolvedValue([cardFixture]);
    itemsMock.mockResolvedValue([itemFixture]);
    defaultsMock.mockResolvedValue({ 'account-1': defaultReplyFixture });
    issuesMock.mockResolvedValue({ runs: [], pending_tasks: [] });
    repliesMock.mockResolvedValue([replyFixture]);
    shippingPageMock.mockResolvedValue({ success: true, data: [shippingFixture], total: 1, page: 1, page_size: 10, total_pages: 1, trigger_counts: { order_paid: 1 } });
  });

  test('加载参考数据、自动化规则并处理服务端页码修正', /* 当前回调验证自动化页的主要成功路径。 */ async () => {
    // setSelectedAccountId 是参考数据加载后回填首个账号的状态替身。
    const setSelectedAccountId = vi.fn();
    // onPageChange 接收服务端修正后的页码。
    const onPageChange = vi.fn();
    // options 是自动化页的固定筛选条件。
    const options = { activeTab: 'automation' as const, selectedAccountId: 'account-1', automationTriggerFilter: '' as const, automationStatusFilter: 'all' as const, debouncedAutomationSearch: '测试', automationPage: 2, automationPageSize: 10, setSelectedAccountId, onAutomationPageChange: onPageChange };
    shippingPageMock.mockResolvedValueOnce({ success: true, data: [shippingFixture], total: 1, page: 1, page_size: 10, total_pages: 1, trigger_counts: { order_paid: 1 } });
    // hook 是规则数据 Hook 的渲染结果。
    const hook = renderHook(
      // automationHookFactory 创建自动化页规则 Hook。
      () => useRulesData(options),
    );
    await act(
      // referenceAction 加载规则编辑器参考数据。
      async () => hook.result.current.loadReferenceData(),
    );
    expect(hook.result.current.accounts).toEqual([accountFixture]);
    expect(hook.result.current.cards).toEqual([cardFixture]);
    expect(setSelectedAccountId).toHaveBeenCalled();
    // selectedAccountUpdater 是规则 Hook 回填账号时交给 React 的函数式更新器。
    const selectedAccountUpdater = setSelectedAccountId.mock.calls[0][0] as (current: string) => string;
    expect(selectedAccountUpdater('')).toBe('account-1');
    expect(selectedAccountUpdater('existing')).toBe('existing');
    await act(
      // automationAction 加载分页规则和异常。
      async () => hook.result.current.loadAutomationRules(),
    );
    expect(shippingPageMock).toHaveBeenCalledWith(expect.objectContaining({ cookieId: 'account-1', page: 2, search: '测试' }));
    expect(hook.result.current.automationRules).toEqual([shippingFixture]);
    expect(hook.result.current.automationTotalPages).toBe(1);
    expect(onPageChange).toHaveBeenCalledWith(1);
    hook.unmount();
  });

  test('异常请求失败不阻断规则展示，关键词和默认回复按页签刷新', /* 当前回调验证异常隔离和三个页签刷新分支。 */ async () => {
    // setSelectedAccountId 是页签刷新所需的账号状态替身。
    const setSelectedAccountId = vi.fn();
    // activeTab 是当前规则页签，可在测试中切换。
    let activeTab: 'automation' | 'reply' | 'default' = 'automation';
    // optionsFactory 根据当前页签生成 Hook 参数。
    const optionsFactory = () => ({ activeTab, selectedAccountId: 'account-1', automationTriggerFilter: 'order_paid' as const, automationStatusFilter: 'enabled' as const, debouncedAutomationSearch: '', automationPage: 1, automationPageSize: 5, setSelectedAccountId });
    issuesMock.mockRejectedValueOnce(new Error('异常接口不可用'));
    // hook 是多页签规则 Hook 的渲染结果。
    const hook = renderHook(
      // multiTabHookFactory 创建可切换页签的规则 Hook。
      () => useRulesData(optionsFactory()),
    );
    await act(
      // automationRefresh 刷新自动化页并隔离异常接口失败。
      async () => hook.result.current.refresh(),
    );
    expect(hook.result.current.automationRules).toEqual([shippingFixture]);
    expect(hook.result.current.automationIssues).toEqual({ runs: [], pending_tasks: [] });

    activeTab = 'reply';
    hook.rerender();
    await act(
      // replyRefresh 刷新关键词规则页签。
      async () => hook.result.current.refresh(),
    );
    expect(repliesMock).toHaveBeenCalledWith('account-1');
    expect(hook.result.current.replyRules).toEqual([replyFixture]);

    activeTab = 'default';
    hook.rerender();
    await act(
      // defaultRefresh 刷新默认回复页签。
      async () => hook.result.current.refresh(),
    );
    expect(defaultsMock).toHaveBeenCalled();
    expect(hook.result.current.defaultReplies).toEqual({ 'account-1': defaultReplyFixture });

    activeTab = 'reply';
    // emptyHook 是无账号筛选场景的规则 Hook 渲染结果。
    const emptyHook = renderHook(
      // emptyHookFactory 创建无账号筛选的规则 Hook。
      () => useRulesData({ ...optionsFactory(), selectedAccountId: '' }),
    );
    await act(
      // emptyReplyAction 验证没有账号时关键词列表直接清空。
      async () => emptyHook.result.current.loadReplyRules(),
    );
    expect(emptyHook.result.current.replyRules).toEqual([]);
    emptyHook.unmount();
    hook.unmount();
  });

  test('参考账号为空时清空当前账号选择', /* 当前回调验证参考数据无账号的默认选择分支。 */ async () => {
    // setSelectedAccountId 是空账号参考数据的选择状态替身。
    const setSelectedAccountId = vi.fn();
    accountsMock.mockResolvedValueOnce([]);
    // hook 是空账号参考数据场景的规则 Hook 渲染结果。
    const hook = renderHook(
      // emptyReferenceHookFactory 创建空账号参考数据的规则 Hook。
      () => useRulesData({ activeTab: 'default', selectedAccountId: '', automationTriggerFilter: '', automationStatusFilter: 'all', debouncedAutomationSearch: '', automationPage: 1, automationPageSize: 10, setSelectedAccountId }),
    );
    await act(
      // referenceAction 加载空账号参考数据。
      async () => hook.result.current.loadReferenceData(),
    );
    expect(setSelectedAccountId).toHaveBeenCalledWith(expect.any(Function));
    hook.unmount();
  });

  test('带分页回调的规则 Hook 入口保持数据契约', /* 当前回调验证分页兼容入口委托到统一规则 Hook。 */ async () => {
    // setSelectedAccountId 是分页兼容入口所需的账号状态替身。
    const setSelectedAccountId = vi.fn();
    // hook 是分页兼容入口的规则数据 Hook 渲染结果。
    const hook = renderHook(
      // pageChangeHookFactory 创建带分页回调的规则 Hook。
      () => useRulesDataWithPageChange({ activeTab: 'automation', selectedAccountId: 'account-1', automationTriggerFilter: '', automationStatusFilter: 'all', debouncedAutomationSearch: '', automationPage: 1, automationPageSize: 10, setSelectedAccountId }),
    );
    await act(
      // refreshAction 通过兼容入口刷新规则数据。
      async () => hook.result.current.refresh(),
    );
    expect(hook.result.current.automationRules).toEqual([shippingFixture]);
    hook.unmount();
  });
});
