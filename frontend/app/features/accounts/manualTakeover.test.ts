import { describe,expect,test } from 'vitest';
import { buildManualTakeoverUpdate,MANUAL_TAKEOVER_MAX_MINUTES,MANUAL_TAKEOVER_SCOPES,manualTakeoverFormError,normalizeManualTakeoverMinutes,normalizeManualTakeoverScope } from './manualTakeover';

describe('manualTakeover', /* 当前回调验证人工介入暂停设置的归一、校验与补丁生成。 */ () => {
  test('缺失或未知值归一为关闭与只暂停 AI', /* 当前回调验证旧后端与异常值兼容。 */ () => {
    expect(normalizeManualTakeoverScope(undefined)).toBe('ai');
    expect(normalizeManualTakeoverScope('garbage')).toBe('ai');
    expect(normalizeManualTakeoverScope('all')).toBe('all');
    expect(normalizeManualTakeoverMinutes(undefined)).toBe(0);
    expect(normalizeManualTakeoverMinutes('abc')).toBe(0);
    expect(normalizeManualTakeoverMinutes(-3)).toBe(0);
    expect(normalizeManualTakeoverMinutes(12.7)).toBe(12);
    expect(normalizeManualTakeoverMinutes(99999)).toBe(MANUAL_TAKEOVER_MAX_MINUTES);
    expect(MANUAL_TAKEOVER_SCOPES.map(/* option 是界面选项，只取值比较顺序。 */ option => option.value)).toEqual(['ai', 'all']);
  });

  test('分钟数越界或非整数时给出错误', /* 当前回调验证提交前校验与后端规则一致。 */ () => {
    expect(manualTakeoverFormError({ manual_takeover_minutes: 0, manual_takeover_scope: 'ai' })).toBe('');
    expect(manualTakeoverFormError({ manual_takeover_minutes: MANUAL_TAKEOVER_MAX_MINUTES, manual_takeover_scope: 'all' })).toBe('');
    expect(manualTakeoverFormError({ manual_takeover_minutes: -1, manual_takeover_scope: 'ai' })).toContain('0 到');
    expect(manualTakeoverFormError({ manual_takeover_minutes: MANUAL_TAKEOVER_MAX_MINUTES + 1, manual_takeover_scope: 'ai' })).toContain('0 到');
    expect(manualTakeoverFormError({ manual_takeover_minutes: 1.5, manual_takeover_scope: 'ai' })).toContain('整数');
    expect(manualTakeoverFormError({ manual_takeover_minutes: Number.NaN, manual_takeover_scope: 'ai' })).toContain('整数');
  });

  test('只提交变化的字段', /* 当前回调验证最小补丁语义。 */ () => {
    expect(buildManualTakeoverUpdate({}, { manual_takeover_minutes: 0, manual_takeover_scope: 'ai' })).toBeNull();
    expect(buildManualTakeoverUpdate({ manual_takeover_minutes: 10, manual_takeover_scope: 'all' }, { manual_takeover_minutes: 10, manual_takeover_scope: 'all' })).toBeNull();
    expect(buildManualTakeoverUpdate({}, { manual_takeover_minutes: 10, manual_takeover_scope: 'ai' })).toEqual({ manual_takeover_minutes: 10 });
    expect(buildManualTakeoverUpdate({ manual_takeover_minutes: 10 }, { manual_takeover_minutes: 10, manual_takeover_scope: 'all' })).toEqual({ manual_takeover_scope: 'all' });
    expect(buildManualTakeoverUpdate({ manual_takeover_minutes: 5, manual_takeover_scope: 'all' }, { manual_takeover_minutes: 0, manual_takeover_scope: 'ai' })).toEqual({ manual_takeover_minutes: 0, manual_takeover_scope: 'ai' });
  });
});
