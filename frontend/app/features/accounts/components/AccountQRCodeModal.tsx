import React from 'react';
import { createPortal } from 'react-dom';
import { Check, Loader2, X } from 'lucide-react';
import type { AccountDetail } from '../../../../types';
import { RiskVerificationPanel } from '../../../../components/RiskVerificationPanel';
import { SquareQRCode } from '../../../../components/SquareQRCode';

// AccountQRCodeModalProps 描述二维码登录弹窗的展示状态和关闭回调。
export interface AccountQRCodeModalProps {
  // target 是重新授权时的目标账号，新增账号时为空。
  target: AccountDetail | null;
  // status 是二维码登录当前阶段。
  status: string;
  // codeUrl 是待扫描的二维码地址。
  codeUrl: string;
  // errorMessage 是二维码生成或轮询失败提示。
  errorMessage: string;
  // faceQrUrl 是风控人脸验证二维码地址。
  faceQrUrl: string;
  // verificationScreenshot 是风控验证截图地址。
  verificationScreenshot: string;
  // onClose 关闭二维码登录弹窗并取消后台请求。
  onClose: () => void;
}

// AccountQRCodeModal 渲染二维码登录、风控验证和结果状态。
export const AccountQRCodeModal: React.FC<AccountQRCodeModalProps> = ({ target, status, codeUrl, errorMessage, faceQrUrl, verificationScreenshot, onClose }) => createPortal(
  <div className="modal-overlay-centered">
    <div className="modal-container relative" style={{ maxWidth: '26rem' }}>
      <button onClick={onClose} className="absolute top-4 right-4 z-10 w-9 h-9 flex items-center justify-center bg-gray-100 hover:bg-gray-200 rounded-full transition-colors" aria-label="关闭二维码登录">
        <X className="w-5 h-5 text-gray-600" />
      </button>
      <div className="modal-body">
        <div className="text-center">
          <h3 className="text-2xl font-extrabold text-gray-900 mb-2">{target ? '重新授权账号' : '扫码添加账号'}</h3>
          <p className="text-gray-500 mb-8 font-medium">{target ? `请用闲鱼APP扫码，为「${target.nickname || target.remark || target.id}」刷新授权` : '请打开闲鱼APP扫描下方二维码'}</p>
          <div className={`w-full bg-surface-subtle rounded-xl mx-auto flex items-center justify-center overflow-hidden border-4 border-white shadow-inner mb-6 relative ${status === 'verification' ? 'max-w-72 min-h-64 h-auto p-2' : 'max-w-64 aspect-square'}`}>
            {status === 'loading' && <Loader2 className="w-10 h-10 text-brand animate-spin" />}
            {status === 'waiting' && <SquareQRCode src={codeUrl} alt="闲鱼登录二维码" className="p-2" />}
            {status === 'success' && (
              <div className="absolute inset-0 bg-white/95 flex flex-col items-center justify-center text-green-600 animate-fade-in">
                <div className="w-16 h-16 bg-green-100 rounded-full flex items-center justify-center mb-4"><Check className="w-8 h-8" /></div>
                <span className="font-bold text-lg">登录成功</span>
              </div>
            )}
            {status === 'error' && <div className="px-5 text-center"><span className="block text-red-600 font-bold">二维码获取失败</span><span className="mt-2 block text-xs leading-5 text-gray-500">{errorMessage || '请关闭窗口后重新发起扫码登录。'}</span></div>}
            {status === 'verification' && <RiskVerificationPanel faceQrUrl={faceQrUrl} verificationScreenshot={verificationScreenshot} />}
          </div>
          {status !== 'verification' && <p className="text-xs text-gray-400 font-medium bg-gray-50 py-2 rounded-xl">二维码有效期为5分钟，请尽快扫码。</p>}
        </div>
      </div>
    </div>
  </div>,
  document.body,
);
