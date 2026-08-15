import { describe, expect, test } from 'vitest';
import { getDateRange, getPreviousDateRange } from './dateRange';

describe('date ranges', () => {
  const now = new Date('2026-07-10T12:00:00'); /* now 表示now。 */

  test.each([
    ['3days', '2026-07-08'],
    ['7days', '2026-07-04'],
    ['30days', '2026-06-11'],
  ] as const)('%s includes exactly the requested number of days', (range, startDate) => {
    expect(getDateRange(range, now)).toEqual({ startDate, endDate: '2026-07-10' });
  } /* 回调函数负责当前业务流程。 */);

  test('previous range has the same length without overlap', () => {
    const current = getDateRange('7days', now); /* current 表示current。 */
    expect(getPreviousDateRange(current)).toEqual({ startDate: '2026-06-27', endDate: '2026-07-03' });
  } /* 回调函数负责当前业务流程。 */);

  test('works across year boundaries', () => {
    expect(getDateRange('3days', new Date('2026-01-01T12:00:00'))).toEqual({
      startDate: '2025-12-30',
      endDate: '2026-01-01',
    });
  } /* 回调函数负责当前业务流程。 */);

  test('rejects reversed custom dates', () => {
    expect(() => getDateRange('custom', now, '2026-07-11', '2026-07-10') /* 回调函数负责当前业务流程。 */).toThrow('开始日期不能晚于结束日期');
  } /* 回调函数负责当前业务流程。 */);

  test('支持自定义日期和昨天范围', () => {
    expect(getDateRange('custom', now, '2026-07-01', '2026-07-05')).toEqual({ startDate: '2026-07-01', endDate: '2026-07-05' });
    expect(getDateRange('yesterday', now)).toEqual({ startDate: '2026-07-09', endDate: '2026-07-09' });
    expect(getDateRange('today', now)).toEqual({ startDate: '2026-07-10', endDate: '2026-07-10' });
  } /* 回调函数负责当前业务流程。 */);
} /* 回调函数负责当前业务流程。 */);
