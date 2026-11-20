import { useCallback,useEffect,useRef,useState } from 'react';
import { getFeishuAppBindingStatus,startFeishuAppBinding } from './api';
import { notificationErrorMessage } from './state';

/** FeishuBindingStatus 是飞书机器人扫码绑定弹窗的阶段；closed 表示弹窗未打开，starting 表示等待用户点击或正在创建会话。 */
export type FeishuBindingStatus = 'closed' | 'starting' | 'waiting' | 'success' | 'expired' | 'error';

/** FeishuBindingOptions 描述飞书扫码绑定 Hook 的页面回调。 */
export interface FeishuBindingOptions {
  /** onSuccess 在绑定成功后接收后端创建或更新的通知渠道主键，供页面刷新渠道列表。 */
  onSuccess?: (channelID: number) => void | Promise<void>;
  /** pollIntervalMs 是扫码状态轮询间隔，默认两秒；测试可注入更短间隔。 */
  pollIntervalMs?: number;
}

/** FeishuBindingState 是飞书扫码绑定 Hook 暴露给弹窗和页面的状态与动作。 */
export interface FeishuBindingState {
  /** status 是当前绑定阶段，closed 时弹窗不渲染。 */
  status: FeishuBindingStatus;
  /** qrCodeUrl 是后端返回的飞书扫码二维码 data URL；非等待阶段为空。 */
  qrCodeUrl: string;
  /** message 是后端返回或本地归一的状态说明，用于错误和过期提示。 */
  message: string;
  /** sessionID 是当前绑定会话标识；轮询期间保持后端返回值。 */
  sessionID: string;
  /** bindAllAccounts 表示绑定成功后是否把新渠道绑定到当前用户全部账号，默认开启。 */
  bindAllAccounts: boolean;
  /** notifyWarning 表示新渠道是否推送警告类通知，默认开启。 */
  notifyWarning: boolean;
  /** notifyInfo 表示新渠道是否推送普通提示类通知，默认开启。 */
  notifyInfo: boolean;
  /** requesting 表示创建绑定会话的请求是否仍在进行，用于禁用开始按钮和展示加载状态。 */
  requesting: boolean;
  /** setBindAllAccounts 更新“绑定全部账号”选项；弹窗在开始绑定后禁用该开关。 */
  setBindAllAccounts: (value: boolean) => void;
  /** setNotifyWarning 更新警告类通知开关；弹窗在开始绑定后禁用该开关。 */
  setNotifyWarning: (value: boolean) => void;
  /** setNotifyInfo 更新普通提示类通知开关；弹窗在开始绑定后禁用该开关。 */
  setNotifyInfo: (value: boolean) => void;
  /** open 打开弹窗并把状态重置为待开始，不发起网络请求。 */
  open: () => void;
  /** close 关闭弹窗并取消进行中的请求和轮询。 */
  close: () => void;
  /** start 使用当前三个设置开关创建扫码会话并开始轮询。 */
  start: () => Promise<void>;
}

// defaultPollIntervalMs 是扫码状态轮询的默认间隔，避免高频请求后端绑定会话。
const defaultPollIntervalMs = 2000;

// useFeishuAppBinding 管理飞书机器人扫码绑定的弹窗、会话创建、两秒轮询和取消边界。
export const useFeishuAppBinding = (options: FeishuBindingOptions = {}): FeishuBindingState => {
  // status 保存弹窗阶段；closed 表示未打开，starting 同时承载待开始和请求进行中两种展示。
  const [status, setStatus] = useState<FeishuBindingStatus>('closed');
  // qrCodeUrl 保存当前可展示的飞书扫码二维码地址。
  const [qrCodeUrl, setQrCodeUrl] = useState('');
  // message 保存后端返回的阶段说明或本地错误文案。
  const [message, setMessage] = useState('');
  // sessionID 保存当前绑定会话标识，关闭弹窗时清空。
  const [sessionID, setSessionID] = useState('');
  // bindAllAccounts 保存用户的账号绑定选项；开始新会话时以调用参数为准。
  const [bindAllAccounts, setBindAllAccountsState] = useState(true);
  // notifyWarning 保存警告类通知开关，默认开启并与后端 default true 保持一致。
  const [notifyWarning, setNotifyWarningState] = useState(true);
  // notifyInfo 保存普通提示类通知开关，默认开启并与后端 default true 保持一致。
  const [notifyInfo, setNotifyInfoState] = useState(true);
  // requesting 保存会话创建请求是否仍在进行，与 status starting 组合区分待开始和请求中。
  const [requesting, setRequesting] = useState(false);
  // generationRef 保存绑定流程代次；关闭、重新开始或卸载后旧响应不能写回状态。
  const generationRef = useRef(0);
  // abortRef 保存当前绑定流程的取消控制器，覆盖会话创建和随后的状态轮询。
  const abortRef = useRef<AbortController | null>(null);
  // pollTimerRef 保存下一次状态轮询的定时器句柄，关闭或终态时清理。
  const pollTimerRef = useRef<number | null>(null);
  // onSuccessRef 保存最新的绑定成功回调，避免轮询闭包一直引用首次渲染的页面回调。
  const onSuccessRef = useRef(options.onSuccess);

  useEffect(
    // onSuccessSync 在页面回调变化时同步最新引用，轮询完成时使用当前回调。
    () => {
      onSuccessRef.current = options.onSuccess;
    },
    [options.onSuccess],
  );

  // clearPollTimer 清理待执行的轮询定时器；重复调用保持幂等。
  const clearPollTimer = useCallback(
    // clearTimerCallback 在关闭或终态时释放尚未触发的轮询。
    () => {
      if (pollTimerRef.current !== null) {
        window.clearTimeout(pollTimerRef.current);
        pollTimerRef.current = null;
      }
    },
    [],
  );

  // stopFlow 取消当前请求和轮询、递增代次，并使所有晚到的异步结果失效。
  const stopFlow = useCallback(
    // stopFlowCallback 取消请求、递增代次并清理定时器。
    () => {
      generationRef.current += 1;
      abortRef.current?.abort();
      abortRef.current = null;
      clearPollTimer();
    },
    [clearPollTimer],
  );

  // pollBinding 按会话代次查询一次状态；waiting 时安排下一次查询，终态或取消时停止。
  const pollBinding = useCallback(
    // pollCallback 查询当前扫码阶段并按结果继续或终止轮询。
    async (
    // sessionId 是本次轮询对应的后端绑定会话标识。
    sessionId: string,
    // generation 是启动本次轮询流程时捕获的代次。
    generation: number,
    // signal 是绑定流程共享的取消信号；关闭弹窗或重新开始会中止它。
    signal: AbortSignal,
    // intervalMs 是当前流程使用的轮询间隔。
    intervalMs: number,
  ) => {
    if (generation !== generationRef.current || signal.aborted) return;
    try {
      // result 是当前扫码绑定的阶段快照。
      const result = await getFeishuAppBindingStatus(sessionId, { signal });
      if (generation !== generationRef.current || signal.aborted) return;
      if (result.status === 'success') {
        stopFlow();
        setStatus('success');
        setMessage(result.message || '飞书机器人绑定成功');
        setRequesting(false);
        if (typeof result.channel_id === 'number') {
          // successHandlerResult 是页面成功回调的返回值；回调失败不影响弹窗已经展示的成功状态。
          const successHandlerResult = onSuccessRef.current?.(result.channel_id);
          void Promise.resolve(successHandlerResult).catch(
            // successHandlerFailure 忽略页面刷新回调中的错误，避免覆盖绑定成功结果。
            () => undefined,
          );
        }
        return;
      }
      if (result.status === 'expired') {
        stopFlow();
        setStatus('expired');
        setMessage(result.message || '二维码已过期，请重新生成');
        setRequesting(false);
        return;
      }
      if (result.status === 'error') {
        stopFlow();
        setStatus('error');
        setMessage(result.message || '扫码绑定失败，请重试');
        setRequesting(false);
        return;
      }
      // waiting 阶段同步后端说明，并安排下一次状态查询；递归定时器保证请求不会重叠。
      setMessage(result.message || '');
      pollTimerRef.current = window.setTimeout(
        // nextPoll 在下一次轮询间隔到达时再次查询绑定状态。
        () => {
          pollTimerRef.current = null;
          void pollBinding(sessionId, generation, signal, intervalMs);
        },
        intervalMs,
      );
    } catch (error: unknown /* 状态轮询异常表示请求或网络失败，需要终止等待并允许用户重试。 */) {
      if (generation !== generationRef.current || signal.aborted) return;
      stopFlow();
      setStatus('error');
      setMessage(notificationErrorMessage(error, '获取扫码状态失败，请重试'));
      setRequesting(false);
    }
  }, [stopFlow]);

  // start 开始飞书扫码绑定，保存二维码并启动状态轮询；重复调用会先取消上一轮流程。
  const start = useCallback(
    // startCallback 使用当前绑定范围与分类开关创建扫码会话。
    async () => {
    stopFlow();
    // generation 是本次绑定流程的代次，用于拒绝旧请求的晚到响应。
    const generation = generationRef.current;
    // controller 允许关闭弹窗或开始新会话时中止会话创建和轮询请求。
    const controller = new AbortController();
    abortRef.current = controller;
    setStatus('starting');
    setRequesting(true);
    setQrCodeUrl('');
    setMessage('');
    setSessionID('');
    try {
      // result 是后端创建的扫码绑定会话，包含二维码和有效时长。
      const result = await startFeishuAppBinding({ bindAllAccounts, notifyWarning, notifyInfo }, { signal: controller.signal });
      if (generation !== generationRef.current || controller.signal.aborted) return;
      setQrCodeUrl(result.qr_code_url);
      setSessionID(result.session_id);
      setStatus('waiting');
      setRequesting(false);
      // intervalMs 是本次流程固定的轮询间隔，避免运行中受到外部选项变化影响。
      const intervalMs = options.pollIntervalMs ?? defaultPollIntervalMs;
      pollTimerRef.current = window.setTimeout(
        // firstPoll 在首个轮询间隔后查询扫码状态。
        () => {
          pollTimerRef.current = null;
          void pollBinding(result.session_id, generation, controller.signal, intervalMs);
        },
        intervalMs,
      );
    } catch (error: unknown /* 会话创建失败时保留弹窗供用户重试。 */) {
      if (generation !== generationRef.current || controller.signal.aborted) return;
      stopFlow();
      setStatus('error');
      setMessage(notificationErrorMessage(error, '创建飞书绑定二维码失败'));
      setRequesting(false);
    }
  }, [bindAllAccounts, notifyInfo, notifyWarning, options.pollIntervalMs, pollBinding, stopFlow]);

  // open 打开扫码绑定弹窗并清理上一次会话的二维码和错误状态。
  const open = useCallback(
    // openCallback 把弹窗重置到等待用户开始的阶段。
    () => {
      stopFlow();
      setStatus('starting');
      setRequesting(false);
      setQrCodeUrl('');
      setMessage('');
      setSessionID('');
      setBindAllAccountsState(true);
      setNotifyWarningState(true);
      setNotifyInfoState(true);
    },
    [stopFlow],
  );

  // close 关闭弹窗、取消进行中的请求和轮询并重置所有临时状态。
  const close = useCallback(
    // closeCallback 取消绑定流程并隐藏弹窗。
    () => {
      stopFlow();
      setStatus('closed');
      setRequesting(false);
      setQrCodeUrl('');
      setMessage('');
      setSessionID('');
      setBindAllAccountsState(true);
      setNotifyWarningState(true);
      setNotifyInfoState(true);
    },
    [stopFlow],
  );

  useEffect(
    // cleanupEffect 在 Hook 卸载时停止请求与轮询，防止组件销毁后继续更新状态。
    () => stopFlow,
    [stopFlow],
  );

  // updateBindAllAccounts 写入用户在开始绑定前选择的账号绑定选项。
  const updateBindAllAccounts = useCallback(
    // updateCallback 把开关状态写入本地选项，请求参数在开始绑定时确定。
    (value: boolean) => {
      setBindAllAccountsState(value);
    },
    [],
  );

  // updateNotifyWarning 写入警告类通知开关，开始绑定时随会话请求提交。
  const updateNotifyWarning = useCallback(
    // warningCallback 保存警告类通知的本地选择。
    (value: boolean) => {
      setNotifyWarningState(value);
    },
    [],
  );

  // updateNotifyInfo 写入普通提示类通知开关，开始绑定时随会话请求提交。
  const updateNotifyInfo = useCallback(
    // infoCallback 保存普通提示类通知的本地选择。
    (value: boolean) => {
      setNotifyInfoState(value);
    },
    [],
  );

  return {
    status, qrCodeUrl, message, sessionID, bindAllAccounts, notifyWarning, notifyInfo, requesting,
    setBindAllAccounts: updateBindAllAccounts, setNotifyWarning: updateNotifyWarning, setNotifyInfo: updateNotifyInfo, open, close, start,
  };
};
