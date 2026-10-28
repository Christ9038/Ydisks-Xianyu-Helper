import { describe,expect,test } from 'vitest';
import { auditActionLabel,auditEventTypeLabel,auditSourceLabel,auditStatusMeta,auditStatusOptions,auditTriggerLabel,auditTriggerOptions } from './auditState';

describe('auditState', /* 当前回调验证规则日志的状态与文案映射。 */ () => {
  // 用例：已知状态返回中文名与样式，未知状态原样显示。
  test('状态映射覆盖全部状态并兜底未知值', () => {
    for (const option /* option 是当前遍历的状态选项。 */ of auditStatusOptions) {
      expect(auditStatusMeta(option.value).label).toBe(option.label);
      expect(auditStatusMeta(option.value).badgeClass).not.toBe('');
    }
    expect(auditStatusOptions.map(/* option 是一个状态选项。 */ option => option.value)).toEqual(['success', 'failed', 'queued', 'running', 'skipped', 'needs_review', 'canceled']);
    expect(auditStatusMeta('future_status').label).toBe('future_status');
    expect(auditStatusMeta('').label).toBe('未知');
  });

  // 用例：行为、触发、动作和来源文案命中映射，未知值原样返回。
  test('行为、触发、动作和来源文案命中映射并兜底', /* 当前回调断言各类文案映射。 */ () => {
    expect(auditEventTypeLabel('run_finished')).toBe('运行结束');
    expect(auditEventTypeLabel('custom')).toBe('custom');
    expect(auditTriggerLabel('order_paid')).toBe('买家付款');
    expect(auditTriggerLabel('custom')).toBe('custom');
    expect(auditTriggerOptions.some(/* option 是一个触发类型选项。 */ option => option.value === 'order_paid')).toBe(true);
    expect(auditActionLabel('send_card')).toBe('发卡');
    expect(auditActionLabel('')).toBe('');
    expect(auditSourceLabel('ws')).toBe('实时消息');
    expect(auditSourceLabel('custom')).toBe('custom');
  });
});
