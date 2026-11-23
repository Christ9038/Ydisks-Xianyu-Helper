import type { AccountDetail } from './models';
import type { AccountEditForm } from './types';

// REPLY_MERGE_MIN_SECONDS 与 REPLY_MERGE_MAX_SECONDS 是买家分段消息“合并等待”秒数的允许区间，与后端校验一致。
export const REPLY_MERGE_MIN_SECONDS = 1;
// REPLY_MERGE_MAX_SECONDS 是合并等待秒数上限；更长的等待会让买家明显感到没人理。
export const REPLY_MERGE_MAX_SECONDS = 10;
// REPLY_MERGE_DEFAULT_SECONDS 是新账号与旧后端缺失该字段时使用的默认秒数，与后端默认值一致。
export const REPLY_MERGE_DEFAULT_SECONDS = 3;

// normalizeReplyMergeSeconds 把接口返回值归一为 [1, 10] 内的整数秒；缺失、非数字或非正数按默认值处理（旧后端、手工改库）。
export const normalizeReplyMergeSeconds = (value: unknown): number => {
  // seconds 是数值化后的秒数。
  const seconds = Number(value);
  if (!Number.isFinite(seconds) || seconds < REPLY_MERGE_MIN_SECONDS) return REPLY_MERGE_DEFAULT_SECONDS;
  return Math.min(REPLY_MERGE_MAX_SECONDS, Math.floor(seconds));
};

// ReplyMergeEditFields 是编辑表单中参与合并等待设置的字段。
export type ReplyMergeEditFields = Pick<AccountEditForm, 'reply_merge_seconds'>;

// replyMergeFormError 返回合并等待设置的表单错误文案；空字符串表示可以保存。规则与后端校验一致。
export const replyMergeFormError = (form: ReplyMergeEditFields): string => {
  // seconds 是表单中的秒数草稿。
  const seconds = form.reply_merge_seconds;
  if (!Number.isInteger(seconds) || seconds < REPLY_MERGE_MIN_SECONDS || seconds > REPLY_MERGE_MAX_SECONDS) {
    return `合并等待秒数必须是 ${REPLY_MERGE_MIN_SECONDS} 到 ${REPLY_MERGE_MAX_SECONDS} 之间的整数`;
  }
  return '';
};

// ReplyMergePayload 是提交给账号设置接口的合并等待补丁。
export interface ReplyMergePayload {
  // reply_merge_seconds 是合并等待秒数。
  reply_merge_seconds: number;
}

// buildReplyMergeUpdate 只在秒数相对账号当前值发生变化时返回补丁；没有变化时返回 null。
export const buildReplyMergeUpdate = (
  account: Pick<AccountDetail, 'reply_merge_seconds'>,
  form: ReplyMergeEditFields,
): ReplyMergePayload | null => (
  form.reply_merge_seconds !== normalizeReplyMergeSeconds(account.reply_merge_seconds) ? { reply_merge_seconds: form.reply_merge_seconds } : null
);
