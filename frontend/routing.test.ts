import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const repoRoot = resolve(__dirname);

const readFrontendFile = (relativePath: string) =>
  readFileSync(resolve(repoRoot, relativePath), 'utf8');

const extractSingleQuotedValues = (source: string, pattern: RegExp) => {
  const values = new Set<string>();
  for (const match of source.matchAll(pattern)) {
    values.add(match[1]);
  }
  return values;
};

describe('frontend navigation routing', () => {
  test('sidebar entries and App activeTab routes stay in sync', () => {
    const sidebar = readFrontendFile('components/Sidebar.tsx');
    const app = readFrontendFile('App.tsx');

    const sidebarIDs = extractSingleQuotedValues(sidebar, /id:\s*'([^']+)'/g);
    const appRouteIDs = extractSingleQuotedValues(app, /case\s+'([^']+)'/g);

    expect([...sidebarIDs].sort()).toEqual([...appRouteIDs].sort());
  });

  test('item delivery shortcut opens existing automation rule modal', () => {
    const rules = readFrontendFile('components/Rules.tsx');
    const existingRuleBranch = rules.match(/if \(rule\) \{([\s\S]*?)\} else \{/);

    expect(existingRuleBranch?.[1]).toContain('openAutomationRule(rule)');
  });

  test('item delivery shortcut is not marked handled before async open completes', () => {
    const rules = readFrontendFile('components/Rules.tsx');

    expect(rules).not.toContain('handledDeliveryTarget.current = initialDeliveryTarget.requestId');
    expect(rules).toContain('onDeliveryTargetHandled?.();');
  });

  test('automation editor keeps multiple delivery contents for normal products', () => {
    const rules = readFrontendFile('components/Rules.tsx');

    expect(rules).toContain('添加发货内容');
    expect(rules).toContain('{displayVariants.map((variant, index) => (');
    expect(rules).toContain(': variants.map(variant => ({');
    expect(rules).not.toContain(': (isMultiSpecRule ? variants : [variants[0]]).map');
  });

  test('batch publishing help explains card fields without required-field jargon', () => {
    const itemList = readFrontendFile('components/ItemList.tsx');

    expect(itemList).not.toContain('条件必填');
    expect(itemList).toContain('“付款后发送的卡密”怎么填');
    expect(itemList).toContain('101:1:0;102:2:3');
    expect(itemList).toContain('买家购买 3 件时会发送 6 份');
  });
});
