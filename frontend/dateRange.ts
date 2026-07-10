export type TimeRange = 'today' | 'yesterday' | '3days' | '7days' | '30days' | 'custom';

export type DateRange = {
  startDate: string;
  endDate: string;
};

export const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
};

const addDays = (date: Date, days: number): Date => {
  const next = new Date(date);
  next.setHours(12, 0, 0, 0);
  next.setDate(next.getDate() + days);
  return next;
};

const rangeEndingAt = (end: Date, days: number): DateRange => ({
  startDate: formatLocalDate(addDays(end, -(days - 1))),
  endDate: formatLocalDate(end),
});

export const getDateRange = (
  range: TimeRange,
  now = new Date(),
  customStartDate = '',
  customEndDate = '',
): DateRange => {
  if (range === 'custom' && customStartDate && customEndDate) {
    if (customStartDate > customEndDate) {
      throw new Error('开始日期不能晚于结束日期');
    }
    return { startDate: customStartDate, endDate: customEndDate };
  }
  if (range === 'yesterday') {
    return rangeEndingAt(addDays(now, -1), 1);
  }
  const days = range === '3days' ? 3 : range === '30days' ? 30 : range === 'today' ? 1 : 7;
  return rangeEndingAt(now, days);
};

export const getPreviousDateRange = (current: DateRange): DateRange => {
  const start = new Date(`${current.startDate}T12:00:00`);
  const end = new Date(`${current.endDate}T12:00:00`);
  const dayCount = Math.round((end.getTime() - start.getTime()) / 86_400_000) + 1;
  const previousEnd = addDays(start, -1);
  return rangeEndingAt(previousEnd, dayCount);
};
