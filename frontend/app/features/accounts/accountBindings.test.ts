import { describe,expect,test } from 'vitest';
import { shouldSaveNotificationBindings } from './accountBindings';

describe('notification binding save guard', /* 当前回调处理用户交互或异步状态变化。 */ () => {
  test('does not overwrite bindings when loading failed', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    expect(shouldSaveNotificationBindings(false, true)).toBe(false);
  });

  test('does not write unchanged bindings', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    expect(shouldSaveNotificationBindings(true, false)).toBe(false);
  });

  test('writes an explicitly changed, successfully loaded selection', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    expect(shouldSaveNotificationBindings(true, true)).toBe(true);
  });
});
