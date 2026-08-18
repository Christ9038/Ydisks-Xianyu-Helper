import { afterEach, describe, expect, test, vi } from 'vitest';
import { del, get, post, postForm, put } from './client';

afterEach(() => vi.unstubAllGlobals() /* 清理测试安装的全局网络替身，避免跨用例泄漏。 */);

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
  } /* 测试回调验证：encodes query parameters and includes credentials。 */);

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
  } /* 测试回调验证：serializes JSON bodies。 */);

  test('支持 PUT 和 DELETE 请求及查询参数', async () => {
    // fetchMock 是 PUT/DELETE 请求的网络替身。
    const fetchMock = vi.fn().mockImplementation(/* responseFactory 为每次调用生成独立响应体。 */ () => Promise.resolve(new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    })));
    vi.stubGlobal('fetch', fetchMock);

    await expect(put('/items/1', { enabled: true })).resolves.toEqual({ ok: true });
    await expect(del('/items/1', { force: true })).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/items/1', expect.objectContaining({ method: 'PUT', body: JSON.stringify({ enabled: true }) }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/items/1?force=true', expect.objectContaining({ method: 'DELETE' }));
  } /* 测试回调验证：支持 PUT 和 DELETE 请求及查询参数。 */);

  test('surfaces unified backend errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 'bad_request', message: 'bad request' }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    })));
    await expect(get('/bad')).rejects.toThrow('bad request');
  } /* 测试回调验证：surfaces unified backend errors。 */);

  test('使用非 JSON 错误文本作为失败消息', async () => {
    // fetchMock 是返回纯文本错误的网络替身。
    const fetchMock = vi.fn().mockResolvedValue(new Response('网关暂时不可用', {
      status: 502,
      headers: { 'content-type': 'text/plain' },
    }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(get('/gateway')).rejects.toThrow('网关暂时不可用');
  } /* 测试回调验证：使用非 JSON 错误文本作为失败消息。 */);

  test('成功的非 JSON 响应直接返回文本', async () => {
    // fetchMock 是返回纯文本成功响应的网络替身。
    const fetchMock = vi.fn().mockResolvedValue(new Response('ok', {
      status: 200,
      headers: { 'content-type': 'text/plain' },
    }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(get('/text')).resolves.toBe('ok');
  } /* 测试回调验证：成功的非 JSON 响应直接返回文本。 */);

  test('JSON 错误体无法解析时使用 HTTP 状态兜底', async () => {
    // fetchMock 是返回损坏 JSON 错误体的网络替身。
    const fetchMock = vi.fn().mockResolvedValue(new Response('{broken', {
      status: 500,
      headers: { 'content-type': 'application/json' },
    }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(get('/broken')).rejects.toThrow('请求失败: 500');
  } /* 测试回调验证：JSON 错误体无法解析时使用 HTTP 状态兜底。 */);

  test('普通请求纯文本错误体读取失败时使用状态码兜底', /* 当前回调验证普通请求文本错误读取失败分支。 */ async () => {
    // response 是文本读取失败的普通请求响应替身。
    const response = { ok: false, status: 502, headers: { get: /* contentTypeGetter 返回纯文本类型。 */ () => 'text/plain' }, text: vi.fn().mockRejectedValue(new Error('读取失败')) } as unknown as Response;
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response));
    await expect(get('/broken-text')).rejects.toThrow('请求失败: 502');
  } /* 测试回调验证：普通请求纯文本错误体读取失败时使用状态码兜底。 */);

  test('网络层异常原样透传', async () => {
    // fetchMock 是抛出非取消网络异常的替身。
    const fetchMock = vi.fn().mockRejectedValue(new Error('网络断开'));
    vi.stubGlobal('fetch', fetchMock);

    await expect(get('/network-error')).rejects.toThrow('网络断开');
  } /* 测试回调验证：网络层异常原样透传。 */);

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
  } /* 测试回调验证：notifies the app when an authenticated request returns 401。 */);

  test('并发认证失败只触发一次登出事件', /* 当前回调验证认证失效通知的并发去重。 */ async () => {
    // events 是认证失效事件分发目标。
    const events = new EventTarget();
    // listener 是认证失效事件监听器。
    const listener = vi.fn();
    events.addEventListener('auth:logout', listener);
    vi.stubGlobal('window', events);
    vi.stubGlobal('fetch', vi.fn().mockImplementation(/* responseFactory 为每次调用生成认证失败响应。 */ () => Promise.resolve(new Response('{}', {
      status: 401,
      headers: { 'content-type': 'application/json' },
    }))));

    await Promise.allSettled([get('/private-a'), get('/private-b')]);
    expect(listener).toHaveBeenCalledOnce();
  } /* 测试回调验证：并发认证失败只触发一次登出事件。 */);

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
  } /* 测试回调验证：does not notify logout for a failed login request。 */);

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
  } /* 测试回调验证：posts FormData without forcing a content type。 */);

  test('aborts requests at the configured timeout', async () => {
	vi.useFakeTimers();
	const fetchMock = vi.fn((_url: string, init: RequestInit) => new Promise<Response>((_resolve, reject) => {
	  init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')) /* 请求被取消时拒绝网络替身 Promise。 */, { once: true });
	} /* 网络替身只在测试显式取消前保持挂起。 */) /* mockImplementation 回调模拟永不完成的上传请求。 */); /* fetchMock 是用于观察上传请求取消的网络替身。 */
	vi.stubGlobal('fetch', fetchMock);
	const pending = get('/slow', undefined, { timeoutMs: 50 }); /* pending 表示pending。 */
	const rejection = expect(pending).rejects.toThrow('请求超时'); /* rejection 表示rejection。 */
	await vi.advanceTimersByTimeAsync(50);
	await rejection;
	vi.useRealTimers();
  } /* 测试回调验证：aborts requests at the configured timeout。 */);

  test('上传请求超时后返回上传专用错误', /* 当前回调验证上传请求的超时错误分支。 */ async () => {
    vi.useFakeTimers();
    try {
      // fetchMock 是等待上传超时信号的网络替身。
      const fetchMock = vi.fn(/* uploadTimeoutFactory 创建等待上传超时的网络替身。 */ (_url: string, init: RequestInit) => new Promise<Response>(/* uploadTimeoutExecutor 等待上传控制器超时。 */ (_resolve, reject) => {
        init.signal?.addEventListener('abort', /* abortCallback 将上传超时转换为网络异常。 */ () => reject(new DOMException('timeout', 'AbortError')), { once: true });
      }));
      vi.stubGlobal('fetch', fetchMock);
      // pending 是等待上传超时结果的请求 Promise。
      const pending = postForm('/slow-upload', new FormData(), { timeoutMs: 50 });
      // rejection 是断言上传超时错误的异步结果。
      const rejection = expect(pending).rejects.toThrow('上传超时');
      await vi.advanceTimersByTimeAsync(50);
      await rejection;
    } finally {
      vi.useRealTimers();
    }
  } /* 测试回调验证：上传请求超时后返回上传专用错误。 */);

  test('外部 AbortSignal 取消请求时返回取消错误', async () => {
    // controller 是调用方主动取消请求的控制器。
    const controller = new AbortController();
    // fetchMock 是等待外部取消信号的网络替身。
    const fetchMock = vi.fn((_url: string, init: RequestInit) => new Promise<Response>(
      /* fetchPromiseExecutor 等待请求信号触发取消。 */ (_resolve, reject) => {
        init.signal?.addEventListener('abort', /* abortCallback 将取消转换为网络异常。 */ () => reject(new DOMException('aborted', 'AbortError')), { once: true });
      },
    ));
    vi.stubGlobal('fetch', fetchMock);

    // pending 是等待外部取消结果的请求 Promise。
    const pending = get('/cancelled', undefined, { signal: controller.signal });
    controller.abort();
    await expect(pending).rejects.toThrow('请求已取消');
  } /* 测试回调验证：外部 AbortSignal 取消请求时返回取消错误。 */);

  test('上传请求支持外部取消并返回取消错误', async () => {
    // controller 是上传调用方主动取消请求的控制器。
    const controller = new AbortController();
    // fetchMock 是等待上传取消信号的网络替身。
    const fetchMock = vi.fn((_url: string, init: RequestInit) => new Promise<Response>(
      /* uploadPromiseExecutor 等待上传信号触发取消。 */ (_resolve, reject) => {
        init.signal?.addEventListener('abort', /* abortCallback 将上传取消转换为网络异常。 */ () => reject(new DOMException('aborted', 'AbortError')), { once: true });
      },
    ));
    vi.stubGlobal('fetch', fetchMock);

    // pending 是等待上传取消结果的请求 Promise。
    const pending = postForm('/upload', new FormData(), { signal: controller.signal });
    controller.abort();
    await expect(pending).rejects.toThrow('请求已取消');
  } /* 测试回调验证：上传请求支持外部取消并返回取消错误。 */);

  test('上传网络层异常原样透传', async () => {
    // fetchMock 是抛出非取消上传异常的替身。
    const fetchMock = vi.fn().mockRejectedValue(new Error('上传网络断开'));
    vi.stubGlobal('fetch', fetchMock);

    await expect(postForm('/upload', new FormData())).rejects.toThrow('上传网络断开');
  } /* 测试回调验证：上传网络层异常原样透传。 */);

  test('上传失败保留原始错误载荷', async () => {
    // payload 是服务端返回的上传错误详情。
    const payload = { code: 'invalid_file', message: '文件格式不支持' };
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), {
      status: 422,
      headers: { 'content-type': 'application/json' },
    })));

    try {
      await postForm('/upload', new FormData());
      throw new Error('应当抛出上传错误');
    } catch (error /* error 表示上传异常。 */) {
      // uploadError 是包含后端载荷的上传异常。
      const uploadError = error as Error & { /* payload 表示原始错误载荷。 */ payload?: unknown };
      expect(uploadError.message).toBe('文件格式不支持');
      expect(uploadError.payload).toEqual(payload);
    }
  } /* 测试回调验证：上传失败保留原始错误载荷。 */);

  test('非 JSON 上传错误体读取失败时使用状态码兜底', /* 当前回调验证上传错误文本读取失败分支。 */ async () => {
    // response 是文本读取失败的上传响应替身。
    const response = { ok: false, status: 500, headers: { get: /* contentTypeGetter 返回纯文本类型。 */ () => 'text/plain' }, text: vi.fn().mockRejectedValue(new Error('读取失败')) } as unknown as Response;
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response));
    await expect(postForm('/upload', new FormData())).rejects.toThrow('请求失败: 500');
  } /* 测试回调验证：非 JSON 上传错误体读取失败时使用状态码兜底。 */);

  test('上传认证失败也会触发登出事件', /* 当前回调验证上传请求的认证失效通知。 */ async () => {
    // events 是上传认证失效事件分发目标。
    const events = new EventTarget();
    // listener 是上传认证失效事件监听器。
    const listener = vi.fn();
    events.addEventListener('auth:logout', listener);
    vi.stubGlobal('window', events);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', {
      status: 401,
      headers: { 'content-type': 'application/json' },
    })));
    await expect(postForm('/upload', new FormData())).rejects.toThrow();
    expect(listener).toHaveBeenCalledOnce();
  } /* 测试回调验证：上传认证失败也会触发登出事件。 */);

  test('请求开始前已取消的外部信号返回取消错误', /* 当前回调验证预取消信号分支。 */ async () => {
    // controller 是预先取消请求的外部控制器。
    const controller = new AbortController();
    controller.abort();
    // fetchMock 是观察预取消信号的网络替身。
    const fetchMock = vi.fn(/* fetchFactory 创建预取消信号网络替身。 */ (_url: string, init: RequestInit) => new Promise<Response>(/* fetchExecutor 观察预取消信号。 */ (_resolve, reject) => {
      if (init.signal?.aborted) reject(new DOMException('aborted', 'AbortError'));
    }));
    vi.stubGlobal('fetch', fetchMock);
    await expect(get('/pre-cancelled', undefined, { signal: controller.signal })).rejects.toThrow('请求已取消');
  } /* 测试回调验证：请求开始前已取消的外部信号返回取消错误。 */);
} /* 测试套件回调汇总共享 HTTP 客户端的成功、失败与取消契约。 */);
