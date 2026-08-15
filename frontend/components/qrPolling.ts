// 旧组件路径保留兼容导出，实际实现归属于账号 feature。
export {
  createLatestRequestGate,
  createQRLoginPoller,
} from '../app/features/accounts/qrPolling';
export type {
  QRLoginPollHandlers,
  QRLoginStatusResult,
} from '../app/features/accounts/qrPolling';
