import { afterEach, describe, expect, test, vi } from 'vitest';
import { get, post, postForm } from './request';

afterEach(() => vi.unstubAllGlobals());

describe('request helpers', () => {
  test('encodes query parameters and includes credentials', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(get('/items', { page: 2, keyword: 'a b', ignored: undefined })).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledWith('/items?page=2&keyword=a+b', expect.objectContaining({
      method: 'GET',
      credentials: 'include',
    }));
  });

  test('serializes JSON bodies', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    }));
    vi.stubGlobal('fetch', fetchMock);

    await post('/login', { username: 'admin' });
    expect(fetchMock).toHaveBeenCalledWith('/login', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ username: 'admin' }),
      headers: { 'Content-Type': 'application/json' },
    }));
  });

  test('surfaces backend errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ detail: 'bad request' }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    })));
    await expect(get('/bad')).rejects.toThrow('bad request');
  });

  test('notifies the app when an authenticated request returns 401', async () => {
    const events = new EventTarget();
    const listener = vi.fn();
    events.addEventListener('auth:logout', listener);
    vi.stubGlobal('window', events);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', {
      status: 401,
      headers: { 'content-type': 'application/json' },
    })));

    await expect(get('/private')).rejects.toThrow();
    expect(listener).toHaveBeenCalledOnce();
  });

  test('does not notify logout for a failed login request', async () => {
    const events = new EventTarget();
    const listener = vi.fn();
    events.addEventListener('auth:logout', listener);
    vi.stubGlobal('window', events);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', {
      status: 401,
      headers: { 'content-type': 'application/json' },
    })));

    await expect(post('/login', {}, { skipAuthLogout: true })).rejects.toThrow();
    expect(listener).not.toHaveBeenCalled();
  });

  test('posts FormData without forcing a content type', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('ok', { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const form = new FormData();
    form.set('name', 'value');
    await expect(postForm('/upload', form)).resolves.toBe('ok');
    expect(fetchMock).toHaveBeenCalledWith('/upload', {
      method: 'POST',
      credentials: 'include',
      body: form,
    });
  });
});
