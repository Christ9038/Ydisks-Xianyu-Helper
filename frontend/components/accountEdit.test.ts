import { expect, test } from 'vitest';
import { buildAccountLoginInfoUpdate } from './accountEdit';

test('buildAccountLoginInfoUpdate omits blank password while updating other login fields', () => {
  const payload = buildAccountLoginInfoUpdate(
    { id: 'acc1', enabled: true, auto_confirm: true, username: 'old-user', show_browser: true },
    { username: 'new-user', login_password: '', show_browser: false },
  );

  expect(payload).toEqual({
    username: 'new-user',
    show_browser: false,
  });
});

test('buildAccountLoginInfoUpdate includes password only when user entered one', () => {
  const payload = buildAccountLoginInfoUpdate(
    { id: 'acc1', enabled: true, auto_confirm: true, username: 'old-user', show_browser: false },
    { username: 'old-user', login_password: 'new-secret', show_browser: false },
  );

  expect(payload).toEqual({
    username: 'old-user',
    login_password: 'new-secret',
    show_browser: false,
  });
});

test('buildAccountLoginInfoUpdate skips unchanged login info', () => {
  const payload = buildAccountLoginInfoUpdate(
    { id: 'acc1', enabled: true, auto_confirm: true, username: 'old-user', show_browser: false },
    { username: 'old-user', login_password: '', show_browser: false },
  );

  expect(payload).toBeNull();
});

test('buildAccountLoginInfoUpdate sends explicit clear password flag', () => {
  const payload = buildAccountLoginInfoUpdate(
    { id: 'acc1', enabled: true, auto_confirm: true, username: 'old-user', show_browser: false },
    { username: 'old-user', login_password: '', show_browser: false, clear_password: true },
  );

  expect(payload).toEqual({
    username: 'old-user',
    show_browser: false,
    clear_password: true,
  });
});

test('buildAccountLoginInfoUpdate prefers clear password over typed password', () => {
  const payload = buildAccountLoginInfoUpdate(
    { id: 'acc1', enabled: true, auto_confirm: true, username: 'old-user', show_browser: false },
    { username: 'old-user', login_password: 'new-secret', show_browser: false, clear_password: true },
  );

  expect(payload).toEqual({
    username: 'old-user',
    show_browser: false,
    clear_password: true,
  });
});
