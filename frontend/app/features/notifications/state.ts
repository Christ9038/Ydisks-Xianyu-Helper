import { Bell, Mail, MessageCircle, Send as Telegram, Webhook } from 'lucide-react';
import type { NotificationChannel, NotificationChannelType, NotificationEventType, SystemSettings } from '../../../shared/api-contract';
import { buildEmailChannelConfig, enableCustomSMTP, normalizeEmailChannelConfig } from '../../../notificationEmailConfig';
import type { NotificationChannelMeta, NotificationEventMeta, NotificationForm, NotificationPayload } from './types';

// notificationChannelTypes 是所有通知渠道的静态字段、图标和使用指南。
export const notificationChannelTypes: Record<NotificationChannelType, NotificationChannelMeta> = {
  bark: {
    label: 'Bark', icon: Bell,
    fields: [
      { key: 'server_url', label: 'Bark 服务器', placeholder: 'https://api.day.app', required: true },
      { key: 'device_key', label: 'Device Key', placeholder: '你的 Bark 设备 Key', required: true },
      { key: 'title', label: '标题（可选）', placeholder: 'Ydisks闲鱼助手' },
      { key: 'sound', label: '铃声（可选）', placeholder: 'default' },
      { key: 'group', label: '分组（可选）', placeholder: 'xianyu' },
    ],
    guide: { steps: ['App Store 搜索并安装 Bark（免费）', '打开 Bark App，首页会显示一条测试 URL，形如 https://api.day.app/<key>/这是测试推送内容', 'URL 路径里 <key> 那一段就是 Device Key，复制填入', '服务器地址默认 https://api.day.app，自建服务端则填自建域名'], urlFormat: 'https://api.day.app/<你的key>/测试内容', note: '不需要 secret，Device Key 本身就是唯一凭证。' },
  },
  dingtalk: {
    label: '钉钉机器人', icon: MessageCircle,
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=...', required: true },
      { key: 'secret', label: '加签密钥', placeholder: 'SEC...', help: '安全设置勾选「加签」后生成，SEC 开头' },
    ],
    guide: { steps: ['钉钉进入一个群（可建单人群）→ 右上角 群设置', '机器人 → 添加机器人 → 选择「自定义机器人」', '安全设置勾选「加签」', '完成页同时展示 Webhook 地址和 SEC 开头的 Secret，两个都复制', 'Webhook URL 填上面，Secret 填加签密钥'], urlFormat: 'https://oapi.dingtalk.com/robot/send?access_token=XXX', note: '加签密钥可选，但强烈建议启用，否则机器人可能被滥用。' },
  },
  feishu: {
    label: '飞书机器人', icon: MessageCircle,
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/...', required: true },
      { key: 'secret', label: '签名校验密钥', placeholder: 'sec-...', help: '安全设置选「签名校验」后生成' },
    ],
    guide: { steps: ['飞书进入目标群 → 右上角 更多 → 设置', '群机器人 → 添加机器人 → 自定义机器人', '安全设置选择「签名校验」，复制生成的秘钥', '完成会给 Webhook 地址，复制', 'Webhook URL 填上面，秘钥填签名校验密钥'], urlFormat: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx', note: '签名密钥可选，建议启用。频率限制：单机器人 100 次/分钟。' },
  },
  wechat: {
    label: '企业微信机器人', icon: MessageCircle,
    fields: [{ key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...', required: true }],
    guide: { steps: ['企业微信进入一个内部群 → 右上角 … → 群设置', '找到「群机器人」（新版可能叫「消息推送」）', '添加 → 选择自定义机器人', '完成给 Webhook 地址（含 ?key=），复制填入'], urlFormat: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=XXX', note: '不需要 secret，URL 里的 key 就是唯一凭证，注意不要泄露。频率限制 20 条/分钟。' },
  },
  webhook: {
    label: '自定义 Webhook', icon: Webhook,
    fields: [{ key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://your-server/notify', required: true }],
    guide: { steps: ['仅适用于自己有服务器的用户：在你自己的机器上起一个 HTTP 接口收 JSON', '系统会 POST 这个 URL，Content-Type: application/json', '请求体固定为右侧 JSON 格式，按需解析 message 字段'], urlFormat: '{"message":"告警正文","timestamp":"2026-07-05 12:00:00","source":"xianyu-auto-reply"}', note: '若用 Server酱 / PushPlus 等第三方推送服务，格式不兼容，需写中间脚本转发，或改用对应专用渠道。' },
  },
  telegram: {
    label: 'Telegram', icon: Telegram,
    fields: [{ key: 'bot_token', label: 'Bot Token', placeholder: '123456:ABC-DEF...', required: true }, { key: 'chat_id', label: 'Chat ID', placeholder: '-1001234567890 或你的用户 ID', required: true }],
    guide: { steps: ['在 Telegram 搜索 @BotFather，发送 /newbot，按提示创建机器人，拿到 Bot Token', '把你的 Bot 拉进一个群，或私聊它发一条消息', '浏览器访问 https://api.telegram.org/bot<你的Token>/getUpdates', '从返回 JSON 里找 "chat":{"id":xxx}，xxx 就是 Chat ID（群是负数，私聊是正数）'], urlFormat: 'Bot Token: 123456:ABC-DEF...  Chat ID: -1001234567890', note: '群聊需先把 Bot 设为管理员才能发消息。' },
  },
  email: {
    label: '邮件', icon: Mail,
    fields: [{ key: 'to_email', label: '收件邮箱', placeholder: 'receiver@example.com', required: true }],
    guide: { steps: ['通常先保存页面中的系统 SMTP 配置', '邮件渠道只需填写收件邮箱，即可完整继承系统 SMTP', '只有确实需要另一套发件服务时，才开启“使用独立 SMTP”并填写整套配置'], note: '继承和独立 SMTP 是互斥模式，不会再混用两套配置中的部分字段。' },
  },
};

// notificationEvents 是可绑定到通知渠道的事件定义。
export const notificationEvents: NotificationEventMeta[] = [
  { value: 'account_offline', label: '掉线通知', description: '账号过期、断线或登录态失效' },
  { value: 'account_recovered', label: '恢复通知', description: '自动恢复成功并重新在线' },
  { value: 'account_disabled', label: '禁用通知', description: '连续失败、账密错误等导致账号停用' },
  { value: 'security_verification', label: '风控验证', description: '滑块、人脸、扫码验证等安全校验' },
  { value: 'delivery_result', label: '交易通知', description: '订单发货、卡密发送等交易结果' },
  { value: 'token_renewal', label: '续期通知', description: 'Cookie/token 续期和自动恢复过程' },
  { value: 'system_error', label: '系统错误', description: '后台任务或系统级异常' },
];

// emptyNotificationForm 返回新建渠道的初始表单。
export const emptyNotificationForm = (): NotificationForm => ({ name: '', type: 'bark', enabled: true, config: {}, event_types: [] });

// notificationEventSummary 将事件值转换为渠道列表中的中文摘要。
export const notificationEventSummary = (events?: NotificationEventType[]): string => {
  if (!events || events.length === 0) return '全部事件';
  return events.map(
    // event 是当前待转换的后端事件值。
    event => notificationEvents.find(
      // item 是当前事件定义。
      item => item.value === event,
    )?.label || event,
  ).join('、');
};

// normalizeNotificationForm 将编辑中的渠道配置复制为独立表单对象。
export const normalizeNotificationForm = (channel: NotificationChannel, smtp: SystemSettings): NotificationForm => {
  // normalizedEmailConfig 是邮件渠道的兼容配置副本。
  const normalizedEmailConfig = channel.type === 'email' ? normalizeEmailChannelConfig({ ...(channel.config || {}) }) : null;
  // config 是编辑表单实际使用的渠道配置。
  const config = normalizedEmailConfig
    ? (normalizedEmailConfig.use_custom_smtp === true ? enableCustomSMTP(normalizedEmailConfig, smtp) : normalizedEmailConfig)
    : { ...(channel.config || {}) };
  return { name: channel.name, type: channel.type, enabled: channel.enabled, config, event_types: channel.event_types || [] };
};

// validateNotificationForm 校验渠道名称、渠道字段和独立 SMTP 必填项。
export const validateNotificationForm = (form: NotificationForm): string => {
  // meta 是当前渠道类型的静态校验配置。
  const meta = notificationChannelTypes[form.type];
  // missingField 是第一个未填写的渠道必填字段。
  const missingField = meta.fields.find(
    // field 是当前渠道字段定义。
    field => field.required && !String(form.config[field.key] || '').trim(),
  );
  if (missingField) return `请填写 ${missingField.label}`;
  if (!form.name.trim()) return '请填写渠道名称';
  if (form.type === 'email') {
    // config 是邮件渠道归一化后的提交配置。
    const config = buildEmailChannelConfig(form.config);
    if (config.use_custom_smtp) {
      // requiredFields 是独立 SMTP 模式下必须填写的字段列表。
      const requiredFields: Array<[string, string]> = [
        ['smtp_server', '独立 SMTP 服务器'], ['smtp_port', '独立 SMTP 端口'], ['smtp_user', '独立 SMTP 登录邮箱'],
        ['smtp_password', '独立 SMTP 密码 / 授权码'], ['smtp_from_address', '独立 SMTP 发件地址'],
      ];
      // missing 是独立 SMTP 模式下第一个缺失字段。
      const missing = requiredFields.find(
        // field 是字段名和用户可见名称的二元组。
        ([key]) => !String(config[key] || '').trim(),
      );
      if (missing) return `请填写 ${missing[1]}`;
    }
  }
  return '';
};

// buildNotificationPayload 将表单转换为通知渠道保存请求。
export const buildNotificationPayload = (form: NotificationForm): NotificationPayload => ({
  name: form.name.trim(),
  type: form.type,
  config: form.type === 'email' ? buildEmailChannelConfig(form.config) : form.config,
  event_types: form.event_types,
  enabled: form.enabled,
});

// isCurrentNotificationRequest 判断异步通知请求是否仍属于最新代次。
export const isCurrentNotificationRequest = (generation: number, currentGeneration: number): boolean => generation === currentGeneration;

// notificationErrorMessage 将未知异常转换为用户可读的通知错误。
export const notificationErrorMessage = (error: unknown, fallback: string): string => error instanceof Error && error.message ? error.message : fallback;
