// notifications 只公开 OpenAPI 生成的通知传输类型；表单状态属于 notifications feature。
import type { components } from './generated/schema';

/** NotificationChannelTransport 表示生成的通知渠道摘要。 */
export type NotificationChannelTransport = components['schemas']['NotificationChannelResponse'];
/** NotificationBindingsTransport 表示生成的账号通知绑定映射。 */
export type NotificationBindingsTransport = components['schemas']['NotificationBindingsByAccount'];
