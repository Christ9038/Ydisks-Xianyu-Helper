import { describe, expect, test } from 'vitest';
import { shouldSaveNotificationBindings } from './accountBindings';

describe('notification binding save guard', () => {
  test('does not overwrite bindings when loading failed', () => {
    expect(shouldSaveNotificationBindings(false, true)).toBe(false);
  });

  test('does not write unchanged bindings', () => {
    expect(shouldSaveNotificationBindings(true, false)).toBe(false);
  });

  test('writes an explicitly changed, successfully loaded selection', () => {
    expect(shouldSaveNotificationBindings(true, true)).toBe(true);
  });
});
