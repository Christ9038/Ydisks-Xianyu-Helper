import { useEffect,useRef,useState } from 'react';
import type { AccountDetail } from './api';
import { checkQRLoginStatus,completeQRVerification,generateQRLogin } from './api';
import { createLatestRequestGate,createQRLoginPoller } from './qrPolling';

// AccountQRCodeLoginOptions 描述二维码登录协调器需要的页面刷新回调。
export interface AccountQRCodeLoginOptions {
  // onLoginSuccess 表示二维码授权成功后刷新账号列表的回调。
  onLoginSuccess: () => void | Promise<void>;
}

// AccountQRCodeLoginState 描述二维码授权弹窗和异步流程的页面状态。
export interface AccountQRCodeLoginState {
  // showQRModal 表示二维码登录弹窗是否打开。
  showQRModal: boolean;
  // qrCodeUrl 保存当前二维码登录地址。
  qrCodeUrl: string;
  // qrStatus 保存二维码登录流程状态。
  qrStatus: string;
  // qrErrorMessage 保存二维码登录错误说明。
  qrErrorMessage: string;
  // verificationScreenshot 保存风控验证截图地址。
  verificationScreenshot: string;
  // faceQrUrl 保存人脸验证二维码地址。
  faceQrUrl: string;
  // qrReauthTarget 保存需要重新授权的目标账号。
  qrReauthTarget: AccountDetail | null;
  // startQRLogin 启动二维码登录或账号重新授权流程。
  startQRLogin: (target?: AccountDetail) => Promise<void>;
  // closeQRModal 关闭二维码登录弹窗并取消相关异步资源。
  closeQRModal: () => void;
}

// useAccountQRCodeLogin 统一管理二维码生成、轮询、风控验证和资源取消生命周期。
export const useAccountQRCodeLogin = ({ onLoginSuccess }: AccountQRCodeLoginOptions): AccountQRCodeLoginState => {
  // showQRModal 表示二维码登录弹窗是否打开。
  const [showQRModal, setShowQRModal] = useState(false);
  // qrCodeUrl 保存当前二维码登录地址。
  const [qrCodeUrl, setQrCodeUrl] = useState('');
  // qrStatus 保存二维码登录流程状态。
  const [qrStatus, setQrStatus] = useState('pending');
  // qrErrorMessage 保存二维码登录错误说明。
  const [qrErrorMessage, setQrErrorMessage] = useState('');
  // verificationScreenshot 保存风控验证截图地址。
  const [verificationScreenshot, setVerificationScreenshot] = useState('');
  // faceQrUrl 保存人脸验证二维码地址。
  const [faceQrUrl, setFaceQrUrl] = useState('');
  // qrReauthTarget 保存需要重新授权的目标账号。
  const [qrReauthTarget, setQrReauthTarget] = useState<AccountDetail | null>(null);
  // qrPollerRef 保存二维码登录状态轮询器实例。
  const qrPollerRef = useRef<ReturnType<typeof createQRLoginPoller> | null>(null);
  // qrRequestGateRef 隔离过期的二维码生成请求。
  const qrRequestGateRef = useRef<ReturnType<typeof createLatestRequestGate> | null>(null);
  // qrGenerateAbortRef 保存二维码生成请求控制器。
  const qrGenerateAbortRef = useRef<AbortController | null>(null);
  // qrCloseTimerRef 保存二维码弹窗延迟关闭定时器。
  const qrCloseTimerRef = useRef<number | null>(null);

  if (qrPollerRef.current === null) {
    qrPollerRef.current = createQRLoginPoller();
  }
  if (qrRequestGateRef.current === null) {
    qrRequestGateRef.current = createLatestRequestGate();
  }

  // clearQRCloseTimer 清理二维码弹窗延迟关闭定时器。
  const clearQRCloseTimer = () => {
    if (qrCloseTimerRef.current !== null) {
      window.clearTimeout(qrCloseTimerRef.current);
      qrCloseTimerRef.current = null;
    }
  };

  // stopQRPolling 停止二维码登录状态轮询。
  const stopQRPolling = () => {
    qrPollerRef.current?.stop();
  };

  // closeQRModal 关闭二维码登录弹窗并取消请求、轮询和延迟任务。
  const closeQRModal = () => {
    qrGenerateAbortRef.current?.abort();
    qrRequestGateRef.current?.cancel();
    stopQRPolling();
    clearQRCloseTimer();
    setShowQRModal(false);
  };

  // scheduleQRModalClose 在登录成功后延迟关闭二维码弹窗并刷新账号列表。
  const scheduleQRModalClose = () => {
    clearQRCloseTimer();
    qrCloseTimerRef.current = window.setTimeout(/* 当前回调处理二维码成功后的延迟收束。 */ () => {
      qrCloseTimerRef.current = null;
      setShowQRModal(false);
      void onLoginSuccess();
    }, 1000);
  };

  useEffect(/* 当前回调管理二维码异步资源的页面生命周期。 */ () => {
    // cleanup 清理页面卸载时仍存在的二维码请求、轮询和定时器。
    const cleanup = () => {
      stopQRPolling();
      qrRequestGateRef.current?.cancel();
      qrGenerateAbortRef.current?.abort();
      clearQRCloseTimer();
    };
    return cleanup;
  }, []);

  // completeAndPersistQRSession 完成二维码风控验证并持久化授权结果。
  const completeAndPersistQRSession = async (sessionId: string, target?: AccountDetail | null): Promise<string> => {
    // response 保存风控验证完成接口返回值。
    const response = await completeQRVerification(sessionId, target?.id);
    if (!response.success || !response.account_id) {
      throw new Error(response.message || '保存扫码授权失败');
    }
    return response.account_id;
  };

  // startQRLogin 启动二维码生成、轮询和成功后的账号持久化流程。
  const startQRLogin = async (target?: AccountDetail): Promise<void> => {
    stopQRPolling();
    qrGenerateAbortRef.current?.abort();
    // controller 控制当前二维码生成请求的取消。
    const controller = new AbortController();
    qrGenerateAbortRef.current = controller;
    // requestGeneration 标识当前二维码生成请求代次。
    const requestGeneration = qrRequestGateRef.current!.next();
    clearQRCloseTimer();
    // targetAccount 保存当前二维码授权目标账号。
    const targetAccount = target || null;
    setQrReauthTarget(targetAccount);
    setShowQRModal(true);
    setQrStatus('loading');
    setQrErrorMessage('');
    setQrCodeUrl('');
    setVerificationScreenshot('');
    setFaceQrUrl('');
    try {
      // response 保存二维码生成接口返回值。
      const response = await generateQRLogin({ signal: controller.signal });
      if (!qrRequestGateRef.current?.isCurrent(requestGeneration)) return;
      if (!response.success || !response.qr_code_url || !response.session_id) {
        throw new Error(response.message || '闲鱼未返回可用的登录二维码');
      }

      // sessionId 保存后端生成的二维码登录会话标识。
      const sessionId = response.session_id;
      setQrCodeUrl(response.qr_code_url);
      setQrStatus('waiting');
      qrPollerRef.current?.start(sessionId, checkQRLoginStatus, {
        onSuccess: /* 当前回调处理二维码登录成功结果。 */ async () => {
          try {
            await completeAndPersistQRSession(sessionId, targetAccount);
          } catch (/* error 表示二维码授权结果持久化错误。 */ error) {
            console.error('保存扫码授权失败', error);
            setQrStatus('error');
            return;
          }
          setQrStatus('success');
          scheduleQRModalClose();
        },
        onScanned: /* 当前回调处理二维码已扫描状态。 */ () => {
          setQrStatus('waiting');
        },
        onTerminalError: /* 当前回调处理二维码终止状态。 */ () => {
          setQrStatus('error');
        },
        onPollError: /* 当前回调处理二维码轮询错误。 */ error => {
          console.error('轮询扫码状态失败', error);
          setQrStatus('error');
        },
        onVerificationRequired: /* 当前回调处理二维码风控验证状态。 */ statusResponse => {
          setQrStatus('verification');
          if (statusResponse.face_qr_url) setFaceQrUrl(statusResponse.face_qr_url);
          if (statusResponse.verification_screenshot) setVerificationScreenshot(statusResponse.verification_screenshot);
        },
      });
    } catch (/* error 表示二维码生成或请求取消错误。 */ error) {
      if (!qrRequestGateRef.current?.isCurrent(requestGeneration)) return;
      if (controller.signal.aborted) return;
      setQrErrorMessage(error instanceof Error ? error.message : '二维码获取失败，请稍后重试');
      setQrStatus('error');
    } finally {
      if (qrGenerateAbortRef.current === controller) qrGenerateAbortRef.current = null;
    }
  };

  return {
    showQRModal,
    qrCodeUrl,
    qrStatus,
    qrErrorMessage,
    verificationScreenshot,
    faceQrUrl,
    qrReauthTarget,
    startQRLogin,
    closeQRModal,
  };
};
