import type { AccountDetail,ManualTakeoverScope } from './models';
import type { AccountEditForm } from './types';

// MANUAL_TAKEOVER_MAX_MINUTES 是人工介入暂停窗口的最大分钟数，与后端 ManualTakeoverMaxMinutes 一致。
export const MANUAL_TAKEOVER_MAX_MINUTES = 1440;

// ManualTakeoverScopeOption 是编辑弹窗中一个暂停范围选项的展示信息。
export interface ManualTakeoverScopeOption {
  // value 是提交给后端的范围取值。
  value: ManualTakeoverScope;
  // label 是选项按钮上的简短名称。
  label: string;
  // hint 说明选中该范围后暂停窗口内哪些自动回复会被跳过。
  hint: string;
}

// MANUAL_TAKEOVER_SCOPES 是编辑弹窗展示的暂停范围选项，顺序即界面顺序。
export const MANUAL_TAKEOVER_SCOPES: ReadonlyArray<ManualTakeoverScopeOption> = [
  { value: 'ai', label: '只暂停 AI', hint: '暂停窗口内跳过 Agent 与 AI 回复；关键词、默认回复和商品首次回复照常发送' },
  { value: 'all', label: '暂停全部', hint: '暂停窗口内该会话不发送任何自动回复；买家命中“通知人工”关键词时仍会通知' },
];

// normalizeManualTakeoverScope 把接口返回值归一为已知范围；缺失或未知值（旧后端、手工改库）按 ai 处理。
export const normalizeManualTakeoverScope = (value: unknown): ManualTakeoverScope => (value === 'all' ? 'all' : 'ai');

// normalizeManualTakeoverMinutes 把接口返回值归一为 [0, 1440] 内的整数分钟；非数字按 0（关闭）处理。
export const normalizeManualTakeoverMinutes = (value: unknown): number => {
  // minutes 是数值化后的分钟数。
  const minutes = Number(value);
  if (!Number.isFinite(minutes) || minutes <= 0) return 0;
  return Math.min(MANUAL_TAKEOVER_MAX_MINUTES, Math.floor(minutes));
};

// ManualTakeoverEditFields 是编辑表单中参与人工介入暂停设置的字段。
export type ManualTakeoverEditFields = Pick<AccountEditForm, 'manual_takeover_minutes' | 'manual_takeover_scope'>;

// manualTakeoverFormError 返回人工介入暂停设置的表单错误文案；空字符串表示可以保存。规则与后端校验一致。
export const manualTakeoverFormError = (form: ManualTakeoverEditFields): string => {
  // minutes 是表单中的分钟数草稿。
  const minutes = form.manual_takeover_minutes;
  if (!Number.isInteger(minutes) || minutes < 0 || minutes > MANUAL_TAKEOVER_MAX_MINUTES) return `暂停分钟数必须是 0 到 ${MANUAL_TAKEOVER_MAX_MINUTES} 之间的整数`;
  return '';
};

// ManualTakeoverPayload 是提交给账号设置接口的人工介入暂停补丁。
export interface ManualTakeoverPayload {
  // manual_takeover_minutes 是暂停分钟数，0 表示关闭。
  manual_takeover_minutes?: number;
  // manual_takeover_scope 是暂停范围。
  manual_takeover_scope?: ManualTakeoverScope;
}

// buildManualTakeoverUpdate 只提交发生变化的字段；没有变化时返回 null。
export const buildManualTakeoverUpdate = (
  account: Pick<AccountDetail, 'manual_takeover_minutes' | 'manual_takeover_scope'>,
  form: ManualTakeoverEditFields,
): ManualTakeoverPayload | null => {
  // payload 收集与账号当前值不同的字段。
  const payload: ManualTakeoverPayload = {};
  if (form.manual_takeover_minutes !== normalizeManualTakeoverMinutes(account.manual_takeover_minutes)) payload.manual_takeover_minutes = form.manual_takeover_minutes;
  if (form.manual_takeover_scope !== normalizeManualTakeoverScope(account.manual_takeover_scope)) payload.manual_takeover_scope = form.manual_takeover_scope;
  return Object.keys(payload).length > 0 ? payload : null;
};
