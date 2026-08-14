import { afterEach, describe, expect, test, vi } from 'vitest';
import { get, post, postForm } from './request';

afterEach(() => vi.unstubAllGlobals() /* 回调函数负责当前业务流程。 */);

describe('request helpers', () => {
  test('encodes query parameters and includes credentials', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    })); /* fetchMock 表示fetchMock。 */
    vi.stubGlobal('fetch', fetchMock);

    await expect(get('/items', { page: 2, keyword: 'a b', ignored: undefined })).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledWith('/items?page=2&keyword=a+b', expect.objectContaining({
      method: 'GET',
      credentials: 'include',
    }));
  } /* 回调函数负责当前业务流程。 */);

  test('serializes JSON bodies', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ success: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    })); /* fetchMock 表示fetchMock。 */
    vi.stubGlobal('fetch', fetchMock);

    await post('/login', { username: 'admin' });
    expect(fetchMock).toHaveBeenCalledWith('/login', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ username: 'admin' }),
      headers: { 'Content-Type': 'application/json' },
    }));
  } /* 回调函数负责当前业务流程。 */);

  test('surfaces unified backend errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 'bad_request', message: 'bad request' }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    })));
    await expect(get('/bad')).rejects.toThrow('bad request');
  } /* 回调函数负责当前业务流程。 */);

  test('notifies the app when an authenticated request returns 401', async () => {
    const events = new EventTarget(); /* events 表示events。 */
    const listener = vi.fn(); /* listener 表示listener。 */
    events.addEventListener('auth:logout', listener);
    vi.stubGlobal('window', events);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', {
      status: 401,
      headers: { 'content-type': 'application/json' },
    })));

    await expect(get('/private')).rejects.toThrow();
    expect(listener).toHaveBeenCalledOnce();
  } /* 回调函数负责当前业务流程。 */);

  test('does not notify logout for a failed login request', async () => {
    const events = new EventTarget(); /* events 表示events。 */
    const listener = vi.fn(); /* listener 表示listener。 */
    events.addEventListener('auth:logout', listener);
    vi.stubGlobal('window', events);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', {
      status: 401,
      headers: { 'content-type': 'application/json' },
    })));

    await expect(post('/login', {}, { skipAuthLogout: true })).rejects.toThrow();
    expect(listener).not.toHaveBeenCalled();
  } /* 回调函数负责当前业务流程。 */);

  test('posts FormData without forcing a content type', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('ok', { status: 200 })); /* fetchMock 表示fetchMock。 */
    vi.stubGlobal('fetch', fetchMock);
    const form = new FormData(); /* form 表示form。 */
    form.set('name', 'value');
    await expect(postForm('/upload', form)).resolves.toBe('ok');
	expect(fetchMock).toHaveBeenCalledWith('/upload', expect.objectContaining({
	  method: 'POST',
	  credentials: 'include',
	  body: form,
	}));
  } /* 回调函数负责当前业务流程。 */);

  test('aborts requests at the configured timeout', async () => {
	vi.useFakeTimers();
	const fetchMock = vi.fn((_url: string, init: RequestInit) => new Promise<Response>((_resolve, reject) => {
	  init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')) /* 回调函数负责当前业务流程。 */, { once: true });
	} /* 回调函数负责当前业务流程。 */) /* 回调函数负责当前业务流程。 */); /* fetchMock 表示fetchMock。 */
	vi.stubGlobal('fetch', fetchMock);
	const pending = get('/slow', undefined, { timeoutMs: 50 }); /* pending 表示pending。 */
	const rejection = expect(pending).rejects.toThrow('请求超时'); /* rejection 表示rejection。 */
	await vi.advanceTimersByTimeAsync(50);
	await rejection;
	vi.useRealTimers();
  } /* 回调函数负责当前业务流程。 */);
} /* 回调函数负责当前业务流程。 */);
