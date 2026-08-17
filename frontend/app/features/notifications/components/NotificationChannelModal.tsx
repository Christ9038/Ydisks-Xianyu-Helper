import React from 'react';
import { createPortal } from 'react-dom';
import { Bell, Check, Eye, EyeOff, Loader2, X } from 'lucide-react';
import { enableCustomSMTP } from '../../../../notificationEmailConfig';
import { notificationChannelTypes } from '../state';
import { NotificationEventSelector } from './NotificationEventSelector';
import type { NotificationChannel, NotificationChannelType, NotificationEventType, SystemSettings } from '../../../../shared/api-contract';
import type { NotificationForm } from '../types';

// NotificationChannelModalProps 描述渠道编辑弹窗所需的状态和回调。
export interface NotificationChannelModalProps {
  // showModal 表示弹窗是否打开。
  showModal: boolean;
  // editing 表示当前编辑的渠道，新增时为空。
  editing: NotificationChannel | null;
  // form 是当前渠道表单值。
  form: NotificationForm;
  // setForm 更新渠道表单值。
  setForm: React.Dispatch<React.SetStateAction<NotificationForm>>;
  // smtp 是系统 SMTP 配置，用于填充独立 SMTP 初始值。
  smtp: SystemSettings;
  // showChannelSmtpPassword 表示是否显示独立 SMTP 密码。
  showChannelSmtpPassword: boolean;
  // setShowChannelSmtpPassword 切换独立 SMTP 密码显示状态。
  setShowChannelSmtpPassword: React.Dispatch<React.SetStateAction<boolean>>;
  // saving 表示渠道保存请求是否正在执行。
  saving: boolean;
  // onClose 关闭弹窗。
  onClose: () => void;
  // onSave 提交渠道表单。
  onSave: () => void | Promise<void>;
}

// NotificationChannelModal 渲染渠道字段、SMTP 覆盖项和事件绑定表单。
export const NotificationChannelModal: React.FC<NotificationChannelModalProps> = ({ showModal, editing, form, setForm, smtp, showChannelSmtpPassword, setShowChannelSmtpPassword, saving, onClose, onSave }) => {
  // meta 是当前渠道类型对应的字段和指南。
  const meta = notificationChannelTypes[form.type];
  // handleNameChange 更新渠道名称。
  const handleNameChange = (event: React.ChangeEvent<HTMLInputElement>) => setForm(
    // nextFormUpdater 使用函数式更新名称字段。
    current => ({ ...current, name: event.target.value }),
  );
  // handleTypeChange 切换渠道类型并清空旧类型配置。
  const handleTypeChange = (event: React.MouseEvent<HTMLButtonElement>) => {
    // type 是按钮数据属性中的目标渠道类型。
    const type = event.currentTarget.dataset.type as NotificationChannelType | undefined;
    if (!type) return;
    setForm(
      // nextFormUpdater 使用函数式更新渠道类型。
      current => ({ ...current, type, config: {} }),
    );
  };
  // handleConfigFieldChange 更新普通或独立 SMTP 配置字段。
  const handleConfigFieldChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    // key 是输入框数据属性中的配置字段名。
    const key = event.currentTarget.dataset.field;
    if (!key) return;
    setForm(
      // nextFormUpdater 使用函数式更新配置字段。
      current => ({ ...current, config: { ...current.config, [key]: event.target.value } }),
    );
  };
  // handleToggleCustomSMTP 切换邮件渠道是否使用独立 SMTP。
  const handleToggleCustomSMTP = () => setForm(
    // nextFormUpdater 使用函数式更新 SMTP 来源模式。
    current => ({ ...current, config: current.config.use_custom_smtp === true ? { ...current.config, use_custom_smtp: false } : enableCustomSMTP(current.config, smtp) }),
  );
  // handleChannelTlsChange 切换独立 SMTP 的 STARTTLS 并保持模式互斥。
  const handleChannelTlsChange = (event: React.ChangeEvent<HTMLInputElement>) => setForm(
    // nextFormUpdater 使用函数式更新独立 SMTP 的 TLS/SSL 互斥字段。
    current => ({ ...current, config: { ...current.config, smtp_use_tls: event.target.checked, smtp_use_ssl: event.target.checked ? false : current.config.smtp_use_ssl } }),
  );
  // handleChannelSslChange 切换独立 SMTP 的 SSL 并保持模式互斥。
  const handleChannelSslChange = (event: React.ChangeEvent<HTMLInputElement>) => setForm(
    // nextFormUpdater 使用函数式更新独立 SMTP 的 SSL/TLS 互斥字段。
    current => ({ ...current, config: { ...current.config, smtp_use_ssl: event.target.checked, smtp_use_tls: event.target.checked ? false : current.config.smtp_use_tls } }),
  );
  // handleToggleEvent 使用函数式更新切换事件绑定。
  const handleToggleEvent = (event: NotificationEventType) => setForm(
    // nextFormUpdater 使用函数式更新事件绑定集合。
    current => ({ ...current, event_types: current.event_types.includes(event) ? current.event_types.filter(
      // item 是待保留的已有事件值。
      item => item !== event,
    ) : [...current.event_types, event] }),
  );
  // handleToggleEnabled 使用函数式更新切换渠道启用状态。
  const handleToggleEnabled = () => setForm(
    // nextFormUpdater 使用函数式更新启用状态。
    current => ({ ...current, enabled: !current.enabled }),
  );
  // handleToggleChannelPassword 切换独立 SMTP 密码明文显示。
  const handleToggleChannelPassword = () => setShowChannelSmtpPassword(
    // nextPasswordState 使用函数式更新密码显示状态。
    value => !value,
  );
  // renderGuideStep 渲染当前渠道指南中的单个步骤。
  const renderGuideStep = (step: string, index: number) => <li key={index}>{step}</li>;

  if (!showModal) return null;

  return createPortal(
    <div className="modal-overlay">
      <div className="modal-container" style={{ maxWidth: '36rem' }}>
        <div className="px-6 py-5 border-b border-gray-100 flex items-center justify-between"><h3 className="text-xl font-extrabold text-gray-900">{editing ? '编辑通知渠道' : '新建通知渠道'}</h3><button onClick={onClose} className="w-10 h-10 rounded-2xl bg-gray-100 hover:bg-gray-200 flex items-center justify-center"><X className="w-5 h-5 text-gray-600" /></button></div>
        <div className="px-6 py-5 space-y-5 overflow-y-auto flex-1 min-h-0">
          <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">渠道名称</label><input type="text" value={form.name} onChange={handleNameChange} placeholder="例如：我的 Bark" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
          <div className="space-y-2">
            <label className="block text-sm font-bold text-gray-800">渠道类型</label>
            <div className="grid grid-cols-3 sm:grid-cols-4 gap-2">
              {(Object.keys(notificationChannelTypes) as NotificationChannelType[]).map(
                // type 是当前渠道类型选择项。
                type => {
                  // channelMeta 是当前类型的展示元数据。
                  const channelMeta = notificationChannelTypes[type];
                  // TypeIcon 是当前类型的图标组件。
                  const TypeIcon = channelMeta.icon;
                  // selected 表示当前类型是否已选中。
                  const selected = form.type === type;
                  return <button key={type} data-type={type} type="button" onClick={handleTypeChange} className={`p-3 rounded-xl border-2 flex flex-col items-center gap-1.5 transition-all ${selected ? 'border-brand bg-blue-50' : 'border-gray-100 hover:border-gray-300'}`}><TypeIcon className={`w-5 h-5 ${selected ? 'text-brand' : 'text-gray-500'}`} /><span className={`text-xs font-bold ${selected ? 'text-brand' : 'text-gray-600'}`}>{channelMeta.label}</span></button>;
                },
              )}
            </div>
          </div>
          <div className="rounded-xl bg-amber-50 border border-amber-200 p-4 space-y-2"><div className="flex items-center gap-2"><Bell className="w-4 h-4 text-amber-600" /><span className="text-sm font-bold text-amber-800">如何获取 {meta.label} 配置？</span></div><ol className="space-y-1.5 text-xs text-amber-900/90 leading-5 list-decimal pl-5">{meta.guide.steps.map(renderGuideStep)}</ol>{meta.guide.urlFormat && <div className="text-xs text-amber-900/90"><span className="font-bold">格式示例：</span><code className="mt-1 block bg-amber-100/70 px-2.5 py-1.5 rounded-lg break-all font-mono text-[11px]">{meta.guide.urlFormat}</code></div>}{meta.guide.note && <p className="text-xs text-amber-800/80 leading-5 pt-1">💡 {meta.guide.note}</p>}</div>
          <div className="space-y-3">
            {meta.fields.map(
              // field 是当前渠道配置字段。
              field => <div key={field.key} className="space-y-2"><label className="block text-sm font-bold text-gray-800">{field.label}{field.required && <span className="text-red-500 ml-1">*</span>}</label><input data-field={field.key} type={field.type === 'password' ? 'password' : field.type === 'number' ? 'number' : 'text'} value={String(form.config[field.key] || '')} onChange={handleConfigFieldChange} placeholder={field.placeholder} className="w-full ios-input px-4 py-3 rounded-xl text-sm" />{field.help && <p className="text-xs text-gray-500">{field.help}</p>}</div>,
            )}
          </div>
          {form.type === 'email' && (
            <div className="overflow-hidden rounded-2xl border border-blue-100 bg-blue-50/40">
              <div className="flex items-center justify-between gap-4 p-4"><div><div className="text-sm font-extrabold text-gray-900">SMTP 来源</div><p className="mt-1 text-xs leading-5 text-gray-500">{form.config.use_custom_smtp === true ? '当前渠道使用一套完整、独立的发件配置。' : '当前渠道完整继承系统 SMTP，只单独设置收件邮箱。'}</p></div><button type="button" onClick={handleToggleCustomSMTP} className={`relative h-7 w-12 shrink-0 rounded-full transition-colors ${form.config.use_custom_smtp === true ? 'bg-brand' : 'bg-gray-300'}`} aria-label="使用独立 SMTP"><span className={`absolute left-1 top-1 h-5 w-5 rounded-full bg-white shadow-sm transition-transform ${form.config.use_custom_smtp === true ? 'translate-x-5' : ''}`} /></button></div>
              {form.config.use_custom_smtp === true && (
                <div className="space-y-4 border-t border-blue-100 bg-white/80 p-4">
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><div className="space-y-2"><label className="block text-sm font-bold text-gray-800">SMTP 服务器 <span className="text-red-500">*</span></label><input data-field="smtp_server" type="text" value={String(form.config.smtp_server || '')} onChange={handleConfigFieldChange} placeholder="smtp.qq.com" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div><div className="space-y-2"><label className="block text-sm font-bold text-gray-800">SMTP 端口 <span className="text-red-500">*</span></label><input data-field="smtp_port" type="number" value={String(form.config.smtp_port || 587)} onChange={handleConfigFieldChange} placeholder="587" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div></div>
                  <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">登录邮箱 <span className="text-red-500">*</span></label><input data-field="smtp_user" type="email" value={String(form.config.smtp_user || '')} onChange={handleConfigFieldChange} placeholder="your-email@qq.com" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
                  <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">密码 / 授权码 <span className="text-red-500">*</span></label><div className="relative"><input data-field="smtp_password" type={showChannelSmtpPassword ? 'text' : 'password'} value={String(form.config.smtp_password || '')} onChange={handleConfigFieldChange} placeholder="输入密码或授权码" className="w-full ios-input px-4 py-3 pr-12 rounded-xl text-sm" /><button type="button" onClick={handleToggleChannelPassword} className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600">{showChannelSmtpPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}</button></div></div>
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><div className="space-y-2"><label className="block text-sm font-bold text-gray-800">发件人显示名（可选）</label><input data-field="smtp_from_name" type="text" value={String(form.config.smtp_from_name || '')} onChange={handleConfigFieldChange} placeholder="闲鱼自动回复系统" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div><div className="space-y-2"><label className="block text-sm font-bold text-gray-800">发件邮箱地址 <span className="text-red-500">*</span></label><input data-field="smtp_from_address" type="email" value={String(form.config.smtp_from_address || '')} onChange={handleConfigFieldChange} placeholder="your-email@qq.com" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div></div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2"><label className="flex items-center gap-3 rounded-xl border border-gray-200 p-3 text-xs font-bold text-gray-700"><input type="checkbox" checked={form.config.smtp_use_tls !== false} onChange={handleChannelTlsChange} />STARTTLS（常用于 587）</label><label className="flex items-center gap-3 rounded-xl border border-gray-200 p-3 text-xs font-bold text-gray-700"><input type="checkbox" checked={form.config.smtp_use_ssl === true} onChange={handleChannelSslChange} />SSL/TLS 直连（常用于 465）</label></div>
                </div>
              )}
            </div>
          )}
          <NotificationEventSelector selectedEvents={form.event_types} onToggleEvent={handleToggleEvent} />
          <label className="flex items-center gap-3 cursor-pointer"><button type="button" onClick={handleToggleEnabled} className={`relative w-11 h-6 rounded-full transition-colors ${form.enabled ? 'bg-brand' : 'bg-gray-300'}`}><span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform ${form.enabled ? 'translate-x-5' : ''}`} /></button><span className="text-sm font-bold text-gray-800">启用此渠道</span></label>
        </div>
        <div className="px-6 py-4 border-t border-gray-100 flex items-center justify-end gap-3"><button onClick={onClose} className="px-5 py-2.5 rounded-xl bg-gray-100 hover:bg-gray-200 font-bold text-gray-700 transition-colors">取消</button><button onClick={onSave} disabled={saving} className="ios-btn-primary px-6 py-2.5 rounded-xl font-bold flex items-center gap-2 disabled:opacity-50">{saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}{saving ? '保存中...' : '保存'}</button></div>
      </div>
    </div>,
    document.body,
  );
};
