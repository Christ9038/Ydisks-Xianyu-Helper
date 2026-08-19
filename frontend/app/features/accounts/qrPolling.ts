import type { QRLoginStatusResult } from './api';

// QRLoginPollerTimers 描述轮询器使用的定时器依赖，便于测试替换时间源。
interface QRLoginPollerTimers {
  // setInterval 表示创建轮询定时器的函数。
  setInterval: (handler: () => void, timeout: number) => ReturnType<typeof setInterval>;
  // clearInterval 表示清理轮询定时器的函数。
  clearInterval: (id: ReturnType<typeof setInterval>) => void;
}

// QRLoginPollHandlers 描述二维码轮询的状态回调契约。
export interface QRLoginPollHandlers {
  // onSuccess 表示二维码登录成功后的回调。
  onSuccess: (status: QRLoginStatusResult) => void | Promise<void>;
  // onScanned 表示二维码被扫描后的回调。
  onScanned?: (status: QRLoginStatusResult) => void;
  // onVerificationRequired 表示需要风控验证时的回调。
  onVerificationRequired?: (status: QRLoginStatusResult) => void;
  // onTerminalError 表示登录流程终止错误的回调。
  onTerminalError: (status: QRLoginStatusResult) => void;
  // onPollError 表示轮询请求失败的回调。
  onPollError: (error: unknown) => void;
}

// terminalQRStatuses 保存二维码登录不可继续的终止状态集合。
const terminalQRStatuses = new Set(['expired', 'cancelled', 'error', 'not_found']);

// createLatestRequestGate 让只能由最后一次用户操作提交结果的异步请求拥有明确代次。
export const createLatestRequestGate = () => {
  // generation 保存当前请求代次。
  let generation = 0;
  return {
    // next 生成下一次请求代次并使旧请求失效。
    next: () => {
      generation += 1;
      return generation;
    },
    // cancel 让所有尚未返回的请求失效。
    cancel: () => {
      generation += 1;
    },
    // isCurrent 判断候选请求是否仍属于最新代次。
    isCurrent: /* currentCheck 判断候选请求是否仍属于最新代次。 */ (candidate: number) => candidate === generation,
  };
};

// createQRLoginPoller 创建二维码登录轮询器，并隔离请求重叠和过期回调。
export const createQRLoginPoller = (
  timers: QRLoginPollerTimers = {
    setInterval: globalThis.setInterval.bind(globalThis),
    clearInterval: globalThis.clearInterval.bind(globalThis),
  },
) => {
  // interval 保存当前轮询定时器句柄。
  let interval: ReturnType<typeof setInterval> | null = null;
  // requestController 保存当前轮询请求的取消控制器。
  let requestController: AbortController | null = null;
  // inFlightGeneration 保存正在执行请求的代次，避免请求重叠。
  let inFlightGeneration = -1;
  // generation 保存轮询器当前生命周期代次。
  let generation = 0;

  // stop 停止轮询、取消正在执行的请求并使旧回调失效。
  const stop = () => {
    generation += 1;
    requestController?.abort();
    requestController = null;
    if (interval !== null) {
      timers.clearInterval(interval);
      interval = null;
    }
  };

  // start 启动指定会话的二维码状态轮询。
  const start = (
    sessionId: string,
    checkStatus: (sessionId: string, signal?: AbortSignal) => Promise<QRLoginStatusResult>,
    handlers: QRLoginPollHandlers,
    intervalMs = 2000,
  ) => {
    stop();
    // currentGeneration 保存本次轮询生命周期代次。
    const currentGeneration = generation;
    requestController = new AbortController();
    // signal 保存本次轮询请求使用的取消信号。
    const signal = requestController.signal;
    interval = timers.setInterval(/* 当前回调触发二维码状态检查。 */ () => {
      if (inFlightGeneration === currentGeneration || currentGeneration !== generation) return;
      inFlightGeneration = currentGeneration;
      void (/* 当前回调执行一次二维码状态检查并分发结果。 */ async () => {
        try {
          // statusResponse 保存二维码状态接口响应。
          const statusResponse = await checkStatus(sessionId, signal);
          if (statusResponse.status === 'success') {
            stop();
            await handlers.onSuccess(statusResponse);
            return;
          }
          if (statusResponse.status === 'scanned') {
            handlers.onScanned?.(statusResponse);
            return;
          }
          if (statusResponse.status === 'verification_required') {
            handlers.onVerificationRequired?.(statusResponse);
            return;
          }
          if (statusResponse.status && terminalQRStatuses.has(statusResponse.status)) {
            stop();
            handlers.onTerminalError(statusResponse);
          }
        } catch (/* error 表示二维码状态请求错误。 */ error) {
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
    // isActive 判断轮询器当前是否仍有活动定时器。
    isActive: /* activeCheck 判断轮询器当前是否仍有活动定时器。 */ () => interval !== null,
  };
};
