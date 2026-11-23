import { describe,expect,it } from 'vitest';
import { buildReplyMergeUpdate,normalizeReplyMergeSeconds,REPLY_MERGE_DEFAULT_SECONDS,REPLY_MERGE_MAX_SECONDS,REPLY_MERGE_MIN_SECONDS,replyMergeFormError } from './replyMerge';

describe('买家消息合并等待', /* 套件覆盖归一化、校验与补丁构建。 */ () => {
  it('把缺失、非数字与越界的接口值归一到 1–10 的整数秒', /* 用例只验证纯函数输出。 */ () => {
    expect(normalizeReplyMergeSeconds(undefined)).toBe(REPLY_MERGE_DEFAULT_SECONDS);
    expect(normalizeReplyMergeSeconds('abc')).toBe(REPLY_MERGE_DEFAULT_SECONDS);
    expect(normalizeReplyMergeSeconds(0)).toBe(REPLY_MERGE_DEFAULT_SECONDS);
    expect(normalizeReplyMergeSeconds(-5)).toBe(REPLY_MERGE_DEFAULT_SECONDS);
    expect(normalizeReplyMergeSeconds(Number.NaN)).toBe(REPLY_MERGE_DEFAULT_SECONDS);
    expect(normalizeReplyMergeSeconds(1)).toBe(REPLY_MERGE_MIN_SECONDS);
    expect(normalizeReplyMergeSeconds(4.9)).toBe(4);
    expect(normalizeReplyMergeSeconds('7')).toBe(7);
    expect(normalizeReplyMergeSeconds(999)).toBe(REPLY_MERGE_MAX_SECONDS);
  });

  it('校验秒数必须是 1–10 之间的整数，规则与后端一致', /* 用例只验证纯函数输出。 */ () => {
    expect(replyMergeFormError({ reply_merge_seconds: 3 })).toBe('');
    expect(replyMergeFormError({ reply_merge_seconds: REPLY_MERGE_MIN_SECONDS })).toBe('');
    expect(replyMergeFormError({ reply_merge_seconds: REPLY_MERGE_MAX_SECONDS })).toBe('');
    expect(replyMergeFormError({ reply_merge_seconds: 0 })).toContain('1 到 10');
    expect(replyMergeFormError({ reply_merge_seconds: 11 })).toContain('1 到 10');
    expect(replyMergeFormError({ reply_merge_seconds: 2.5 })).toContain('整数');
    expect(replyMergeFormError({ reply_merge_seconds: Number.NaN })).toContain('整数');
  });

  it('只在秒数相对账号当前值变化时生成补丁；旧后端缺失字段按默认 3 秒比较', /* 用例只验证纯函数输出。 */ () => {
    expect(buildReplyMergeUpdate({ reply_merge_seconds: 5 }, { reply_merge_seconds: 5 })).toBeNull();
    expect(buildReplyMergeUpdate({}, { reply_merge_seconds: REPLY_MERGE_DEFAULT_SECONDS })).toBeNull();
    expect(buildReplyMergeUpdate({ reply_merge_seconds: 5 }, { reply_merge_seconds: 8 })).toEqual({ reply_merge_seconds: 8 });
    expect(buildReplyMergeUpdate({}, { reply_merge_seconds: 6 })).toEqual({ reply_merge_seconds: 6 });
  });
});
