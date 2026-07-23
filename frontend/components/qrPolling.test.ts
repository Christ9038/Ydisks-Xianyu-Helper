import { afterEach, expect, test, vi } from 'vitest';
import { createLatestRequestGate, createQRLoginPoller } from './qrPolling';

afterEach(() => {
  vi.useRealTimers();
});

const flushMicrotasks = async () => {
  await Promise.resolve();
  await Promise.resolve();
};

test('QR poller clears the previous interval before starting another session', () => {
  vi.useFakeTimers();
  const checkStatus = vi.fn().mockResolvedValue({ status: 'waiting' });
  const handlers = {
    onSuccess: vi.fn(),
    onTerminalError: vi.fn(),
    onPollError: vi.fn(),
  };
  const poller = createQRLoginPoller();

  poller.start('sid-1', checkStatus, handlers);
  poller.start('sid-2', checkStatus, handlers);
  vi.advanceTimersByTime(2000);

  expect(checkStatus).toHaveBeenCalledTimes(1);
	expect(checkStatus).toHaveBeenCalledWith('sid-2', expect.any(AbortSignal));
});

test('QR poller stops on success and terminal errors', async () => {
  vi.useFakeTimers();
  const checkStatus = vi.fn()
    .mockResolvedValueOnce({ status: 'success', cookies: 'a=b', unb: 'acc1' })
    .mockResolvedValueOnce({ status: 'expired' });
  const onSuccess = vi.fn();
  const onTerminalError = vi.fn();
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

test('QR poller keeps polling during verification and stops on thrown errors', async () => {
  vi.useFakeTimers();
  const checkStatus = vi.fn()
    .mockResolvedValueOnce({
      status: 'verification_required',
      verification_url: 'https://verify.example',
      face_qr_url: 'https://face.example',
    })
    .mockRejectedValueOnce(new Error('network down'));
  const onVerificationRequired = vi.fn();
  const onPollError = vi.fn();
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

test('QR poller never overlaps slow status requests', async () => {
  vi.useFakeTimers();
  let resolveStatus: ((value: { status: string }) => void) | undefined;
  const checkStatus = vi.fn(() => new Promise<{ status: string }>(resolve => {
    resolveStatus = resolve;
  }));
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

test('latest request gate rejects stale generation after switch or cancel', () => {
  const gate = createLatestRequestGate();
  const first = gate.next();
  const second = gate.next();

  expect(gate.isCurrent(first)).toBe(false);
  expect(gate.isCurrent(second)).toBe(true);

  gate.cancel();
  expect(gate.isCurrent(second)).toBe(false);
});

test('stopping QR polling aborts the in-flight status request without reporting an error', async () => {
	vi.useFakeTimers();
	const onPollError = vi.fn();
	let observedSignal: AbortSignal | undefined;
	const checkStatus = vi.fn((_sessionId: string, signal?: AbortSignal) => {
	  observedSignal = signal;
	  return new Promise<{ status: string }>((_resolve, reject) => {
		signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true });
	  });
	});
	const poller = createQRLoginPoller();
	poller.start('sid', checkStatus, { onSuccess: vi.fn(), onTerminalError: vi.fn(), onPollError });
	await vi.advanceTimersByTimeAsync(2000);
	poller.stop();
	await flushMicrotasks();
	expect(observedSignal?.aborted).toBe(true);
	expect(onPollError).not.toHaveBeenCalled();
});
