import { describe, expect, test } from 'vitest';
import { estimatePublishSeconds, formatDuration } from './publishEstimate';

// 发布耗时估算按“行数 - 1”个间隔计算，无效间隔回退为服务端默认 5 秒。
describe('estimatePublishSeconds', /* 测试组回调覆盖行数与间隔的组合。 */ () => {
  test('按串行间隔估算并回退默认间隔', /* 当前回调验证估算公式。 */ () => {
    expect(estimatePublishSeconds(0, '5')).toBe(0);
    expect(estimatePublishSeconds(1, '30')).toBe(0);
    expect(estimatePublishSeconds(61, '10')).toBe(600);
    expect(estimatePublishSeconds(3, '0')).toBe(10);
    expect(estimatePublishSeconds(3, '')).toBe(10);
    expect(estimatePublishSeconds(3, 'abc')).toBe(10);
  });
});

// 时长文本按秒、分钟、小时分档，分钟向上取整。
describe('formatDuration', /* 测试组回调覆盖各档位。 */ () => {
  test('按档位格式化', /* 当前回调验证格式化结果。 */ () => {
    expect(formatDuration(45)).toBe('约 45 秒');
    expect(formatDuration(61)).toBe('约 2 分钟');
    expect(formatDuration(3600)).toBe('约 1 小时');
    expect(formatDuration(3700)).toBe('约 1 小时 2 分钟');
  });
});
