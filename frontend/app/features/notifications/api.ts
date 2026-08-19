import type { MutationIDResponse,NotificationBinding,NotificationChannel,NotificationChannelResponse,NotificationEventType,OperationResponse,SystemSettings } from './models';
import { type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
export type * from './models';

/** 通知渠道写入时使用的具名请求 DTO。 */
export interface NotificationChannelRequest {
  /** 管理界面展示的渠道名称。 */
  name?: string;
  /** 后端支持的渠道类型。 */
  type?: string;
  /** 由各渠道表单构造的非敏感配置。 */
  config?: Record<string, unknown>;
  /** 需要订阅的系统事件。 */
  event_types?: NotificationEventType[];
  /** 渠道是否参与通知投递。 */
  enabled?: boolean;
}

/** 将后端字符串或历史分隔文本转换为稳定事件类型列表。 */
const parseNotificationEventTypes = (raw: unknown): NotificationEventType[] => {
  if (Array.isArray(raw)) return raw.filter(Boolean) as NotificationEventType[];
  if (typeof raw !== 'string' || !raw.trim()) return [];
  try {
    // parsed 是解析后的历史 JSON 事件列表。
    const parsed: unknown = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed.filter(Boolean) as NotificationEventType[];
  } catch {
    // 非 JSON 历史值继续按分隔符兼容解析。
  }
  return raw.split(/[,\s;]+/).map(value => value.trim()).filter(Boolean) as NotificationEventType[];
};

/** 将事件类型列表序列化为后端稳定保存的 JSON 文本。 */
const stringifyNotificationEventTypes = (eventTypes?: NotificationEventType[]): string => {
  // uniqueTypes 是去重且去除空值后的事件列表。
  const uniqueTypes = Array.from(new Set((eventTypes || []).filter(Boolean)));
  return uniqueTypes.length > 0 ? JSON.stringify(uniqueTypes) : '';
};

/** 把后端渠道 DTO 转换为通知编辑器使用的 UI 模型。 */
const toNotificationChannel = (channel: NotificationChannelResponse): NotificationChannel => {
	// config 是列表摘要不返回 SMTP 等秘密时使用的空编辑初始值。
	const config: Record<string, unknown> = {};
  // type 是处理 ding_talk/lark 历史别名后的渠道类型。
  const type = (channel.type === 'ding_talk' ? 'dingtalk' : channel.type === 'lark' ? 'feishu' : channel.type) as NotificationChannel['type'];
	return { id: String(channel.id), name: channel.name, type, config, event_types: parseNotificationEventTypes(channel.event_types), enabled: channel.enabled };
};

/** 获取全部通知渠道并转换为编辑器模型。 */
export const getNotificationChannels = async (options?: RequestControlOptions): Promise<{ /** 操作是否完成。 */ success: boolean; /** 渠道列表。 */ data: NotificationChannel[] }> => {
  // response 是渠道列表的原始传输响应。
  const response = await runContractRequest(/* signal 控制通知渠道列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/notifications/channels', { signal }), options);
  return { success: true, data: response.map(toNotificationChannel) };
};

/** 创建通知渠道，并在传输边界序列化配置与事件。 */
export const createNotificationChannel = async (data: Required<Pick<NotificationChannelRequest, 'name' | 'type' | 'config'>> & NotificationChannelRequest, options?: RequestControlOptions): Promise<MutationIDResponse> => runContractRequest(/* signal 控制通知渠道创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/channels', { body: { name: data.name, type: data.type, config: JSON.stringify(data.config), event_types: stringifyNotificationEventTypes(data.event_types), enabled: data.enabled }, signal }), options);

/** 更新通知渠道的可编辑字段。 */
export const updateNotificationChannel = async (channelID: string, data: NotificationChannelRequest, options?: RequestControlOptions): Promise<OperationResponse> => {
  // payload 是已完成配置和事件序列化的更新请求。
  const payload = {
    name: data.name,
    type: data.type,
    config: data.config === undefined ? undefined : JSON.stringify(data.config),
    event_types: data.event_types === undefined ? undefined : stringifyNotificationEventTypes(data.event_types),
    enabled: data.enabled,
  };
  return runContractRequest(/* signal 控制通知渠道更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/notifications/channels/{channel_id}', { params: { path: { channel_id: channelID } }, body: payload, signal }), options);
};

/** 删除指定通知渠道。 */
export const deleteNotificationChannel = async (channelID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制通知渠道删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/notifications/channels/{channel_id}', { params: { path: { channel_id: channelID } }, signal }), options);

/** 获取并展平按账号分组存储的消息通知绑定。 */
export const getMessageNotifications = async (): Promise<{ /** 操作是否完成。 */ success: boolean; /** 展平后的绑定列表。 */ data: NotificationBinding[] }> => {
  // response 是按账号 ID 分组的通知绑定响应。
  const response = await runContractRequest(/* signal 控制消息通知列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/notifications/messages', { signal }));
  // bindings 是供页面直接渲染的扁平绑定集合。
  const bindings: NotificationBinding[] = [];
  for (const /* accountID、channelBindings 是当前账号及其原始渠道绑定列表。 */ [accountID, channelBindings] of Object.entries(response || {})) {
    if (!Array.isArray(channelBindings)) continue;
    for (const /* binding 是当前待展平的单个账号渠道绑定。 */ binding of channelBindings) bindings.push({ ...binding, cookie_id: accountID });
  }
  return { success: true, data: bindings };
};

/** 设置单个账号与渠道之间的启用状态。 */
export const setMessageNotification = async (accountID: string, channelID: number, enabled: boolean): Promise<OperationResponse> => runContractRequest(/* signal 控制单账号通知开关更新请求的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/accounts/{cid}/bindings', { params: { path: { cid: accountID } }, body: { channel_id: channelID, enabled }, signal }));

/** 删除单个消息通知绑定。 */
export const deleteMessageNotification = async (notificationID: string): Promise<OperationResponse> => runContractRequest(/* signal 控制单条通知删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/notifications/messages/{notification_id}', { params: { path: { notification_id: notificationID } }, signal }));

/** 删除指定账号的全部消息通知绑定。 */
export const deleteAccountNotifications = async (accountID: string): Promise<OperationResponse> => runContractRequest(/* signal 控制账号通知清理请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/notifications/messages/account/{cid}', { params: { path: { cid: accountID } }, signal }));

/** 读取指定账号已绑定的通知渠道主键。 */
export const getAccountBindings = async (accountID: string, options?: RequestControlOptions): Promise<number[]> => {
  // response 是由 OpenAPI 约束的账号渠道绑定响应；未绑定时后端兼容返回 null。
  const response = await runContractRequest(/* signal 控制账号通知绑定读取的取消和超时。 */ signal => contractClient.GET('/api/v1/notifications/accounts/{cid}/bindings', { params: { path: { cid: accountID } }, signal }), options);
  return response.channel_ids ?? [];
};

/** 覆盖保存指定账号的通知渠道绑定。 */
export const setAccountBindings = async (accountID: string, channelIDs: number[]): Promise<OperationResponse> => runContractRequest(/* signal 控制账号通知绑定覆盖请求的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/accounts/{cid}/bindings', { params: { path: { cid: accountID } }, body: { channel_ids: channelIDs }, signal }));

/** 向单一通知渠道发送测试投递。 */
export const testNotificationChannel = async (channelID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制通知渠道测试投递的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/channels/{channel_id}/test', { params: { path: { channel_id: channelID } }, signal }), options);

/** 将系统配置响应转换为通知 SMTP 表单使用的状态模型。 */
const normalizeSystemSettings = (settings: Record<string, unknown>): SystemSettings => {
  // result 是不改变原始响应对象的设置副本。
  const result: Record<string, unknown> = { ...settings };
  if ('renewal_log_retention_days' in result) {
    // days 是已校验的日志保留天数。
    const days = Number(result.renewal_log_retention_days);
    result.renewal_log_retention_days = Number.isFinite(days) ? days : 10;
  }
  return result as SystemSettings;
};

/** 获取通知 SMTP 编辑器需要的系统设置。 */
export const getSystemSettings = async (options?: RequestControlOptions): Promise<SystemSettings> => {
  // response 是服务端可能以 data 包装的系统设置响应。
  const response = await runContractRequest(/* signal 控制系统设置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/settings/system', { signal }), options) as unknown as { /** data 是当前服务端系统设置。 */ data?: Record<string, unknown>; /** settings 是兼容字段。 */ settings?: Record<string, unknown> } & Record<string, unknown>;
  return normalizeSystemSettings(response.data || response.settings || response);
};

/** 保存通知 SMTP 编辑器提交的系统设置。 */
export const updateSystemSettings = async (settings: Partial<SystemSettings>, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制系统设置更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/settings/system', { body: settings as never, signal }), options);
