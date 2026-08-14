export type TimeRange = 'today' | 'yesterday' | '3days' | '7days' | '30days' | 'custom';

export type DateRange = {
// startDate 表示startDate。
    startDate: string;
// endDate 表示endDate。
    endDate: string;
};

export const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear(); /* year 表示year。 */
  const month = String(date.getMonth() + 1).padStart(2, '0'); /* month 表示month。 */
  const day = String(date.getDate()).padStart(2, '0'); /* day 表示day。 */
  return `${year}-${month}-${day}`;
}; /* formatLocalDate 表示formatLocalDate。 */

const addDays = (date: Date, days: number): Date => {
  const next = new Date(date); /* next 表示next。 */
  next.setHours(12, 0, 0, 0);
  next.setDate(next.getDate() + days);
  return next;
}; /* addDays 表示addDays。 */

const rangeEndingAt = (end: Date, days: number): DateRange => ({
  startDate: formatLocalDate(addDays(end, -(days - 1))),
  endDate: formatLocalDate(end),
}); /* rangeEndingAt 表示rangeEndingAt。 */

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
  const days = range === '3days' ? 3 : range === '30days' ? 30 : range === 'today' ? 1 : 7; /* days 表示days。 */
  return rangeEndingAt(now, days);
}; /* getDateRange 表示getDateRange。 */

export const getPreviousDateRange = (current: DateRange): DateRange => {
  const start = new Date(`${current.startDate}T12:00:00`); /* start 表示start。 */
  const end = new Date(`${current.endDate}T12:00:00`); /* end 表示end。 */
  const dayCount = Math.round((end.getTime() - start.getTime()) / 86_400_000) + 1; /* dayCount 表示dayCount。 */
  const previousEnd = addDays(start, -1); /* previousEnd 表示previousEnd。 */
  return rangeEndingAt(previousEnd, dayCount);
}; /* getPreviousDateRange 表示getPreviousDateRange。 */
