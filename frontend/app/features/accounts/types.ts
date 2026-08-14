import type { Dispatch, SetStateAction } from 'react';
import type { AccountDetail, AIReplySettings, NotificationChannel } from '../../../types';

// AccountEditForm 描述账号编辑弹窗中的可编辑字段。
export interface AccountEditForm {
  // remark 是账号备注。
  remark: string;
  // cookie 是手工维护的授权 Cookie。
  cookie: string;
  // auto_confirm 表示是否自动确认发货。
  auto_confirm: boolean;
  // pause_duration 是账号订单处理暂停时长，单位为分钟。
  pause_duration: number;
  // username 是用于密码登录的闲鱼账号。
  username: string;
  // login_password 是本次输入或已保存的登录密码。
  login_password: string;
  // show_browser 表示密码登录时是否展示浏览器窗口。
  show_browser: boolean;
  // showLoginPassword 控制密码输入框是否显示明文。
  showLoginPassword: boolean;
  // clear_password 表示保存时清空服务端密码。
  clear_password: boolean;
}

// PasswordLoginView 描述密码登录刷新授权的异步状态。
export interface PasswordLoginView {
  // sessionId 是后端创建的密码登录会话标识。
  sessionId: string;
  // status 是当前密码登录阶段。
  status: 'idle' | 'processing' | 'verification_required' | 'success' | 'failed';
  // message 是面向用户展示的状态说明。
  message: string;
  // qrCodeUrl 是风控人脸验证二维码地址。
  qrCodeUrl: string;
}

// LongLoginState 描述闲鱼官方长登录设置的读取和保存状态。
export interface LongLoginState {
  // loading 表示正在读取长登录设置。
  loading: boolean;
  // saving 表示正在保存长登录设置。
  saving: boolean;
  // canOpen 表示当前账号是否允许修改长登录设置。
  canOpen: boolean;
  // enabled 表示闲鱼官方当前是否启用长登录。
  enabled: boolean;
  // error 保存读取或保存失败的用户可见说明。
  error: string;
}

// AccountRuntimePresentation 是账号运行状态在列表中的展示信息。
export interface AccountRuntimePresentation {
  // label 是状态徽标文本。
  label: string;
  // badge 是状态徽标使用的 Tailwind 样式。
  badge: string;
  // dot 是头像旁状态点使用的 Tailwind 样式。
  dot: string;
}

// AccountEditModalProps 描述账号编辑组件需要的状态和交互回调。
export interface AccountEditModalProps {
  // account 是当前正在编辑的账号。
  account: AccountDetail;
  // editForm 是编辑表单的当前值。
  editForm: AccountEditForm;
  // setEditForm 更新编辑表单字段。
  setEditForm: Dispatch<SetStateAction<AccountEditForm>>;
  // saving 表示保存或立即暂停动作正在执行。
  saving: boolean;
  // onClose 关闭编辑弹窗并取消未完成的登录操作。
  onClose: () => void | Promise<void>;
  // onSave 保存账号编辑内容。
  onSave: () => void | Promise<void>;
  // onRestartPause 按当前时长重新暂停账号。
  onRestartPause: () => void | Promise<void>;
  // longLogin 是官方长登录设置状态。
  longLogin: LongLoginState;
  // onToggleLongLogin 切换官方长登录设置。
  onToggleLongLogin: () => void | Promise<void>;
  // passwordLoginView 是密码登录刷新授权状态。
  passwordLoginView: PasswordLoginView;
  // onPasswordLogin 启动密码登录刷新授权。
  onPasswordLogin: () => void | Promise<void>;
  // onCancelPasswordLogin 取消当前密码登录会话。
  onCancelPasswordLogin: () => void | Promise<void>;
  // notifChannels 是可供账号绑定的通知渠道。
  notifChannels: NotificationChannel[];
  // selectedChannelIds 是当前已选中的通知渠道 ID。
  selectedChannelIds: number[];
  // bindingsLoaded 表示通知绑定是否成功加载。
  bindingsLoaded: boolean;
  // bindingsLoading 表示通知渠道和绑定关系正在加载。
  bindingsLoading: boolean;
  // bindingsLoadError 是通知绑定加载失败的说明。
  bindingsLoadError: string;
  // onRetryBindings 重新加载当前账号的通知绑定。
  onRetryBindings: () => void | Promise<void>;
  // onToggleChannel 切换一个通知渠道的选中状态。
  onToggleChannel: (channelId: number) => void;
  // onSettingsDirty 标记通知绑定已被用户修改。
  onSettingsDirty: () => void;
}

// AccountAISettingsState 是账号列表合并 AI 配置时使用的索引结构。
export type AccountAISettingsState = Record<string, AIReplySettings>;
