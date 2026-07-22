import { describe, expect, test } from 'vitest';
import { formatLocalDateTime } from './dateTime';

describe('formatLocalDateTime', () => {
  test('uses the browser local timezone and the standard second-level format', () => {
    expect(formatLocalDateTime(new Date(2026, 6, 15, 10, 54, 12))).toBe('2026-07-15 10:54:12');
  });

  test('returns a placeholder for missing or invalid timestamps', () => {
    expect(formatLocalDateTime()).toBe('-');
    expect(formatLocalDateTime('not-a-date')).toBe('-');
  });
});
