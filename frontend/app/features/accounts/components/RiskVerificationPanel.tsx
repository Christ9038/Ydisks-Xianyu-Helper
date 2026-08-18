import { AlertTriangle,Loader2 } from 'lucide-react';
import React from 'react';

interface RiskVerificationPanelProps {
  /** faceQrUrl 表示人脸验证二维码地址。 */ faceQrUrl?: string;
  /** verificationScreenshot 表示验证失败截图地址。 */ verificationScreenshot?: string;
}

// RiskVerificationPanel 渲染风控验证面板。
export const RiskVerificationPanel: React.FC<RiskVerificationPanelProps> = ({
  faceQrUrl,
  verificationScreenshot,
}) => (
  <div className="w-full min-w-0 max-h-[min(64vh,28rem)] overflow-y-auto overscroll-contain px-1 py-2 text-center">
    <div className="mx-auto mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-amber-100 text-amber-700">
      <AlertTriangle className="h-5 w-5" aria-hidden="true" />
    </div>
    <h4 className="break-words text-base font-extrabold leading-6 text-amber-900">
      需要完成安全风控验证
    </h4>
    <p className="mx-auto mt-1 max-w-[15rem] break-words text-xs leading-5 text-amber-800">
      当前账号触发了闲鱼平台风控。请使用手机闲鱼 App 扫描下方二维码，并按 App 提示完成人脸识别。
    </p>

    <div className="mx-auto mt-3 flex min-h-36 w-full max-w-[12rem] items-center justify-center overflow-hidden rounded-xl border border-amber-200 bg-white p-2 shadow-sm">
      {faceQrUrl ? (
        <img src={faceQrUrl} alt="闲鱼安全风控验证二维码" className="block max-h-44 w-full object-contain" />
      ) : verificationScreenshot ? (
        <img src={verificationScreenshot} alt="闲鱼风控验证页面" className="block max-h-44 w-full object-contain" />
      ) : (
        <div className="flex flex-col items-center gap-2 text-amber-700">
          <Loader2 className="h-6 w-6 animate-spin" aria-hidden="true" />
          <span className="text-xs font-bold">正在准备风控二维码…</span>
        </div>
      )}
    </div>

    <p className="mx-auto mt-2 max-w-[15rem] break-words text-[11px] leading-4 text-gray-500">
      请勿在浏览器中打开验证链接。完成验证后系统会自动检测并刷新登录状态，无需手动确认。
    </p>
  </div>
);
