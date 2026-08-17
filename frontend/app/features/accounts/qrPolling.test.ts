import { afterEach, expect, test, vi } from 'vitest';
import { createLatestRequestGate, createQRLoginPoller } from './qrPolling';

afterEach(/* 当前回调处理用户交互或异步状态变化。 */ () => {
  vi.useRealTimers();
});

// flushMicrotasks 刷新微任务队列。
const flushMicrotasks = async () => {
  await Promise.resolve();
  await Promise.resolve();
};

test('QR poller clears the previous interval before starting another session', /* 当前回调处理用户交互或异步状态变化。 */ () => {
  vi.useFakeTimers();
  // checkStatus check状态，负责当前功能中的对应处理。
  const checkStatus = vi.fn().mockResolvedValue({ status: 'waiting' });
  // handlers 处理当前用户操作（rs）。
  const handlers = {
    onSuccess: vi.fn(),
    onTerminalError: vi.fn(),
    onPollError: vi.fn(),
  };
  // poller poller，负责当前功能中的对应处理。
  const poller = createQRLoginPoller();

  poller.start('sid-1', checkStatus, handlers);
  poller.start('sid-2', checkStatus, handlers);
  vi.advanceTimersByTime(2000);

  expect(checkStatus).toHaveBeenCalledTimes(1);
	expect(checkStatus).toHaveBeenCalledWith('sid-2', expect.any(AbortSignal));
});

test('QR poller stops on success and terminal errors', /* 当前回调处理用户交互或异步状态变化。 */ async () => {
  vi.useFakeTimers();
  // checkStatus check状态，负责当前功能中的对应处理。
  const checkStatus = vi.fn()
    .mockResolvedValueOnce({ status: 'success', cookies: 'a=b', unb: 'acc1' })
    .mockResolvedValueOnce({ status: 'expired' });
  // onSuccess 响应当前用户操作（Success）。
  const onSuccess = vi.fn();
  // onTerminalError 响应当前用户操作（Terminal错误）。
  const onTerminalError = vi.fn();
  // poller poller，负责当前功能中的对应处理。
  const poller = createQRLoginPoller();

  poller.start('sid-1', checkStatus, {
    onSuccess,
    onTerminalError,
    onPollError: vi.fn(),
  });
  await vi.advanceTimersByTimeAsync(2000);
  await flushMicrotasks();

  expect(onSuccess).toHaveBeenCalledWith({ status: 'success', cookies: 'a=b', unb: 'acc1' });
  expect(poller.isActive()).toBe(false);
  await vi.advanceTimersByTimeAsync(4000);
  expect(checkStatus).toHaveBeenCalledTimes(1);

  poller.start('sid-2', checkStatus, {
    onSuccess,
    onTerminalError,
    onPollError: vi.fn(),
  });
  await vi.advanceTimersByTimeAsync(2000);
  await flushMicrotasks();

  expect(onTerminalError).toHaveBeenCalledWith({ status: 'expired' });
  expect(poller.isActive()).toBe(false);
});

test('QR poller keeps polling during verification and stops on thrown errors', /* 当前回调处理用户交互或异步状态变化。 */ async () => {
  vi.useFakeTimers();
  // checkStatus check状态，负责当前功能中的对应处理。
  const checkStatus = vi.fn()
    .mockResolvedValueOnce({
      status: 'verification_required',
      verification_url: 'https://verify.example',
      face_qr_url: 'https://face.example',
    })
    .mockRejectedValueOnce(new Error('network down'));
  // onVerificationRequired 响应当前用户操作（VerificationRequired）。
  const onVerificationRequired = vi.fn();
  // onPollError 响应当前用户操作（轮询函数错误）。
  const onPollError = vi.fn();
  // poller poller，负责当前功能中的对应处理。
  const poller = createQRLoginPoller();

  poller.start('sid-1', checkStatus, {
    onSuccess: vi.fn(),
    onTerminalError: vi.fn(),
    onPollError,
    onVerificationRequired,
  });
  await vi.advanceTimersByTimeAsync(2000);
  await flushMicrotasks();

  expect(onVerificationRequired).toHaveBeenCalledWith(expect.objectContaining({
    status: 'verification_required',
    face_qr_url: 'https://face.example',
  }));
  expect(poller.isActive()).toBe(true);

  await vi.advanceTimersByTimeAsync(2000);
  await flushMicrotasks();

  expect(onPollError).toHaveBeenCalledWith(expect.any(Error));
  expect(poller.isActive()).toBe(false);
});

test('QR poller never overlaps slow status requests', /* 当前回调处理用户交互或异步状态变化。 */ async () => {
  vi.useFakeTimers();
  // resolveStatus resolve状态，负责当前功能中的对应处理。
  let resolveStatus: ((value: { /** status 表示状态。 */ status: string }) => void) | undefined;
  // checkStatus check状态，负责当前功能中的对应处理。
  const checkStatus = vi.fn(/* 当前回调处理用户交互或异步状态变化。 */ () => new Promise<{ /** status 表示状态。 */ status: string }>(/* 当前回调处理用户交互或异步状态变化。 */ resolve => {
    resolveStatus = resolve;
  }));
  // poller poller，负责当前功能中的对应处理。
  const poller = createQRLoginPoller();
  poller.start('slow-session', checkStatus, {
    onSuccess: vi.fn(),
    onTerminalError: vi.fn(),
    onPollError: vi.fn(),
  });

  await vi.advanceTimersByTimeAsync(6000);
  expect(checkStatus).toHaveBeenCalledTimes(1);

  resolveStatus?.({ status: 'waiting' });
  await flushMicrotasks();
  await vi.advanceTimersByTimeAsync(2000);
  expect(checkStatus).toHaveBeenCalledTimes(2);
  poller.stop();
});

test('latest request gate rejects stale generation after switch or cancel', /* 当前回调处理用户交互或异步状态变化。 */ () => {
  // gate gate，负责当前功能中的对应处理。
  const gate = createLatestRequestGate();
  // first 首项。
  const first = gate.next();
  // second second，负责当前功能中的对应处理。
  const second = gate.next();

  expect(gate.isCurrent(first)).toBe(false);
  expect(gate.isCurrent(second)).toBe(true);

  gate.cancel();
  expect(gate.isCurrent(second)).toBe(false);
});

test('stopping QR polling aborts the in-flight status request without reporting an error', /* 当前回调处理用户交互或异步状态变化。 */ async () => {
	vi.useFakeTimers();
	// onPollError 响应当前用户操作（轮询函数错误）。
	const onPollError = vi.fn();
	// observedSignal observedSignal，负责当前功能中的对应处理。
	let observedSignal: AbortSignal | undefined;
	// checkStatus check状态，负责当前功能中的对应处理。
	const checkStatus = vi.fn(/* 当前回调处理用户交互或异步状态变化。 */ (_sessionId: string, signal?: AbortSignal) => {
	  observedSignal = signal;
	  return new Promise<{ /** status 表示状态。 */ status: string }>(/* 当前回调处理用户交互或异步状态变化。 */ (_resolve, reject) => {
		signal?.addEventListener('abort', /* 当前回调处理用户交互或异步状态变化。 */ () => reject(new DOMException('aborted', 'AbortError')), { once: true });
	  });
	});
	// poller poller，负责当前功能中的对应处理。
	const poller = createQRLoginPoller();
	poller.start('sid', checkStatus, { onSuccess: vi.fn(), onTerminalError: vi.fn(), onPollError });
	await vi.advanceTimersByTimeAsync(2000);
	poller.stop();
	await flushMicrotasks();
	expect(observedSignal?.aborted).toBe(true);
	expect(onPollError).not.toHaveBeenCalled();
});
