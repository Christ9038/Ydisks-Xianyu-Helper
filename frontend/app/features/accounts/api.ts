// 账号 feature 只通过本适配层访问账号相关网络接口，页面不再直接依赖共享 API 文件。
export {
  cancelPasswordLogin,
  checkPasswordLoginStatus,
  checkQRLoginStatus,
  completeQRVerification,
  deleteAccount,
  generateQRLogin,
  getAccountAISettings,
  getAccountBindings,
  getAccountDetails,
  getAccountRuntimeStatuses,
  getAllAISettings,
  getLongLoginSettings,
  getNotificationChannels,
  passwordLogin,
  refreshAccountProfile,
  setLongLoginSettings,
  updateAccountAISettings,
  updateAccountPauseDuration,
  updateAccountSettings,
  updateAccountStatus,
} from '../../../services/api';

// 账号 feature 重新导出接口返回类型，保持页面与网络契约的依赖方向稳定。
export type {
  AccountRuntimeStatus,
  LongLoginSettings,
  PasswordLoginStartResponse,
  PasswordLoginStatusResponse,
} from '../../../services/api';
