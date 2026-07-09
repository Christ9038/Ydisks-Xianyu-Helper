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

export const createQRLoginPoller = (
  timers: QRLoginPollerTimers = {
    setInterval: globalThis.setInterval.bind(globalThis),
    clearInterval: globalThis.clearInterval.bind(globalThis),
  },
) => {
  let interval: ReturnType<typeof setInterval> | null = null;

  const stop = () => {
    if (interval !== null) {
      timers.clearInterval(interval);
      interval = null;
    }
  };

  const start = (
    sessionId: string,
    checkStatus: (sessionId: string) => Promise<QRLoginStatusResult>,
    handlers: QRLoginPollHandlers,
    intervalMs = 2000,
  ) => {
    stop();
    interval = timers.setInterval(() => {
      void (async () => {
        try {
          const statusRes = await checkStatus(sessionId);
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
          stop();
          handlers.onPollError(error);
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
