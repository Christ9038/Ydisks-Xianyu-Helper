export interface QRLoginStatusResult {
  /** status 表示状态。 */ status?: string;
  /** cookies 表示登录凭证字符串。 */ cookies?: string;
  /** unb 表示闲鱼账号的用户标识。 */ unb?: string;
  /** verification_url 表示风控验证地址。 */ verification_url?: string;
  /** verification_screenshot 表示风控验证截图地址。 */ verification_screenshot?: string;
  /** face_qr_url 表示人脸验证二维码地址。 */ face_qr_url?: string;
}

interface QRLoginPollerTimers {
  /** setInterval 表示创建轮询定时器的函数。 */ setInterval: (handler: () => void, timeout: number) => ReturnType<typeof setInterval>;
  /** clearInterval 表示清理轮询定时器的函数。 */ clearInterval: (id: ReturnType<typeof setInterval>) => void;
}

export interface QRLoginPollHandlers {
  /** onSuccess 表示二维码登录成功后的回调。 */ onSuccess: (status: QRLoginStatusResult) => void | Promise<void>;
  /** onScanned 表示二维码被扫描后的回调。 */ onScanned?: (status: QRLoginStatusResult) => void;
  /** onVerificationRequired 表示需要风控验证时的回调。 */ onVerificationRequired?: (status: QRLoginStatusResult) => void;
  /** onTerminalError 表示登录流程终止错误的回调。 */ onTerminalError: (status: QRLoginStatusResult) => void;
  /** onPollError 表示轮询请求失败的回调。 */ onPollError: (error: unknown) => void;
}

// terminalQRStatuses 二维码登录终止状态集合。
const terminalQRStatuses = new Set(['expired', 'cancelled', 'error', 'not_found']);

// createLatestRequestGate 让只能由“最后一次用户操作”提交结果的异步请求拥有
// 明确代次。cancel 会使所有尚未返回的请求失效，但不要求底层 fetch 支持中断。
export const createLatestRequestGate = () => {
  // generation 请求代次。
  let generation = 0;
  return {
    next: /* 当前回调处理用户交互或异步状态变化。 */ () => {
      generation += 1;
      return generation;
    },
    cancel: /* 当前回调处理用户交互或异步状态变化。 */ () => {
      generation += 1;
    },
    isCurrent: /* 当前回调处理用户交互或异步状态变化。 */ (candidate: number) => candidate === generation,
  };
};

// createQRLoginPoller 创建二维码登录轮询器。
export const createQRLoginPoller = (
  timers: QRLoginPollerTimers = {
    setInterval: globalThis.setInterval.bind(globalThis),
    clearInterval: globalThis.clearInterval.bind(globalThis),
  },
) => {
  // interval 轮询间隔。
  let interval: ReturnType<typeof setInterval> | null = null;
  // requestController 请求取消控制器。
  let requestController: AbortController | null = null;
  // inFlightGeneration 正在执行的请求代次。
  let inFlightGeneration = -1;
  // generation 请求代次。
  let generation = 0;

  // stop 停止函数。
  const stop = () => {
    generation += 1;
	requestController?.abort();
	requestController = null;
    if (interval !== null) {
      timers.clearInterval(interval);
      interval = null;
    }
  };

  // start 启动函数。
  const start = (
    sessionId: string,
	checkStatus: (sessionId: string, signal?: AbortSignal) => Promise<QRLoginStatusResult>,
    handlers: QRLoginPollHandlers,
    intervalMs = 2000,
  ) => {
    stop();
    // currentGeneration 当前请求代次，负责当前功能中的对应处理。
    const currentGeneration = generation;
	requestController = new AbortController();
	// signal signal，负责当前功能中的对应处理。
	const signal = requestController.signal;
    interval = timers.setInterval(/* 当前回调处理用户交互或异步状态变化。 */ () => {
      if (inFlightGeneration === currentGeneration || currentGeneration !== generation) return;
      inFlightGeneration = currentGeneration;
      void (/* 当前回调处理用户交互或异步状态变化。 */ async () => {
        try {
		  // statusRes 状态Res，负责当前功能中的对应处理。
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
        } catch (/* error 表示错误。 */ error) {
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
    isActive: /* 当前回调处理用户交互或异步状态变化。 */ () => interval !== null,
  };
};
