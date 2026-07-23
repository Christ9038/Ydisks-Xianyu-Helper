export interface QRLoginStatusResult {
  status?: string;
  cookies?: string;
  unb?: string;
  verification_url?: string;
  verification_screenshot?: string;
  face_qr_url?: string;
}

interface QRLoginPollerTimers {
  setInterval: (handler: () => void, timeout: number) => ReturnType<typeof setInterval>;
  clearInterval: (id: ReturnType<typeof setInterval>) => void;
}

export interface QRLoginPollHandlers {
  onSuccess: (status: QRLoginStatusResult) => void | Promise<void>;
  onScanned?: (status: QRLoginStatusResult) => void;
  onVerificationRequired?: (status: QRLoginStatusResult) => void;
  onTerminalError: (status: QRLoginStatusResult) => void;
  onPollError: (error: unknown) => void;
}

const terminalQRStatuses = new Set(['expired', 'cancelled', 'error', 'not_found']);

// createLatestRequestGate 让只能由“最后一次用户操作”提交结果的异步请求拥有
// 明确代次。cancel 会使所有尚未返回的请求失效，但不要求底层 fetch 支持中断。
export const createLatestRequestGate = () => {
  let generation = 0;
  return {
    next: () => {
      generation += 1;
      return generation;
    },
    cancel: () => {
      generation += 1;
    },
    isCurrent: (candidate: number) => candidate === generation,
  };
};

export const createQRLoginPoller = (
  timers: QRLoginPollerTimers = {
    setInterval: globalThis.setInterval.bind(globalThis),
    clearInterval: globalThis.clearInterval.bind(globalThis),
  },
) => {
  let interval: ReturnType<typeof setInterval> | null = null;
  let requestController: AbortController | null = null;
  let inFlightGeneration = -1;
  let generation = 0;

  const stop = () => {
    generation += 1;
	requestController?.abort();
	requestController = null;
    if (interval !== null) {
      timers.clearInterval(interval);
      interval = null;
    }
  };

  const start = (
    sessionId: string,
	checkStatus: (sessionId: string, signal?: AbortSignal) => Promise<QRLoginStatusResult>,
    handlers: QRLoginPollHandlers,
    intervalMs = 2000,
  ) => {
    stop();
    const currentGeneration = generation;
	requestController = new AbortController();
	const signal = requestController.signal;
    interval = timers.setInterval(() => {
      if (inFlightGeneration === currentGeneration || currentGeneration !== generation) return;
      inFlightGeneration = currentGeneration;
      void (async () => {
        try {
		  const statusRes = await checkStatus(sessionId, signal);
          if (statusRes.status === 'success') {
            stop();
            await handlers.onSuccess(statusRes);
            return;
          }
          if (statusRes.status === 'scanned') {
            handlers.onScanned?.(statusRes);
            return;
          }
          if (statusRes.status === 'verification_required') {
            handlers.onVerificationRequired?.(statusRes);
            return;
          }
          if (statusRes.status && terminalQRStatuses.has(statusRes.status)) {
            stop();
            handlers.onTerminalError(statusRes);
          }
        } catch (error) {
		  if (signal.aborted || currentGeneration !== generation) return;
          stop();
          handlers.onPollError(error);
        } finally {
          if (inFlightGeneration === currentGeneration) inFlightGeneration = -1;
        }
      })();
    }, intervalMs);
  };

  return {
    start,
    stop,
    isActive: () => interval !== null,
  };
};
