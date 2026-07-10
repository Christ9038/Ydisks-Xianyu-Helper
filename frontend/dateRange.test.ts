import { describe, expect, test } from 'vitest';
import { getDateRange, getPreviousDateRange } from './dateRange';

describe('date ranges', () => {
  const now = new Date('2026-07-10T12:00:00');

  test.each([
    ['3days', '2026-07-08'],
    ['7days', '2026-07-04'],
    ['30days', '2026-06-11'],
  ] as const)('%s includes exactly the requested number of days', (range, startDate) => {
    expect(getDateRange(range, now)).toEqual({ startDate, endDate: '2026-07-10' });
  });

  test('previous range has the same length without overlap', () => {
    const current = getDateRange('7days', now);
    expect(getPreviousDateRange(current)).toEqual({ startDate: '2026-06-27', endDate: '2026-07-03' });
  });

  test('works across year boundaries', () => {
    expect(getDateRange('3days', new Date('2026-01-01T12:00:00'))).toEqual({
      startDate: '2025-12-30',
      endDate: '2026-01-01',
    });
  });

  test('rejects reversed custom dates', () => {
    expect(() => getDateRange('custom', now, '2026-07-11', '2026-07-10')).toThrow('开始日期不能晚于结束日期');
  });
});
