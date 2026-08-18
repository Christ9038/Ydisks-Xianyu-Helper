import { describe,expect,test } from 'vitest';
import { getDateRange,getPreviousDateRange } from './dateRange';

describe('date ranges', () => {
  const now = new Date('2026-07-10T12:00:00'); /* now 表示now。 */

  test.each([
    ['3days', '2026-07-08'],
    ['7days', '2026-07-04'],
    ['30days', '2026-06-11'],
  ] as const)('%s includes exactly the requested number of days', (range, startDate) => {
    expect(getDateRange(range, now)).toEqual({ startDate, endDate: '2026-07-10' });
  } /* 参数化测试回调断言预设范围包含正确的起始日期。 */);

  test('previous range has the same length without overlap', () => {
    const current = getDateRange('7days', now); /* current 表示current。 */
    expect(getPreviousDateRange(current)).toEqual({ startDate: '2026-06-27', endDate: '2026-07-03' });
  } /* 测试回调断言上一周期等长且不与当前周期重叠。 */);

  test('works across year boundaries', () => {
    expect(getDateRange('3days', new Date('2026-01-01T12:00:00'))).toEqual({
      startDate: '2025-12-30',
      endDate: '2026-01-01',
    });
  } /* 测试回调断言跨年日期计算保持日历正确性。 */);

  test('rejects reversed custom dates', () => {
    expect(() => getDateRange('custom', now, '2026-07-11', '2026-07-10') /* 断言回调调用日期范围函数并触发参数校验。 */).toThrow('开始日期不能晚于结束日期');
  } /* 测试回调断言反向自定义日期被拒绝。 */);

  test('支持自定义日期和昨天范围', () => {
    expect(getDateRange('custom', now, '2026-07-01', '2026-07-05')).toEqual({ startDate: '2026-07-01', endDate: '2026-07-05' });
    expect(getDateRange('yesterday', now)).toEqual({ startDate: '2026-07-09', endDate: '2026-07-09' });
    expect(getDateRange('today', now)).toEqual({ startDate: '2026-07-10', endDate: '2026-07-10' });
  } /* 测试回调断言自定义、今天和昨天范围的边界。 */);
} /* 测试套件回调汇总日期范围计算契约。 */);
