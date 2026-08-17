import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const source = (path: string) => readFileSync(resolve(__dirname, path), 'utf8'); /* source 表示source。 */

describe('responsive rules layout', () => {
  test('allows the rules page to shrink inside the sidebar layout', () => {
    const app = source('app/shell/AuthenticatedShell.tsx'); /* app 表示认证后应用壳源码。 */
    const rules = source('app/features/rules/pages/Rules.tsx'); /* rules 表示规则集合。 */
    expect(app).toContain('h-screen min-w-0 flex-1 overflow-x-hidden');
    expect(rules).toContain('min-w-0 space-y-8');
    expect(rules).toContain('xl:grid-cols-[minmax(270px,0.72fr)_minmax(0,1.28fr)]');
    expect(rules).not.toContain('2xl:grid-cols-[360px_1fr]');
  } /* 回调函数负责当前业务流程。 */);
} /* 回调函数负责当前业务流程。 */);

describe('rules summary counts', () => {
  test('uses server-side aggregate counts instead of the current page length', () => {
    const rules = source('app/features/rules/pages/Rules.tsx'); /* rules 表示规则集合。 */
    const api = source('app/features/rules/api.ts'); /* api 表示自动化规则 feature 的接口适配器源码。 */
    expect(rules).toContain('automationTriggerCounts');
    expect(rules).toContain('{automationTriggerCounts[trigger] || 0}');
    expect(rules).toContain('筛选结果构成');
    expect(rules).not.toContain('rulesByTrigger[trigger].length');
    expect(api).toContain('trigger_counts');
  } /* 回调函数负责当前业务流程。 */);
} /* 回调函数负责当前业务流程。 */);
