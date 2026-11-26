import { describe,expect,test } from 'vitest';
import type { AutomationAction } from './models';
import { buildReviewActions,defaultReviewCopyPlan,parseReviewPlan,reviewTailDefaultText,summarizeReviewPlan,validateReviewPlan } from './reviewPlan';
import { buildReviewConfig,reviewRequestText } from './utils';

/** legacy 是历史只有一条文本的求评价规则动作。 */
const legacy: AutomationAction[] = [{ id: '7', action_type: 'send_text', message_template: reviewRequestText, enabled: true, sort_order: 1 }];

describe('reviewPlan', /* 当前回调验证求评价三段消息模型的解析、构建、校验与摘要。 */ () => {
  test('历史单文本规则解析为只启用引导语，且保存不丢内容', /* 当前回调验证向后兼容。 */ () => {
    // plan 是解析出的模型。
    const plan = parseReviewPlan(legacy);
    expect(plan.lead).toEqual({ enabled: true, text: reviewRequestText });
    expect(plan.copy.enabled).toBe(false);
    expect(plan.tail).toEqual({ enabled: false, text: reviewTailDefaultText });
    // saved 是保存时提交的动作：只有引导语，并保留原 ID。
    const saved = buildReviewActions(plan, legacy, true);
    expect(saved).toHaveLength(1);
    expect(saved[0]).toMatchObject({ id: '7', action_type: 'send_text', message_template: reviewRequestText, delay_seconds: 0 });
  });

  test('三段消息按启用位置计算延迟与随机上浮', /* 当前回调验证间隔换算。 */ () => {
    // plan 启用三段，间隔 5 到 12 秒。
    const plan = { ...parseReviewPlan(legacy), intervalMin: 5, intervalMax: 12 };
    plan.copy = { ...defaultReviewCopyPlan(), enabled: true, pool: ['好评 A', ' ', '好评 B'] };
    plan.tail = { enabled: true, text: ' 请复制上面的内容 ' };
    // saved 是保存提交的动作。
    const saved = buildReviewActions(plan, legacy, true);
    expect(saved.map(/* 当前回调取动作类型。 */ action => action.action_type)).toEqual(['send_text', 'send_review_copy', 'send_text']);
    expect(saved.map(/* 当前回调取延迟。 */ action => action.delay_seconds)).toEqual([0, 5, 5]);
    expect(JSON.parse(saved[1].config_json || '{}')).toMatchObject({ source: 'pool', pool: ['好评 A', '好评 B'], delay_jitter_seconds: 7 });
    expect(JSON.parse(saved[2].config_json || '{}')).toEqual({ delay_jitter_seconds: 7 });
    expect(saved[2].message_template).toBe('请复制上面的内容');
    expect(saved.map(/* 当前回调取排序。 */ action => action.sort_order)).toEqual([1, 2, 3]);
  });

  test('停用引导语后好评文案成为第一条并立即发送', /* 当前回调验证首条无延迟。 */ () => {
    // plan 只启用好评文案和复制提示。
    const plan = parseReviewPlan(legacy);
    plan.lead.enabled = false;
    plan.copy = { ...defaultReviewCopyPlan(), enabled: true, source: 'ai' };
    plan.tail.enabled = true;
    // saved 是保存提交的动作。
    const saved = buildReviewActions(plan, legacy, true);
    expect(saved.map(/* 当前回调取动作类型。 */ action => action.action_type)).toEqual(['send_review_copy', 'send_text']);
    expect(saved[0].delay_seconds).toBe(0);
    expect(saved[1].delay_seconds).toBe(plan.intervalMin);
  });

  test('编辑中保留停用动作与空白模板，保存时清理', /* 当前回调验证草稿不丢内容。 */ () => {
    // plan 含空白模板且复制提示停用。
    const plan = parseReviewPlan(legacy);
    plan.copy = { ...defaultReviewCopyPlan(), enabled: true, pool: ['', 'x'] };
    // draft 是编辑中的动作列表，三段都在。
    const draft = buildReviewActions(plan, legacy);
    expect(draft).toHaveLength(3);
    expect(draft[2].enabled).toBe(false);
    expect(JSON.parse(draft[1].config_json || '{}').pool).toEqual(['', 'x']);
    // 往返解析保持一致。
    expect(parseReviewPlan(draft).copy.pool).toEqual(['', 'x']);
  });

  test('服务端省略停用动作后仍能解析引导语与复制提示', /* 当前回调验证无文案动作时的第二条文本。 */ () => {
    // actions 模拟停用好评文案后保存的两条文本。
    const actions: AutomationAction[] = [
      { action_type: 'send_text', message_template: '引导', enabled: true, delay_seconds: 0 },
      { action_type: 'send_text', message_template: '提示', enabled: true, delay_seconds: 8, config_json: '{"delay_jitter_seconds":4}' },
    ];
    // plan 是解析结果。
    const plan = parseReviewPlan(actions);
    expect(plan.lead.text).toBe('引导');
    expect(plan.tail).toEqual({ enabled: true, text: '提示' });
    expect([plan.intervalMin, plan.intervalMax]).toEqual([8, 12]);
  });

  test('解析容错：非法配置回退默认值', /* 当前回调验证坏数据不抛错。 */ () => {
    // plan 来自损坏的配置。
    const plan = parseReviewPlan([{ action_type: 'send_review_copy', enabled: true, config_json: '[bad', delay_seconds: 0 }]);
    expect(plan.copy).toMatchObject({ enabled: true, source: 'pool', random: true, aiStyle: 'sincere' });
    // arr 是非对象 JSON。
    expect(parseReviewPlan([{ action_type: 'send_review_copy', enabled: true, config_json: '[]' }]).copy.source).toBe('pool');
    // full 是各字段齐全的配置。
    const full = parseReviewPlan([{ action_type: 'send_review_copy', enabled: true, config_json: JSON.stringify({ source: 'mixed', random: false, ai_ratio: 40, ai_style: 'warm', ai_min_chars: 10, ai_max_chars: 60, ai_prompt: 'p', pool: ['a'] }) }]);
    expect(full.copy).toMatchObject({ source: 'mixed', random: false, aiRatio: 40, aiStyle: 'warm', aiMinChars: 10, aiMaxChars: 60, aiPrompt: 'p', pool: ['a'] });
    expect(parseReviewPlan(undefined).lead.enabled).toBe(false);
  });

  test('校验覆盖每一类错误', /* 当前回调验证用户可见的错误文案。 */ () => {
    // base 是通过校验的基础模型。
    const base = parseReviewPlan(legacy);
    expect(validateReviewPlan(base)).toBe('');
    expect(validateReviewPlan({ ...base, lead: { enabled: false, text: '' } })).toContain('至少启用');
    expect(validateReviewPlan({ ...base, lead: { enabled: true, text: ' ' } })).toContain('引导语');
    expect(validateReviewPlan({ ...base, tail: { enabled: true, text: '' } })).toContain('复制提示');
    expect(validateReviewPlan({ ...base, intervalMin: 9, intervalMax: 3 })).toContain('不能小于');
    expect(validateReviewPlan({ ...base, intervalMax: 601 })).toContain('600');
    // withCopy 构造启用好评文案的模型。
    const withCopy = (patch: object) => ({ ...base, copy: { ...defaultReviewCopyPlan(), enabled: true, ...patch } });
    expect(validateReviewPlan(withCopy({ pool: [] }))).toContain('至少添加');
    expect(validateReviewPlan(withCopy({ source: 'mixed', pool: [] }))).toContain('兜底');
    expect(validateReviewPlan(withCopy({ pool: Array.from({ length: 101 }, /* 当前回调生成模板。 */ (_, index) => `t${index}`) }))).toContain('100');
    expect(validateReviewPlan(withCopy({ source: 'mixed', pool: ['a'], aiRatio: 101 }))).toContain('比例');
    expect(validateReviewPlan(withCopy({ source: 'ai', aiMinChars: 2 }))).toContain('字数');
    expect(validateReviewPlan(withCopy({ source: 'ai', aiMinChars: 60, aiMaxChars: 20 }))).toContain('字数');
    expect(validateReviewPlan(withCopy({ source: 'ai' }))).toBe('');
    expect(validateReviewPlan(withCopy({ pool: ['ok'] }))).toBe('');
  });

  test('摘要描述启用的消息与来源', /* 当前回调验证列表摘要。 */ () => {
    expect(summarizeReviewPlan(legacy)).toBe(reviewRequestText);
    expect(summarizeReviewPlan(undefined)).toBe('发送求评价文案');
    // actions 是三段规则。
    const plan = parseReviewPlan(legacy);
    plan.copy = { ...defaultReviewCopyPlan(), enabled: true, pool: ['a', ' '] };
    plan.tail.enabled = true;
    expect(summarizeReviewPlan(buildReviewActions(plan, legacy))).toBe('引导语 → 好评文案（模板池 1 条） → 复制提示');
    plan.copy.source = 'ai';
    plan.lead.enabled = false;
    plan.tail.enabled = false;
    expect(summarizeReviewPlan(buildReviewActions(plan, legacy))).toBe('好评文案（AI 生成）');
    plan.copy.source = 'mixed';
    expect(summarizeReviewPlan(buildReviewActions(plan, legacy))).toContain('模板池 + AI');
  });

  test('求评价配置保留计时起点', /* 当前回调验证 basis 往返。 */ () => {
    expect(JSON.parse(buildReviewConfig('{}'))).not.toHaveProperty('basis');
    expect(JSON.parse(buildReviewConfig('{}', { basis: 'completed' })).basis).toBe('completed');
    expect(JSON.parse(buildReviewConfig('{"basis":"completed"}', { after_shipped_hours: 5 }))).toMatchObject({ basis: 'completed', after_shipped_hours: 5 });
    expect(JSON.parse(buildReviewConfig('{"basis":"completed"}', { basis: 'shipped' })).basis).toBe('shipped');
  });
});
