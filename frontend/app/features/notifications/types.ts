import type { Dispatch, ElementType, SetStateAction } from 'react';
import type { NotificationChannel, NotificationChannelType, NotificationEventType, SystemSettings } from '../../../types';

// NotificationField 描述通知渠道表单中的一个配置字段。
export interface NotificationField {
  // key 是提交到渠道配置的字段名。
  key: string;
  // label 是字段在表单中的中文名称。
  label: string;
  // placeholder 是输入框的示例提示。
  placeholder?: string;
  // type 是输入控件的 HTML 类型。
  type?: 'text' | 'password' | 'number';
  // required 表示该字段是否必须填写。
  required?: boolean;
  // help 是字段下方的补充说明。
  help?: string;
}

// NotificationGuide 描述如何获取某类通知渠道凭据。
export interface NotificationGuide {
  // steps 是按顺序展示的配置步骤。
  steps: string[];
  // urlFormat 是可选的 URL 或请求格式示例。
  urlFormat?: string;
  // note 是渠道配置的额外注意事项。
  note?: string;
}

// NotificationChannelMeta 描述通知渠道的静态表单和展示配置。
export interface NotificationChannelMeta {
  // label 是渠道类型的中文名称。
  label: string;
  // icon 是渠道列表和表单中使用的图标组件。
  icon: ElementType;
  // fields 是渠道配置字段定义。
  fields: NotificationField[];
  // guide 是渠道凭据获取指南。
  guide: NotificationGuide;
}

// NotificationEventMeta 描述可供渠道订阅的一类系统事件。
export interface NotificationEventMeta {
  // value 是后端识别的事件类型值。
  value: NotificationEventType;
  // label 是事件的中文名称。
  label: string;
  // description 是事件在绑定组件中的说明。
  description: string;
}

// NotificationForm 描述新建或编辑通知渠道的表单值。
export interface NotificationForm {
  // name 是渠道名称。
  name: string;
  // type 是当前渠道类型。
  type: NotificationChannelType;
  // enabled 表示是否启用当前渠道。
  enabled: boolean;
  // config 保存渠道类型对应的配置值。
  config: Record<string, unknown>;
  // event_types 保存用户选择的通知事件，空数组表示全部事件。
  event_types: NotificationEventType[];
}

// NotificationPayload 描述保存通知渠道时发送给 API 的请求体。
export interface NotificationPayload {
  // name 是保存后的渠道名称。
  name: string;
  // type 是保存后的渠道类型。
  type: string;
  // config 是归一化后的渠道配置。
  config: Record<string, unknown>;
  // event_types 是保存后的事件订阅集合。
  event_types: NotificationEventType[];
  // enabled 是保存后的启用状态。
  enabled: boolean;
}

// NotificationToast 描述页面底部的短暂操作提示。
export interface NotificationToast {
  // type 决定提示的成功或失败样式。
  type: 'success' | 'error';
  // text 是提示内容。
  text: string;
}

// NotificationState 暴露通知页面的数据、表单和操作边界。
export interface NotificationState {
  // channels 是当前用户可用的通知渠道列表。
  channels: NotificationChannel[];
  // loading 表示渠道列表是否正在加载。
  loading: boolean;
  // showModal 表示渠道编辑弹窗是否打开。
  showModal: boolean;
  // editing 是当前正在编辑的渠道，新增时为空。
  editing: NotificationChannel | null;
  // saving 表示渠道或 SMTP 配置是否正在保存。
  saving: boolean;
  // testingId 是当前正在发送测试通知的渠道 ID。
  testingId: string;
  // toast 是当前待展示的操作提示。
  toast: NotificationToast | null;
  // smtp 是系统级邮件配置。
  smtp: SystemSettings;
  // smtpSaving 表示系统 SMTP 是否正在保存。
  smtpSaving: boolean;
  // showSmtpPassword 控制系统 SMTP 密码明文展示。
  showSmtpPassword: boolean;
  // showChannelSmtpPassword 控制独立 SMTP 密码明文展示。
  showChannelSmtpPassword: boolean;
  // form 是当前渠道编辑表单值。
  form: NotificationForm;
  // setForm 更新渠道编辑表单。
  setForm: Dispatch<SetStateAction<NotificationForm>>;
  // setSmtp 更新系统 SMTP 配置表单。
  setSmtp: Dispatch<SetStateAction<SystemSettings>>;
  // setShowSmtpPassword 更新系统 SMTP 密码展示状态。
  setShowSmtpPassword: Dispatch<SetStateAction<boolean>>;
  // setShowChannelSmtpPassword 更新独立 SMTP 密码展示状态。
  setShowChannelSmtpPassword: Dispatch<SetStateAction<boolean>>;
  // loadChannels 刷新通知渠道列表。
  loadChannels: () => Promise<void>;
  // openCreate 打开新建渠道弹窗。
  openCreate: () => void;
  // openEdit 打开已有渠道编辑弹窗。
  openEdit: (channel: NotificationChannel) => void;
  // closeModal 关闭弹窗并取消当前保存请求。
  closeModal: () => void;
  // showToast 展示短暂的成功或失败提示。
  showToast: (type: NotificationToast['type'], text: string) => void;
  // handleSave 保存当前渠道配置。
  handleSave: () => Promise<void>;
  // handleDelete 删除指定通知渠道。
  handleDelete: (channel: NotificationChannel) => Promise<void>;
  // handleToggleEnabled 切换指定渠道的启用状态。
  handleToggleEnabled: (channel: NotificationChannel) => Promise<void>;
  // handleTest 发送指定渠道的测试通知。
  handleTest: (channel: NotificationChannel) => Promise<void>;
  // handleSaveSmtp 保存系统级 SMTP 配置。
  handleSaveSmtp: () => Promise<void>;
}
