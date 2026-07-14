import { expect, test } from 'vitest';
import type { AutomationRunIssue } from '../services/api';
import { canResolveAutomationIssue, filterAutomationIssues, loadAutomationPageData } from './automationIssueState';

const issue = (cookieID: string, allowed: AutomationRunIssue['allowed_resolutions']): AutomationRunIssue => ({
  id: cookieID === 'a' ? 1 : 2,
  cookie_id: cookieID,
  order_id: 'o',
  trigger_type: 'buyer_reviewed',
  error_message: 'error',
  issue_kind: 'invalid_snapshot',
  allowed_resolutions: allowed,
  action_cursor: 0,
  sent_count: 0,
  updated_at: '',
});

test('automation issue actions follow the backend policy', () => {
  const invalid = issue('a', ['cancel']);
  expect(canResolveAutomationIssue(invalid, 'cancel')).toBe(true);
  expect(canResolveAutomationIssue(invalid, 'continue')).toBe(false);
  expect(canResolveAutomationIssue(invalid, 'retry')).toBe(false);
});

test('automation issues are filtered before deciding whether to show the panel', () => {
  const visible = filterAutomationIssues({ runs: [issue('a', ['cancel']), issue('b', ['retry', 'cancel'])], pending_tasks: [] }, 'missing');
  expect(visible.runs).toEqual([]);
  expect(visible.pending_tasks).toEqual([]);
});

test('automation rules still load when the diagnostic issue request fails', async () => {
  const receivedRules: string[][] = [];
  const errors: unknown[] = [];
  await loadAutomationPageData({
    loadRules: async () => ['rule'],
    loadIssues: async () => { throw new Error('issues unavailable'); },
    onRules: rules => receivedRules.push(rules),
    onIssues: () => { throw new Error('must not receive issues'); },
    onIssuesError: error => errors.push(error),
  });
  expect(receivedRules).toEqual([['rule']]);
  expect(errors).toHaveLength(1);
});
