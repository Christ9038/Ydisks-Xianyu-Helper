import type { AutomationAction } from './models';

/** 好评文案来源：固定模板池、AI 生成或按比例混合。 */
export type ReviewCopySource = 'pool' | 'ai' | 'mixed';

/** AI 评价风格，与后端 ai_style 取值一致。 */
export type ReviewCopyStyle = 'sincere' | 'warm' | 'concise';

/** 好评文案动作的可编辑配置。 */
export interface ReviewCopyPlan {
  /** 是否启用好评文案这一条消息。 */
  enabled: boolean;
  /** 文案来源。 */
  source: ReviewCopySource;
  /** 固定模板池，编辑中允许出现空白条目，保存时清理。 */
  pool: string[];
  /** 模板池是否随机选取；false 时按规则运行次数顺序循环。 */
  random: boolean;
  /** 混合模式下使用 AI 的概率百分比，范围 0 到 100。 */
  aiRatio: number;
  /** AI 评价风格。 */
  aiStyle: ReviewCopyStyle;
  /** AI 文案最少字数。 */
  aiMinChars: number;
  /** AI 文案最多字数。 */
  aiMaxChars: number;
  /** 交给 AI 的附加要求，可为空。 */
  aiPrompt: string;
}

/** 一条普通文本消息（引导语或复制提示）的可编辑状态。 */
export interface ReviewTextPlan {
  /** 是否启用这条消息。 */
  enabled: boolean;
  /** 消息正文。 */
  text: string;
}

/** 求评价规则三段消息的完整编辑模型。 */
export interface ReviewPlan {
  /** 第一条：引导语。 */
  lead: ReviewTextPlan;
  /** 第二条：好评文案。 */
  copy: ReviewCopyPlan;
  /** 第三条：复制提示。 */
  tail: ReviewTextPlan;
  /** 相邻两条消息之间的最小间隔秒数。 */
  intervalMin: number;
  /** 相邻两条消息之间的最大间隔秒数。 */
  intervalMax: number;
}

/** reviewTailDefaultText 是复制提示的默认文案。 */
export const reviewTailDefaultText = '好评模板我帮你生成好啦，你直接按上方内容复制就行～';

/** reviewPoolLimit 是固定模板池的数量上限，与后端校验一致。 */
export const reviewPoolLimit = 100;

/** reviewIntervalLimit 是随机间隔的秒数上限，与后端 delay_jitter_seconds 上限一致。 */
export const reviewIntervalLimit = 600;

/** defaultReviewCopyPlan 返回好评文案的默认配置：停用，来源为随机模板池。 */
export const defaultReviewCopyPlan = (): ReviewCopyPlan => ({
  enabled: false,
  source: 'pool',
  pool: [],
  random: true,
  aiRatio: 30,
  aiStyle: 'sincere',
  aiMinChars: 20,
  aiMaxChars: 50,
  aiPrompt: '',
});

/** parseActionConfig 把动作 config_json 解析为对象，非法或非对象时返回空对象。 */
const parseActionConfig = (raw?: string): Record<string, unknown> => {
  try {
    // value 是解析后的 JSON 值。
    const value = JSON.parse(raw || '{}');
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
  } catch {
    return {};
  }
};

/** numberOr 把未知值转成有限数字，无法转换时返回回退值。 */
const numberOr = (value: unknown, fallback: number): number => {
  // parsed 是转换后的数字。
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

/** locateReviewActions 返回引导语、好评文案、复制提示在动作列表中的下标，未找到为 -1。 */
const locateReviewActions = (actions: AutomationAction[]) => {
  // copyIndex 是好评文案动作的位置。
  const copyIndex = actions.findIndex(/* 当前回调查找好评文案动作。 */ action => action.action_type === 'send_review_copy');
  // textIndexes 是所有文本动作的位置。
  const textIndexes = actions.flatMap(/* 当前回调收集文本动作下标。 */ (action, index) => action.action_type === 'send_text' ? [index] : []);
  if (copyIndex < 0) {
    // 没有好评文案动作：第一条文本视为引导语；停用好评文案后保存的“引导语 + 复制提示”第二条文本视为复制提示。
    return { leadIndex: textIndexes[0] ?? -1, copyIndex, tailIndex: textIndexes[1] ?? -1 };
  }
  return {
    leadIndex: textIndexes.find(/* 当前回调取好评文案之前的第一条文本。 */ index => index < copyIndex) ?? -1,
    copyIndex,
    tailIndex: textIndexes.find(/* 当前回调取好评文案之后的第一条文本。 */ index => index > copyIndex) ?? -1,
  };
};

/** parseReviewPlan 把规则动作列表还原成三段消息的编辑模型；历史单文本规则解析为“只启用引导语”。 */
export const parseReviewPlan = (actions: AutomationAction[] | undefined): ReviewPlan => {
  // list 是规则当前动作，缺省为空。
  const list = actions || [];
  // positions 是三类动作的位置。
  const positions = locateReviewActions(list);
  // leadAction 是引导语动作，未找到为 undefined。
  const leadAction = positions.leadIndex >= 0 ? list[positions.leadIndex] : undefined;
  // copyAction 是好评文案动作，未找到为 undefined。
  const copyAction = positions.copyIndex >= 0 ? list[positions.copyIndex] : undefined;
  // tailAction 是复制提示动作，未找到为 undefined。
  const tailAction = positions.tailIndex >= 0 ? list[positions.tailIndex] : undefined;
  // defaults 是好评文案的默认配置，缺失字段由它补齐。
  const defaults = defaultReviewCopyPlan();
  // config 是好评文案动作的原始配置。
  const config = parseActionConfig(copyAction?.config_json);
  // source 是合法的文案来源，非法值回退为模板池。
  const source: ReviewCopySource = config.source === 'ai' || config.source === 'mixed' ? config.source : 'pool';
  // style 是合法的评价风格，非法值回退为真诚自然。
  const style: ReviewCopyStyle = config.ai_style === 'warm' || config.ai_style === 'concise' ? config.ai_style : 'sincere';
  // enabledActions 是启用的动作，用于还原相邻消息的间隔。
  const enabledActions = [leadAction, copyAction, tailAction].filter(/* 当前回调保留存在且启用的动作。 */ (action): action is AutomationAction => Boolean(action && action.enabled !== false));
  // second 是第二条启用的消息，它的延迟就是消息间隔。
  const second = enabledActions[1];
  // intervalMin 是还原出的最小间隔秒数。
  const intervalMin = second ? Math.max(0, numberOr(second.delay_seconds, 5)) : 5;
  // jitter 是还原出的随机上浮秒数。
  const jitter = second ? Math.max(0, numberOr(parseActionConfig(second.config_json).delay_jitter_seconds, 5)) : 5;
  return {
    lead: { enabled: leadAction ? leadAction.enabled !== false : false, text: leadAction?.message_template || '' },
    copy: {
      enabled: copyAction ? copyAction.enabled !== false : false,
      source,
      pool: Array.isArray(config.pool) ? config.pool.map(/* 当前回调把模板转成字符串。 */ item => String(item)) : defaults.pool,
      random: config.random === false ? false : true,
      aiRatio: numberOr(config.ai_ratio, defaults.aiRatio),
      aiStyle: style,
      aiMinChars: numberOr(config.ai_min_chars, defaults.aiMinChars),
      aiMaxChars: numberOr(config.ai_max_chars, defaults.aiMaxChars),
      aiPrompt: typeof config.ai_prompt === 'string' ? config.ai_prompt : '',
    },
    tail: { enabled: tailAction ? tailAction.enabled !== false : false, text: tailAction?.message_template ?? reviewTailDefaultText },
    intervalMin,
    intervalMax: intervalMin + jitter,
  };
};

/** serializeCopyConfig 把好评文案编辑状态写成动作 config_json；cleanPool 为 true 时丢弃空白模板并去除首尾空白。 */
const serializeCopyConfig = (copy: ReviewCopyPlan, jitter: number, cleanPool: boolean): string => {
  // pool 是写入的模板池。
  const pool = cleanPool ? copy.pool.map(/* 当前回调去除首尾空白。 */ item => item.trim()).filter(Boolean) : copy.pool;
  return JSON.stringify({
    source: copy.source,
    pool,
    random: copy.random,
    ai_ratio: copy.aiRatio,
    ai_style: copy.aiStyle,
    ai_min_chars: copy.aiMinChars,
    ai_max_chars: copy.aiMaxChars,
    ai_prompt: copy.aiPrompt,
    ...(jitter > 0 ? { delay_jitter_seconds: jitter } : {}),
  });
};

/**
 * buildReviewActions 把编辑模型写回动作列表：固定顺序为引导语、好评文案、复制提示，并保留原动作 ID。
 * 停用的消息以 enabled=false 保留在列表里，让用户切换开关时不丢内容；延迟按“启用消息的先后位置”计算，
 * 第一条启用消息立即发送，之后每条延迟 intervalMin 秒并随机上浮 0 到 (intervalMax-intervalMin) 秒。
 * cleanForSave 为 true 时用于保存：清理模板池并只保留启用的动作。
 */
export const buildReviewActions = (plan: ReviewPlan, existing: AutomationAction[] | undefined, cleanForSave = false): AutomationAction[] => {
  // positions 用于沿用原动作的 ID。
  const positions = locateReviewActions(existing || []);
  // ids 是三类动作原有的 ID。
  const ids = [positions.leadIndex, positions.copyIndex, positions.tailIndex].map(/* 当前回调取原动作 ID。 */ index => index >= 0 ? (existing || [])[index]?.id : undefined);
  // jitter 是相邻消息间隔的随机上浮秒数。
  const jitter = Math.max(0, plan.intervalMax - plan.intervalMin);
  // order 记录已经遇到的启用消息数量，用来决定该动作是不是第一条。
  let order = 0;
  // nextTiming 返回当前动作的延迟与抖动，并推进启用计数。
  const nextTiming = (enabled: boolean) => {
    if (!enabled) return { delay_seconds: 0, jitter: 0 };
    // isFirst 表示这是第一条启用的消息。
    const isFirst = order === 0;
    order += 1;
    return isFirst ? { delay_seconds: 0, jitter: 0 } : { delay_seconds: Math.max(0, plan.intervalMin), jitter };
  };
  // withJitter 把抖动写进文本动作的 config_json。
  const withJitter = (value: number) => value > 0 ? JSON.stringify({ delay_jitter_seconds: value }) : '{}';
  // leadTiming 是引导语的延迟与抖动；三者必须按顺序依次计算，保证位置计数正确。
  const leadTiming = nextTiming(plan.lead.enabled);
  // copyTiming 是好评文案的延迟与抖动。
  const copyTiming = nextTiming(plan.copy.enabled);
  // tailTiming 是复制提示的延迟与抖动。
  const tailTiming = nextTiming(plan.tail.enabled);
  // actions 是三段动作，顺序固定。
  const actions: AutomationAction[] = [
    { id: ids[0], action_type: 'send_text', message_template: cleanForSave ? plan.lead.text.trim() : plan.lead.text, delay_seconds: leadTiming.delay_seconds, config_json: withJitter(leadTiming.jitter), enabled: plan.lead.enabled, sort_order: 1 },
    { id: ids[1], action_type: 'send_review_copy', message_template: '', delay_seconds: copyTiming.delay_seconds, config_json: serializeCopyConfig(plan.copy, copyTiming.jitter, cleanForSave), enabled: plan.copy.enabled, sort_order: 2 },
    { id: ids[2], action_type: 'send_text', message_template: cleanForSave ? plan.tail.text.trim() : plan.tail.text, delay_seconds: tailTiming.delay_seconds, config_json: withJitter(tailTiming.jitter), enabled: plan.tail.enabled, sort_order: 3 },
  ];
  if (!cleanForSave) return actions;
  // 保存时只提交启用的动作，并重新编号。
  return actions.filter(/* 当前回调只保留启用动作。 */ action => action.enabled).map(/* 当前回调重新编号。 */ (action, index) => ({ ...action, sort_order: index + 1 }));
};

/** validateReviewPlan 校验编辑模型，返回第一条面向用户的错误文案；通过时返回空字符串。 */
export const validateReviewPlan = (plan: ReviewPlan): string => {
  if (!plan.lead.enabled && !plan.copy.enabled && !plan.tail.enabled) return '请至少启用一条求评价消息';
  if (plan.lead.enabled && !plan.lead.text.trim()) return '请填写引导语';
  if (plan.tail.enabled && !plan.tail.text.trim()) return '请填写复制提示';
  if (plan.intervalMin < 0 || plan.intervalMax < plan.intervalMin) return '消息间隔的最大秒数不能小于最小秒数';
  if (plan.intervalMax > reviewIntervalLimit) return `消息间隔最大不能超过 ${reviewIntervalLimit} 秒`;
  if (!plan.copy.enabled) return '';
  // pool 是去除空白后的有效模板。
  const pool = plan.copy.pool.map(/* 当前回调去除首尾空白。 */ item => item.trim()).filter(Boolean);
  if (pool.length > reviewPoolLimit) return `好评模板最多 ${reviewPoolLimit} 条`;
  if (plan.copy.source !== 'ai' && pool.length === 0) return plan.copy.source === 'pool' ? '请至少添加一条好评模板' : '混合模式需要至少一条好评模板作为兜底';
  if (plan.copy.source === 'mixed' && (plan.copy.aiRatio < 0 || plan.copy.aiRatio > 100)) return 'AI 使用比例必须在 0 到 100 之间';
  if (plan.copy.source !== 'pool') {
    if (plan.copy.aiMinChars < 5 || plan.copy.aiMaxChars > 200 || plan.copy.aiMinChars > plan.copy.aiMaxChars) return 'AI 文案字数范围必须在 5 到 200 之间，且最少不能大于最多';
  }
  return '';
};

/** summarizeReviewPlan 生成规则列表里的求评价摘要。 */
export const summarizeReviewPlan = (actions: AutomationAction[] | undefined): string => {
  // plan 是还原出的编辑模型。
  const plan = parseReviewPlan(actions);
  if (!plan.copy.enabled) return plan.lead.text || '发送求评价文案';
  // sourceLabel 是文案来源的中文名。
  const sourceLabel = plan.copy.source === 'ai' ? 'AI 生成' : plan.copy.source === 'mixed' ? '模板池 + AI' : `模板池 ${plan.copy.pool.filter(/* 当前回调统计有效模板。 */ item => item.trim()).length} 条`;
  // parts 是启用的各段说明。
  const parts = [plan.lead.enabled ? '引导语' : '', `好评文案（${sourceLabel}）`, plan.tail.enabled ? '复制提示' : ''].filter(Boolean);
  return parts.join(' → ');
};
