// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import Rules from './Rules';
import { useRulesData } from '../app/features/rules/hooks';

vi.mock('../app/features/rules/hooks', /* rulesHookMockFactory 提供规则页面的可控数据状态。 */ () => ({ useRulesData: vi.fn() }));

// useRulesDataMock 是规则页面 Hook 的可控替身。
const useRulesDataMock = vi.mocked(useRulesData);
// rulesPageState 是规则页面初始渲染所需的最小数据集合。
const rulesPageState = {
  automationRules: [], automationIssues: { runs: [], pending_tasks: [] }, replyRules: [], defaultReplies: {}, accounts: [], cards: [], items: [], automationTotal: 0, automationTotalPages: 0, automationTriggerCounts: {}, loading: false, setLoading: vi.fn(), setAutomationRules: vi.fn(), setCards: vi.fn(), setItems: vi.fn(), loadReferenceData: vi.fn().mockResolvedValue(undefined), loadAutomationRules: vi.fn().mockResolvedValue(undefined), loadReplyRules: vi.fn().mockResolvedValue(undefined), loadDefaultReplies: vi.fn().mockResolvedValue(undefined), refresh: vi.fn().mockResolvedValue(undefined),
};

describe('Rules 页面基础交互', /* 当前回调覆盖规则页面标题、页签和无账号状态。 */ () => {
  test('可以切换自动化、关键词和默认回复页签', /* 当前回调验证规则页面的页签边界交互。 */ () => {
    useRulesDataMock.mockReturnValue(rulesPageState as never);
    render(<Rules />);
    expect(screen.getByText('自动化规则')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '关键词回复' }));
    expect(screen.getByText('暂无关键词回复规则')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '账号默认回复' }));
    expect(screen.getByText('账号默认回复')).toBeTruthy();
    expect(screen.getByRole('button', { name: '刷新' })).toBeTruthy();
  });
});
