const padTwoDigits = (value: number): string => String(value).padStart(2, '0');

/** Format a timestamp in the browser's current timezone. */
export const formatLocalDateTime = (value?: string | number | Date | null): string => {
  if (value === undefined || value === null || value === '') return '-';

  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return '-';

  return [
    date.getFullYear(),
    padTwoDigits(date.getMonth() + 1),
    padTwoDigits(date.getDate()),
  ].join('-') + ' ' + [
    padTwoDigits(date.getHours()),
    padTwoDigits(date.getMinutes()),
    padTwoDigits(date.getSeconds()),
  ].join(':');
};
