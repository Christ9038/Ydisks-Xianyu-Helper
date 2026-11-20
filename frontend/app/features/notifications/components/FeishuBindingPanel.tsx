import React from 'react';
import { FeishuAppBindingView } from './FeishuAppBindingView';
import { useFeishuAppBinding,type FeishuBindingForm } from '../feishuBinding';

/** FeishuBindingPanelProps 描述应用壳按需注入的飞书扫码绑定面板回调。 */
export interface FeishuBindingPanelProps {
  /** form 是「新建渠道」弹窗当前的共用表单快照，点击“生成二维码”时随会话提交。 */
  form: FeishuBindingForm;
  /** onSuccess 在绑定成功后接收渠道主键，供页面刷新渠道列表。 */
  onSuccess?: (channelID: number) => void;
  /** onClose 在用户点击“关闭”或“完成”后通知所在的新建渠道弹窗收起。 */
  onClose: () => void;
  /** onLockChange 在扫码会话进行中或已成功时通知弹窗锁定共用表单，失败、过期或面板卸载时解锁。 */
  onLockChange?: (locked: boolean) => void;
}

/** FeishuBindingPanel 在用户于「新建渠道 → 飞书 → 扫码授权」中选择扫码时才加载扫码 Hook 与视图，避免通知设置页首屏承担这组交互代码；面板卸载（切换到链接方式、换类型或关闭弹窗）时自动取消进行中的绑定。 */
export const FeishuBindingPanel: React.FC<FeishuBindingPanelProps> = ({ form, onSuccess, onClose, onLockChange }) => {
  // feishuBinding 管理扫码阶段、二维码轮询和取消边界。
  const feishuBinding = useFeishuAppBinding({ onSuccess });
  // locked 表示共用表单是否应被锁定：请求中、等待扫码或已成功时不可再改，避免会话参数与界面不一致。
  const locked = feishuBinding.requesting || feishuBinding.status === 'waiting' || feishuBinding.status === 'success';

  React.useEffect(/* 当前副作用在面板挂载时进入扫码待开始阶段（open 幂等，StrictMode 重复挂载安全），卸载时取消绑定请求与轮询，保证切换方式或关闭弹窗后旧响应不再写回。 */ () => {
    feishuBinding.open();
    return feishuBinding.close;
  }, [feishuBinding.open, feishuBinding.close]);

  React.useEffect(/* 当前副作用把锁定状态同步给所在弹窗，卸载时一定解锁。 */ () => {
    onLockChange?.(locked);
    return () => onLockChange?.(false);
  }, [locked, onLockChange]);

  /** handleStart 以点击瞬间的表单快照创建扫码会话。 */
  const handleStart = (): Promise<void> => feishuBinding.start(form);

  return <FeishuAppBindingView status={feishuBinding.status} qrCodeUrl={feishuBinding.qrCodeUrl} message={feishuBinding.message} requesting={feishuBinding.requesting} onStart={handleStart} onClose={onClose} />;
};
