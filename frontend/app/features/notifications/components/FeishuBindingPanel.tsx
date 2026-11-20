import React from 'react';
import { FeishuAppBindingModal } from './FeishuAppBindingModal';
import { useFeishuAppBinding } from '../feishuBinding';

/** FeishuBindingPanelProps 描述应用壳按需注入的飞书扫码绑定面板回调。 */
export interface FeishuBindingPanelProps {
  /** onSuccess 在绑定成功后接收渠道主键，供页面刷新渠道列表。 */
  onSuccess?: (channelID: number) => void;
  /** onClose 在用户关闭弹窗后释放页面侧打开状态。 */
  onClose: () => void;
}

/** FeishuBindingPanel 在用户主动打开时才加载扫码 Hook 与弹窗，避免通知设置页首屏承担这组交互代码。 */
export const FeishuBindingPanel: React.FC<FeishuBindingPanelProps> = ({ onSuccess, onClose }) => {
  // feishuBinding 管理扫码弹窗、二维码轮询和取消边界。
  const feishuBinding = useFeishuAppBinding({ onSuccess });
  // openedRef 保证面板挂载后只自动打开一次弹窗，不重复重置用户操作。
  const openedRef = React.useRef(false);

  React.useEffect(/* 当前副作用在按需面板首次挂载时打开扫码弹窗。 */ () => {
    if (openedRef.current) return;
    openedRef.current = true;
    feishuBinding.open();
  }, [feishuBinding.open]);

  /** handleClose 先取消绑定流程，再通知页面卸载当前面板。 */
  const handleClose = (): void => {
    feishuBinding.close();
    onClose();
  };

  return <FeishuAppBindingModal status={feishuBinding.status} qrCodeUrl={feishuBinding.qrCodeUrl} message={feishuBinding.message} bindAllAccounts={feishuBinding.bindAllAccounts} notifyWarning={feishuBinding.notifyWarning} notifyInfo={feishuBinding.notifyInfo} requesting={feishuBinding.requesting} onBindAllAccountsChange={feishuBinding.setBindAllAccounts} onNotifyWarningChange={feishuBinding.setNotifyWarning} onNotifyInfoChange={feishuBinding.setNotifyInfo} onStart={feishuBinding.start} onClose={handleClose} />;
};
