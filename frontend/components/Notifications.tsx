import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { NotificationChannel, NotificationChannelType, NotificationEventType, SystemSettings } from '../types';
import {
  getNotificationChannels,
  createNotificationChannel,
  updateNotificationChannel,
  deleteNotificationChannel,
  testNotificationChannel,
  getSystemSettings,
  updateSystemSettings,
} from '../services/api';
import {
  Bell, Plus, Edit2, Trash2, X, Send, RefreshCw, Loader2,
  Check, MessageCircle, Mail, Webhook, Send as Telegram, Eye, EyeOff, Save,
} from 'lucide-react';
import { normalizeSMTPSettings } from '../smtpSettings';

type ChannelTypeMeta = {
  label: string;
  icon: React.ElementType;
  fields: { key: string; label: string; placeholder?: string; type?: 'text' | 'password' | 'number'; required?: boolean; help?: string }[];
  guide: { steps: string[]; urlFormat?: string; note?: string };
};

const CHANNEL_TYPES: Record<NotificationChannelType, ChannelTypeMeta> = {
  bark: {
    label: 'Bark',
    icon: Bell,
    fields: [
      { key: 'server_url', label: 'Bark 服务器', placeholder: 'https://api.day.app', required: true },
      { key: 'device_key', label: 'Device Key', placeholder: '你的 Bark 设备 Key', required: true },
      { key: 'title', label: '标题（可选）', placeholder: '闲鱼助手' },
      { key: 'sound', label: '铃声（可选）', placeholder: 'default' },
      { key: 'group', label: '分组（可选）', placeholder: 'xianyu' },
    ],
    guide: {
      steps: [
        'App Store 搜索并安装 Bark（免费）',
        '打开 Bark App，首页会显示一条测试 URL，形如 https://api.day.app/<key>/这是测试推送内容',
        'URL 路径里 <key> 那一段就是 Device Key，复制填入',
        '服务器地址默认 https://api.day.app，自建服务端则填自建域名',
      ],
      urlFormat: 'https://api.day.app/<你的key>/测试内容',
      note: '不需要 secret，Device Key 本身就是唯一凭证。',
    },
  },
  dingtalk: {
    label: '钉钉机器人',
    icon: MessageCircle,
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=...', required: true },
      { key: 'secret', label: '加签密钥', placeholder: 'SEC...', help: '安全设置勾选「加签」后生成，SEC 开头' },
    ],
    guide: {
      steps: [
        '钉钉进入一个群（可建单人群）→ 右上角 群设置',
        '机器人 → 添加机器人 → 选择「自定义机器人」',
        '安全设置勾选「加签」',
        '完成页同时展示 Webhook 地址和 SEC 开头的 Secret，两个都复制',
        'Webhook URL 填上面，Secret 填加签密钥',
      ],
      urlFormat: 'https://oapi.dingtalk.com/robot/send?access_token=XXX',
      note: '加签密钥可选，但强烈建议启用，否则机器人可能被滥用。',
    },
  },
  feishu: {
    label: '飞书机器人',
    icon: MessageCircle,
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/...', required: true },
      { key: 'secret', label: '签名校验密钥', placeholder: 'sec-...', help: '安全设置选「签名校验」后生成' },
    ],
    guide: {
      steps: [
        '飞书进入目标群 → 右上角 更多 → 设置',
        '群机器人 → 添加机器人 → 自定义机器人',
        '安全设置选择「签名校验」，复制生成的秘钥',
        '完成会给 Webhook 地址，复制',
        'Webhook URL 填上面，秘钥填签名校验密钥',
      ],
      urlFormat: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx',
      note: '签名密钥可选，建议启用。频率限制：单机器人 100 次/分钟。',
    },
  },
  wechat: {
    label: '企业微信机器人',
    icon: MessageCircle,
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...', required: true },
    ],
    guide: {
      steps: [
        '企业微信进入一个内部群 → 右上角 … → 群设置',
        '找到「群机器人」（新版可能叫「消息推送」）',
        '添加 → 选择自定义机器人',
        '完成给 Webhook 地址（含 ?key=），复制填入',
      ],
      urlFormat: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=XXX',
      note: '不需要 secret，URL 里的 key 就是唯一凭证，注意不要泄露。频率限制 20 条/分钟。',
    },
  },
  webhook: {
    label: '自定义 Webhook',
    icon: Webhook,
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://your-server/notify', required: true },
    ],
    guide: {
      steps: [
        '仅适用于自己有服务器的用户：在你自己的机器上起一个 HTTP 接口收 JSON',
        '系统会 POST 这个 URL，Content-Type: application/json',
        '请求体固定为右侧 JSON 格式，按需解析 message 字段',
      ],
      urlFormat: '{"message":"告警正文","timestamp":"2026-07-05 12:00:00","source":"xianyu-auto-reply"}',
      note: '若用 Server酱 / PushPlus 等第三方推送服务，格式不兼容，需写中间脚本转发，或改用对应专用渠道。',
    },
  },
  telegram: {
    label: 'Telegram',
    icon: Telegram,
    fields: [
      { key: 'bot_token', label: 'Bot Token', placeholder: '123456:ABC-DEF...', required: true },
      { key: 'chat_id', label: 'Chat ID', placeholder: '-1001234567890 或你的用户 ID', required: true },
    ],
    guide: {
      steps: [
        '在 Telegram 搜索 @BotFather，发送 /newbot，按提示创建机器人，拿到 Bot Token',
        '把你的 Bot 拉进一个群，或私聊它发一条消息',
        '浏览器访问 https://api.telegram.org/bot<你的Token>/getUpdates',
        '从返回 JSON 里找 "chat":{"id":xxx}，xxx 就是 Chat ID（群是负数，私聊是正数）',
      ],
      urlFormat: 'Bot Token: 123456:ABC-DEF...  Chat ID: -1001234567890',
      note: '群聊需先把 Bot 设为管理员才能发消息。',
    },
  },
  email: {
    label: '邮件',
    icon: Mail,
    fields: [
      { key: 'to_email', label: '收件邮箱', placeholder: 'receiver@example.com', required: true },
      { key: 'smtp_server', label: '覆盖 SMTP 服务器', placeholder: 'smtp.qq.com', help: '留空时使用上方系统级 SMTP 配置' },
      { key: 'smtp_port', label: '覆盖 SMTP 端口', placeholder: '587', type: 'number', help: '留空时使用系统级 SMTP 端口' },
      { key: 'smtp_user', label: '覆盖发件邮箱', placeholder: 'you@qq.com', help: '留空时使用系统级发件邮箱' },
      { key: 'smtp_password', label: '覆盖密码 / 授权码', type: 'password', help: '留空时使用系统级授权码' },
    ],
    guide: {
      steps: [
        '先在页面上方保存系统级 SMTP 服务器、端口、发件邮箱和授权码',
        '邮件渠道只需要填写收件邮箱',
        '如果某个渠道要使用不同发件邮箱，再填写下面的覆盖 SMTP 字段',
      ],
      note: '收件邮箱会写入 to_email；SMTP 字段留空时后端会复用系统级 SMTP 配置。',
    },
  },
};

const NOTIFICATION_EVENTS: { value: NotificationEventType; label: string; description: string }[] = [
  { value: 'account_offline', label: '掉线通知', description: '账号过期、断线或登录态失效' },
  { value: 'account_recovered', label: '恢复通知', description: '自动恢复成功并重新在线' },
  { value: 'account_disabled', label: '禁用通知', description: '连续失败、账密错误等导致账号停用' },
  { value: 'security_verification', label: '风控验证', description: '滑块、人脸、扫码验证等安全校验' },
  { value: 'delivery_result', label: '交易通知', description: '订单发货、卡密发送等交易结果' },
  { value: 'token_renewal', label: '续期通知', description: 'Cookie/token 续期和自动恢复过程' },
  { value: 'system_error', label: '系统错误', description: '后台任务或系统级异常' },
];

const notificationEventSummary = (events?: NotificationEventType[]) => {
  if (!events || events.length === 0) return '全部事件';
  const labels = events.map(event => NOTIFICATION_EVENTS.find(item => item.value === event)?.label || event);
  return labels.join('、');
};

interface NotificationsProps {
  isAdmin?: boolean;
}

const Notifications: React.FC<NotificationsProps> = ({ isAdmin = false }) => {
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<NotificationChannel | null>(null);
  const [saving, setSaving] = useState(false);
  const [testingId, setTestingId] = useState<string>('');
  const [toast, setToast] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  // SMTP 系统级邮件配置（从系统设置加载/保存）
  const [smtp, setSmtp] = useState<SystemSettings>({});
  const [smtpSaving, setSmtpSaving] = useState(false);
  const [showSmtpPassword, setShowSmtpPassword] = useState(false);

  const [form, setForm] = useState({
    name: '',
    type: 'bark' as NotificationChannelType,
    enabled: true,
    config: {} as Record<string, unknown>,
    event_types: [] as NotificationEventType[],
  });

  const load = async () => {
    setLoading(true);
    try {
      const res = await getNotificationChannels();
      setChannels(res.data || []);
    } catch (e) {
      console.error('加载通知渠道失败', e);
    } finally {
      setLoading(false);
    }
  };

  const loadSmtp = async () => {
    try {
      const s = await getSystemSettings();
      setSmtp(normalizeSMTPSettings(s || {}));
    } catch (e) {
      console.error('加载 SMTP 配置失败', e);
    }
  };

  const handleSaveSmtp = async () => {
    setSmtpSaving(true);
    try {
      await updateSystemSettings({
        smtp_server: smtp.smtp_server || '',
        smtp_port: smtp.smtp_port || 587,
        smtp_user: smtp.smtp_user || '',
        smtp_password: smtp.smtp_password || '',
        smtp_from_name: smtp.smtp_from_name || '',
        smtp_from_address: smtp.smtp_from_address || smtp.smtp_user || '',
      });
      showToast('success', 'SMTP 配置已保存');
    } catch (e: any) {
      showToast('error', e?.message || '保存失败');
    } finally {
      setSmtpSaving(false);
    }
  };

  useEffect(() => {
    load();
    if (isAdmin) {
      loadSmtp();
    } else {
      setSmtp({});
    }
  }, [isAdmin]);

  const showToast = (type: 'success' | 'error', text: string) => {
    setToast({ type, text });
    window.setTimeout(() => setToast(null), 3000);
  };

  const openCreate = () => {
    setEditing(null);
    setForm({ name: '', type: 'bark', enabled: true, config: {}, event_types: [] });
    setShowModal(true);
  };

  const openEdit = (ch: NotificationChannel) => {
    setEditing(ch);
    setForm({
      name: ch.name,
      type: ch.type,
      enabled: ch.enabled,
      config: { ...(ch.config || {}) },
      event_types: ch.event_types || [],
    });
    setShowModal(true);
  };

  const handleSave = async () => {
    const meta = CHANNEL_TYPES[form.type];
    for (const f of meta.fields) {
      if (f.required && !String(form.config[f.key] || '').trim()) {
        showToast('error', `请填写 ${f.label}`);
        return;
      }
    }
    if (!form.name.trim()) {
      showToast('error', '请填写渠道名称');
      return;
    }
    setSaving(true);
    try {
      const payload = { name: form.name.trim(), type: form.type, config: form.config, event_types: form.event_types, enabled: form.enabled };
      if (editing) {
        await updateNotificationChannel(editing.id, payload);
      } else {
        await createNotificationChannel(payload);
      }
      setShowModal(false);
      await load();
      showToast('success', editing ? '已更新' : '已创建');
    } catch (e: any) {
      console.error('保存失败', e);
      showToast('error', e?.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (ch: NotificationChannel) => {
    if (!confirm(`确认删除渠道「${ch.name}」吗？已绑定该渠道的账号会自动解绑。`)) return;
    try {
      await deleteNotificationChannel(ch.id);
      await load();
      showToast('success', '已删除');
    } catch (e: any) {
      showToast('error', e?.message || '删除失败');
    }
  };

  const handleToggleEnabled = async (ch: NotificationChannel) => {
    try {
      await updateNotificationChannel(ch.id, {
        enabled: !ch.enabled,
      });
      setChannels(prev => prev.map(c => c.id === ch.id ? { ...c, enabled: !c.enabled } : c));
    } catch (e: any) {
      showToast('error', e?.message || '切换失败');
    }
  };

  const handleTest = async (ch: NotificationChannel) => {
    setTestingId(ch.id);
    try {
      await testNotificationChannel(ch.id);
      showToast('success', '测试通知已发送，请检查对应渠道');
    } catch (e: any) {
      showToast('error', e?.message || '发送失败');
    } finally {
      setTestingId('');
    }
  };

  const meta = CHANNEL_TYPES[form.type];

  return (
    <div className="space-y-8 animate-fade-in">
      {/* Header */}
      <div className="flex flex-col md:flex-row justify-between md:items-end gap-4">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">通知设置</h2>
          <p className="text-gray-500 mt-2 font-medium">配置通知渠道，账号异常时主动推送告警</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={load}
            className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            刷新
          </button>
          <button
            onClick={openCreate}
            className="ios-btn-primary px-5 py-2.5 rounded-xl font-bold flex items-center gap-2"
          >
            <Plus className="w-4 h-4" />
            新建渠道
          </button>
        </div>
      </div>

      {/* 说明 */}
      <div className="ios-card rounded-xl p-5 bg-blue-50/50 border border-blue-100">
        <div className="flex items-start gap-3">
          <Bell className="w-5 h-5 text-[#0094f7] mt-0.5 flex-shrink-0" />
          <div className="text-sm text-gray-700 leading-6">
            配置通知渠道并在「账号管理 → 编辑」里绑定后，以下事件会主动推送到该账号绑定的渠道：
            <ul className="mt-2 space-y-1 text-gray-600">
              <li>• <strong>账号 session 失效</strong>：系统正在尝试自动恢复（警告）</li>
              <li>• <strong>自动恢复失败</strong>：账号已停止，需人工处理（严重）</li>
              <li>• <strong>触发风控验证</strong>：可能需要扫码完成验证（警告）</li>
            </ul>
          </div>
        </div>
      </div>

      {/* 渠道列表 */}
      {loading ? (
        <div className="flex justify-center py-20">
          <Loader2 className="w-8 h-8 text-[#0094f7] animate-spin" />
        </div>
      ) : channels.length === 0 ? (
        <div className="ios-card rounded-xl p-12 bg-white text-center">
          <Bell className="w-12 h-12 text-gray-300 mx-auto mb-4" />
          <p className="text-gray-500 font-medium">还没有配置任何通知渠道</p>
          <p className="text-gray-400 text-sm mt-1">点击右上角「新建渠道」开始配置</p>
        </div>
      ) : (
        <div className="space-y-3">
          {channels.map(ch => {
            const tMeta = CHANNEL_TYPES[ch.type] || CHANNEL_TYPES.webhook;
            const Icon = tMeta.icon;
            return (
              <div key={ch.id} className="ios-card rounded-xl p-5 bg-white flex items-center gap-4">
                <div className="w-11 h-11 rounded-xl bg-gray-100 flex items-center justify-center flex-shrink-0">
                  <Icon className="w-5 h-5 text-gray-600" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-bold text-gray-900 truncate">{ch.name}</span>
                    <span className="text-xs px-2 py-0.5 rounded-full bg-gray-100 text-gray-600 font-medium">{tMeta.label}</span>
                    {!ch.enabled && (
                      <span className="text-xs px-2 py-0.5 rounded-full bg-gray-100 text-gray-400 font-medium">已停用</span>
                    )}
                  </div>
                  <div className="text-xs text-gray-500 mt-1 truncate">
                    {tMeta.fields.map(f => ch.config?.[f.key]).filter(Boolean).map((v, i) => (
                      <span key={i} className="mr-3">{String(v).length > 40 ? String(v).slice(0, 40) + '…' : String(v)}</span>
                    ))}
                  </div>
                  <div className="text-xs text-gray-400 mt-1 truncate">
                    订阅：{notificationEventSummary(ch.event_types)}
                  </div>
                </div>
                <div className="flex items-center gap-2 flex-shrink-0">
                  <button
                    onClick={() => handleToggleEnabled(ch)}
                    className={`px-3 py-1.5 rounded-lg text-xs font-bold transition-colors ${ch.enabled ? 'bg-green-50 text-green-700 hover:bg-green-100' : 'bg-gray-100 text-gray-500 hover:bg-gray-200'}`}
                  >
                    {ch.enabled ? '启用中' : '已停用'}
                  </button>
                  <button
                    onClick={() => handleTest(ch)}
                    disabled={testingId === ch.id}
                    className="px-3 py-1.5 rounded-lg text-xs font-bold bg-blue-50 text-[#0094f7] hover:bg-blue-100 transition-colors flex items-center gap-1 disabled:opacity-50"
                  >
                    {testingId === ch.id ? <Loader2 className="w-3 h-3 animate-spin" /> : <Send className="w-3 h-3" />}
                    测试
                  </button>
                  <button
                    onClick={() => openEdit(ch)}
                    className="w-8 h-8 rounded-lg bg-gray-100 hover:bg-gray-200 flex items-center justify-center text-gray-600 transition-colors"
                    title="编辑"
                  >
                    <Edit2 className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleDelete(ch)}
                    className="w-8 h-8 rounded-lg bg-red-50 hover:bg-red-100 flex items-center justify-center text-red-500 transition-colors"
                    title="删除"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* SMTP 邮件配置（系统级，用于邮件通知渠道） */}
      {isAdmin && (
      <section className="ios-card rounded-xl p-6 bg-white space-y-5">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-lg font-extrabold text-gray-800">SMTP 邮件配置</h3>
            <p className="text-sm text-gray-500 mt-1">系统级邮件发送服务，供邮件通知渠道复用</p>
          </div>
          <div className="p-2 rounded-xl bg-blue-50 text-blue-600">
            <Mail className="w-5 h-5" />
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="space-y-2">
            <label className="block text-sm font-bold text-gray-800">SMTP 服务器</label>
            <input
              type="text"
              value={smtp.smtp_server || ''}
              onChange={e => setSmtp({ ...smtp, smtp_server: e.target.value })}
              placeholder="smtp.qq.com"
              className="w-full ios-input px-4 py-3 rounded-xl text-sm"
            />
          </div>
          <div className="space-y-2">
            <label className="block text-sm font-bold text-gray-800">SMTP 端口</label>
            <input
              type="number"
              value={smtp.smtp_port || 587}
              onChange={e => setSmtp({ ...smtp, smtp_port: parseInt(e.target.value) || 587 })}
              placeholder="587"
              className="w-full ios-input px-4 py-3 rounded-xl text-sm"
            />
          </div>
        </div>

        <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-800">发件邮箱</label>
          <input
            type="email"
            value={smtp.smtp_user || ''}
            onChange={e => setSmtp({ ...smtp, smtp_user: e.target.value })}
            placeholder="your-email@qq.com"
            className="w-full ios-input px-4 py-3 rounded-xl text-sm"
          />
        </div>

        <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-800">邮箱密码 / 授权码</label>
          <div className="relative">
            <input
              type={showSmtpPassword ? 'text' : 'password'}
              value={smtp.smtp_password || ''}
              onChange={e => setSmtp({ ...smtp, smtp_password: e.target.value })}
              placeholder="输入密码或授权码"
              className="w-full ios-input px-4 py-3 pr-12 rounded-xl text-sm"
            />
            <button
              type="button"
              onClick={() => setShowSmtpPassword(!showSmtpPassword)}
              className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600 transition-colors"
            >
              {showSmtpPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
            </button>
          </div>
          <p className="text-xs text-gray-500">QQ 邮箱需使用授权码（QQ 邮箱设置 → 账号 → 开启 SMTP → 生成授权码）</p>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-800">发件人显示名（可选）</label>
          <input
            type="text"
            value={smtp.smtp_from_name || ''}
            onChange={e => setSmtp({ ...smtp, smtp_from_name: e.target.value })}
            placeholder="闲鱼自动回复系统"
            className="w-full ios-input px-4 py-3 rounded-xl text-sm"
          />
          </div>
          <div className="space-y-2">
            <label className="block text-sm font-bold text-gray-800">发件邮箱地址</label>
            <input
              type="email"
              value={smtp.smtp_from_address || smtp.smtp_user || ''}
              onChange={e => setSmtp({ ...smtp, smtp_from_address: e.target.value })}
              placeholder="your-email@qq.com"
              className="w-full ios-input px-4 py-3 rounded-xl text-sm"
            />
          </div>
        </div>

        <button
          onClick={handleSaveSmtp}
          disabled={smtpSaving}
          className="ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center gap-2 disabled:opacity-50"
        >
          {smtpSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
          {smtpSaving ? '保存中...' : '保存 SMTP 配置'}
        </button>
      </section>
      )}

      {/* 新建/编辑弹窗 */}
      {showModal && createPortal(
        <div className="modal-overlay">
          <div className="modal-container" style={{ maxWidth: '36rem' }}>
            <div className="px-6 py-5 border-b border-gray-100 flex items-center justify-between">
              <h3 className="text-xl font-extrabold text-gray-900">{editing ? '编辑通知渠道' : '新建通知渠道'}</h3>
              <button
                onClick={() => setShowModal(false)}
                className="w-10 h-10 rounded-2xl bg-gray-100 hover:bg-gray-200 flex items-center justify-center"
              >
                <X className="w-5 h-5 text-gray-600" />
              </button>
            </div>

            <div className="px-6 py-5 space-y-5 overflow-y-auto flex-1 min-h-0">
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-800">渠道名称</label>
                <input
                  type="text"
                  value={form.name}
                  onChange={e => setForm({ ...form, name: e.target.value })}
                  placeholder="例如：我的 Bark"
                  className="w-full ios-input px-4 py-3 rounded-xl text-sm"
                />
              </div>

              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-800">渠道类型</label>
                <div className="grid grid-cols-3 sm:grid-cols-4 gap-2">
                  {(Object.keys(CHANNEL_TYPES) as NotificationChannelType[]).map(t => {
                    const m = CHANNEL_TYPES[t];
                    const TIcon = m.icon;
                    const selected = form.type === t;
                    return (
                      <button
                        key={t}
                        type="button"
                        onClick={() => setForm({ ...form, type: t, config: {} })}
                        className={`p-3 rounded-xl border-2 flex flex-col items-center gap-1.5 transition-all ${selected ? 'border-[#0094f7] bg-blue-50' : 'border-gray-100 hover:border-gray-300'}`}
                      >
                        <TIcon className={`w-5 h-5 ${selected ? 'text-[#0094f7]' : 'text-gray-500'}`} />
                        <span className={`text-xs font-bold ${selected ? 'text-[#0094f7]' : 'text-gray-600'}`}>{m.label}</span>
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* 配置指南：选中类型后显示对应获取步骤 */}
              <div className="rounded-xl bg-amber-50 border border-amber-200 p-4 space-y-2">
                <div className="flex items-center gap-2">
                  <Bell className="w-4 h-4 text-amber-600" />
                  <span className="text-sm font-bold text-amber-800">如何获取 {meta.label} 配置？</span>
                </div>
                <ol className="space-y-1.5 text-xs text-amber-900/90 leading-5 list-decimal pl-5">
                  {meta.guide.steps.map((step, i) => (
                    <li key={i}>{step}</li>
                  ))}
                </ol>
                {meta.guide.urlFormat && (
                  <div className="text-xs text-amber-900/90">
                    <span className="font-bold">格式示例：</span>
                    <code className="mt-1 block bg-amber-100/70 px-2.5 py-1.5 rounded-lg break-all font-mono text-[11px]">{meta.guide.urlFormat}</code>
                  </div>
                )}
                {meta.guide.note && (
                  <p className="text-xs text-amber-800/80 leading-5 pt-1">💡 {meta.guide.note}</p>
                )}
              </div>

              <div className="space-y-3">
                {meta.fields.map(f => (
                  <div key={f.key} className="space-y-2">
                    <label className="block text-sm font-bold text-gray-800">
                      {f.label}
                      {f.required && <span className="text-red-500 ml-1">*</span>}
                    </label>
                    <input
                      type={f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text'}
                      value={String(form.config[f.key] || '')}
                      onChange={e => setForm({ ...form, config: { ...form.config, [f.key]: e.target.value } })}
                      placeholder={f.placeholder}
                      className="w-full ios-input px-4 py-3 rounded-xl text-sm"
                    />
                    {f.help && <p className="text-xs text-gray-500">{f.help}</p>}
                  </div>
                ))}
              </div>

              <div className="space-y-3">
                <div>
                  <label className="block text-sm font-bold text-gray-800">通知内容</label>
                  <p className="text-xs text-gray-500 mt-1">不选择表示接收全部通知；选择后仅接收勾选类型。</p>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  {NOTIFICATION_EVENTS.map(event => {
                    const checked = form.event_types.includes(event.value);
                    return (
                      <button
                        key={event.value}
                        type="button"
                        onClick={() => {
                          const next = checked
                            ? form.event_types.filter(item => item !== event.value)
                            : [...form.event_types, event.value];
                          setForm({ ...form, event_types: next });
                        }}
                        className={`text-left rounded-xl border px-3 py-2.5 transition-colors ${checked ? 'border-[#0094f7] bg-blue-50' : 'border-gray-100 hover:border-gray-300'}`}
                      >
                        <div className="flex items-center gap-2">
                          <span className={`w-4 h-4 rounded border flex items-center justify-center ${checked ? 'bg-[#0094f7] border-[#0094f7]' : 'border-gray-300'}`}>
                            {checked && <Check className="w-3 h-3 text-white" />}
                          </span>
                          <span className={`text-sm font-bold ${checked ? 'text-[#0094f7]' : 'text-gray-800'}`}>{event.label}</span>
                        </div>
                        <p className="text-xs text-gray-500 mt-1 pl-6 leading-5">{event.description}</p>
                      </button>
                    );
                  })}
                </div>
              </div>

              <label className="flex items-center gap-3 cursor-pointer">
                <button
                  type="button"
                  onClick={() => setForm({ ...form, enabled: !form.enabled })}
                  className={`relative w-11 h-6 rounded-full transition-colors ${form.enabled ? 'bg-[#0094f7]' : 'bg-gray-300'}`}
                >
                  <span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform ${form.enabled ? 'translate-x-5' : ''}`} />
                </button>
                <span className="text-sm font-bold text-gray-800">启用此渠道</span>
              </label>
            </div>

            <div className="px-6 py-4 border-t border-gray-100 flex items-center justify-end gap-3">
              <button
                onClick={() => setShowModal(false)}
                className="px-5 py-2.5 rounded-xl bg-gray-100 hover:bg-gray-200 font-bold text-gray-700 transition-colors"
              >
                取消
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                className="ios-btn-primary px-6 py-2.5 rounded-xl font-bold flex items-center gap-2 disabled:opacity-50"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}
                {saving ? '保存中...' : '保存'}
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* Toast */}
      {toast && (
        <div className="fixed bottom-8 left-1/2 -translate-x-1/2 z-[10000] px-5 py-3 rounded-xl shadow-lg font-bold text-sm flex items-center gap-2 animate-fade-in"
          style={{ background: toast.type === 'success' ? '#10b981' : '#ef4444', color: 'white' }}>
          {toast.type === 'success' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}
          {toast.text}
        </div>
      )}
    </div>
  );
};

export default Notifications;
