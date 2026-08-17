import React from 'react';
import { createPortal } from 'react-dom';
import { Bell, Check, Clock, Eye, EyeOff, Key, Loader2, X } from 'lucide-react';
import { SquareQRCode } from './SquareQRCode';
import type { NotificationChannel } from '../../../../shared/api-contract';
import type { AccountEditForm, AccountEditModalProps } from '../types';

// AccountEditModal 负责账号备注、暂停、登录信息和通知绑定的编辑界面。
export const AccountEditModal: React.FC<AccountEditModalProps> = ({
  account,
  editForm,
  setEditForm,
  saving,
  onClose,
  onSave,
  onRestartPause,
  longLogin,
  onToggleLongLogin,
  passwordLoginView,
  onPasswordLogin,
  onCancelPasswordLogin,
  notifChannels,
  selectedChannelIds,
  bindingsLoaded,
  bindingsLoading,
  bindingsLoadError,
  onRetryBindings,
  onToggleChannel,
  onSettingsDirty,
}) => {
  // updateField 用函数式更新避免连续输入事件读取旧表单快照。
  const updateField = <K extends keyof AccountEditForm>(field: K, value: AccountEditForm[K]) => {
    setEditForm(
      // current 是事件发生前的最新编辑表单。
      current => ({ ...current, [field]: value }),
    );
  };

  // handleClose 关闭编辑弹窗并取消未完成的异步操作。
  const handleClose = () => void onClose();
  // handleSave 提交编辑表单并让父级刷新账号数据。
  const handleSave = () => void onSave();
  // handleRemarkChange 更新账号备注字段。
  const handleRemarkChange = (event: React.ChangeEvent<HTMLInputElement>) => updateField('remark', event.target.value);
  // handleCookieChange 更新账号 Cookie 字段。
  const handleCookieChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => updateField('cookie', event.target.value);
  // handleAutoConfirmToggle 切换自动确认发货设置。
  const handleAutoConfirmToggle = () => updateField('auto_confirm', !editForm.auto_confirm);
  // handlePauseDurationChange 更新账号暂停时长。
  const handlePauseDurationChange = (event: React.ChangeEvent<HTMLInputElement>) => updateField('pause_duration', parseInt(event.target.value, 10) || 0);
  // handleRestartPause 按当前时长立即重新暂停账号。
  const handleRestartPause = () => void onRestartPause();
  // handleLongLoginToggle 切换闲鱼官方长登录设置。
  const handleLongLoginToggle = () => void onToggleLongLogin();
  // handleUsernameChange 更新密码登录用户名。
  const handleUsernameChange = (event: React.ChangeEvent<HTMLInputElement>) => updateField('username', event.target.value);
  // handlePasswordChange 更新密码并清除待清空标记。
  const handlePasswordChange = (event: React.ChangeEvent<HTMLInputElement>) => setEditForm(
    // current 是密码输入前的最新编辑表单。
    current => ({ ...current, login_password: event.target.value, clear_password: false }),
  );
  // handlePasswordVisibilityToggle 切换密码明文显示状态。
  const handlePasswordVisibilityToggle = () => updateField('showLoginPassword', !editForm.showLoginPassword);
  // handleClearPasswordChange 更新密码清空选项。
  const handleClearPasswordChange = (event: React.ChangeEvent<HTMLInputElement>) => setEditForm(
    // current 是勾选清空密码前的最新编辑表单。
    current => ({ ...current, clear_password: event.target.checked, login_password: event.target.checked ? '' : current.login_password }),
  );
  // handleShowBrowserToggle 切换密码登录时是否展示浏览器。
  const handleShowBrowserToggle = () => updateField('show_browser', !editForm.show_browser);
  // handleCancelPasswordLogin 取消正在执行的密码登录。
  const handleCancelPasswordLogin = () => void onCancelPasswordLogin();
  // handlePasswordLogin 启动密码登录刷新授权。
  const handlePasswordLogin = () => void onPasswordLogin();
  // handleRetryBindings 重新加载当前账号的通知绑定。
  const handleRetryBindings = () => void onRetryBindings();
  // handleChannelClick 切换通知渠道并标记绑定表单已修改。
  const handleChannelClick = (channelId: number) => {
    if (!bindingsLoaded) return;
    onToggleChannel(channelId);
    onSettingsDirty();
  };
  // renderNotificationChannel 渲染单个通知渠道选项。
  const renderNotificationChannel = (channel: NotificationChannel) => {
    // checked 表示当前通知渠道是否已被选中。
    const checked = selectedChannelIds.includes(Number(channel.id));
    // handleChannelButtonClick 把渠道 ID 绑定到当前选项的点击事件。
    const handleChannelButtonClick = () => handleChannelClick(Number(channel.id));
    return (
      <label key={channel.id} className="flex items-center gap-3 p-3 rounded-xl border border-gray-200 hover:bg-gray-50 cursor-pointer transition-colors">
        <button
          type="button"
          onClick={handleChannelButtonClick}
          disabled={!bindingsLoaded}
          className={`w-5 h-5 rounded-md border-2 flex items-center justify-center transition-colors flex-shrink-0 ${checked ? 'bg-brand border-brand' : 'bg-white border-gray-300'}`}
        >
          {checked && <Check className="w-3.5 h-3.5 text-white" />}
        </button>
        <div className="flex-1 min-w-0">
          <div className="font-bold text-gray-900 text-sm">{channel.name}</div>
          <div className="text-xs text-gray-500">{channel.type}{channel.enabled ? '' : ' · 已停用'}</div>
        </div>
      </label>
    );
  };

  return createPortal(
    <div className="modal-overlay-centered">
      <div className="modal-container" style={{ maxWidth: '600px' }}>
        <div className="modal-header">
          <div>
            <h3 className="text-2xl font-extrabold text-gray-900">编辑账号</h3>
            <p className="text-sm text-gray-500 mt-1">{account.nickname || account.remark || account.id}</p>
          </div>
          <button onClick={handleClose} className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0">
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>

        <div className="modal-body space-y-6">
          <div>
            <label className="block text-sm font-bold text-gray-700 mb-2">账号ID</label>
            <input type="text" value={account.id} disabled className="w-full ios-input px-4 py-3 rounded-xl bg-gray-50 text-gray-500" />
          </div>

          <div>
            <label className="block text-sm font-bold text-gray-700 mb-2">备注</label>
            <input
              type="text"
              value={editForm.remark}
              onChange={handleRemarkChange}
              placeholder="为账号添加备注"
              className="w-full ios-input px-4 py-3 rounded-xl"
            />
          </div>

          <div>
            <label className="block text-sm font-bold text-gray-700 mb-2">Cookie</label>
            <textarea
              value={editForm.cookie}
              onChange={handleCookieChange}
              placeholder="更新账号Cookie"
              className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none font-mono text-xs"
            />
            <p className="text-xs text-gray-500 mt-1">当前Cookie长度: {editForm.cookie.length} 字符</p>
          </div>

          <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
            <div>
              <div className="font-bold text-gray-900 flex items-center gap-2"><Check className="w-4 h-4 text-green-500" />自动确认发货</div>
              <div className="text-xs text-gray-500">自动将闲鱼订单标记为已发货</div>
            </div>
            <button
              type="button"
              onClick={handleAutoConfirmToggle}
              className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${editForm.auto_confirm ? 'bg-brand' : 'bg-gray-300'}`}
            >
              <span className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${editForm.auto_confirm ? 'translate-x-6' : 'translate-x-0'}`} />
            </button>
          </div>

          <div>
            <label className="block text-sm font-bold text-gray-700 mb-2 flex items-center gap-2"><Clock className="w-4 h-4 text-blue-500" />暂停处理时长（分钟）</label>
            <input
              type="number"
              value={editForm.pause_duration}
              onChange={handlePauseDurationChange}
              placeholder="0"
              min="0"
              max="1440"
              className="w-full ios-input px-4 py-3 rounded-xl"
            />
            <p className="text-xs text-gray-500 mt-1">设置后会暂停处理该账号的订单，到时间后自动恢复</p>
            {editForm.pause_duration > 0 && !account.paused && editForm.pause_duration === (account.pause_duration || 0) && (
              <button type="button" disabled={saving} onClick={handleRestartPause} className="mt-3 px-4 py-2 rounded-xl bg-amber-50 text-amber-700 hover:bg-amber-100 text-sm font-bold disabled:opacity-50">
                立即按当前时长重新暂停
              </button>
            )}
          </div>

          <div className="border-t border-gray-200 pt-6">
            <h3 className="text-lg font-bold text-gray-900 mb-4 flex items-center gap-2"><Key className="w-5 h-5 text-blue-500" />登录信息</h3>
            <div className="space-y-4">
              <div className="flex items-center justify-between gap-4 rounded-xl bg-blue-50 p-4">
                <div>
                  <div className="font-bold text-gray-900">保存登录信息</div>
                  <div className="mt-1 text-xs text-gray-500">状态直接读取并修改闲鱼官方长登录设置</div>
                  {longLogin.error && <div className="mt-1 text-xs text-red-600">{longLogin.error}</div>}
                </div>
                <button
                  type="button"
                  aria-label="保存登录信息"
                  disabled={longLogin.loading || longLogin.saving || !longLogin.canOpen}
                  onClick={handleLongLoginToggle}
                  className={`relative h-8 w-14 flex-shrink-0 rounded-full transition-colors ${longLogin.enabled ? 'bg-brand' : 'bg-gray-300'} disabled:cursor-not-allowed disabled:opacity-50`}
                >
                  <span className={`absolute left-1 top-1 h-6 w-6 rounded-full bg-white shadow-md transition-transform ${longLogin.enabled ? 'translate-x-6' : 'translate-x-0'}`} />
                </button>
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">用户名</label>
                <input type="text" value={editForm.username} onChange={handleUsernameChange} placeholder="闲鱼账号/手机号" className="w-full ios-input px-4 py-3 rounded-xl" />
              </div>

              <div>
                <label className="block text-sm font-bold text-gray-700 mb-2">登录密码</label>
                <div className="relative">
                  <input
                    type={editForm.showLoginPassword ? 'text' : 'password'}
                    value={editForm.login_password}
                    onChange={handlePasswordChange}
                    placeholder="用于自动登录"
                    className="w-full ios-input px-4 py-3 rounded-xl pr-12"
                  />
                  <button type="button" onClick={handlePasswordVisibilityToggle} className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                    {editForm.showLoginPassword ? <EyeOff className="w-5 h-5" /> : <Eye className="w-5 h-5" />}
                  </button>
                </div>
                <label className="mt-3 flex items-center gap-3 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={editForm.clear_password}
                    onChange={handleClearPasswordChange}
                    className="w-4 h-4 accent-brand"
                  />
                  <span className="text-sm font-bold text-gray-700">清空已保存密码</span>
                </label>
              </div>

              <div className="flex items-center justify-between">
                <div>
                  <div className="font-bold text-gray-900">登录时显示浏览器</div>
                  <div className="text-xs text-gray-500">调试时可开启查看登录过程</div>
                </div>
                <button type="button" onClick={handleShowBrowserToggle} className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${editForm.show_browser ? 'bg-brand' : 'bg-gray-300'}`}>
                  <span className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${editForm.show_browser ? 'translate-x-6' : 'translate-x-0'}`} />
                </button>
              </div>

              <div className="rounded-xl border border-blue-100 bg-blue-50 p-4 space-y-3">
                <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                  <div>
                    <div className="font-bold text-blue-950">立即执行账号密码登录</div>
                    <div className="text-xs text-blue-700 mt-1">需要在上方重新输入本次登录密码；成功后后端会更新 Cookie 和保存的登录信息。</div>
                  </div>
                  {(passwordLoginView.status === 'processing' || passwordLoginView.status === 'verification_required') ? (
                    <button type="button" onClick={handleCancelPasswordLogin} className="px-4 py-2 rounded-xl bg-white text-red-600 font-bold text-sm border border-red-100">取消登录</button>
                  ) : (
                    <button type="button" onClick={handlePasswordLogin} className="px-4 py-2 rounded-xl bg-blue-600 text-white font-bold text-sm whitespace-nowrap">密码登录刷新授权</button>
                  )}
                </div>
                {passwordLoginView.message && (
                  <div className={`text-sm font-medium ${passwordLoginView.status === 'failed' ? 'text-red-700' : passwordLoginView.status === 'success' ? 'text-green-700' : 'text-blue-800'}`}>{passwordLoginView.message}</div>
                )}
                {passwordLoginView.status === 'verification_required' && (
                  <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-amber-900">
                    <div className="font-extrabold">账号已触发平台风控，需要完成人脸识别</div>
                    <div className="text-xs mt-1 leading-5">请在闲鱼 App 或已打开的登录浏览器中按提示完成验证。本页面不会提供可直接打开的风控链接。</div>
                    {passwordLoginView.qrCodeUrl && <div className="mt-3 aspect-square w-48 overflow-hidden rounded-xl border bg-white"><SquareQRCode src={passwordLoginView.qrCodeUrl} alt="密码登录风控二维码" className="p-2" /></div>}
                  </div>
                )}
              </div>
            </div>
          </div>

          {(notifChannels.length > 0 || bindingsLoading || bindingsLoadError) && (
            <div className="border-t border-gray-200 pt-6">
              <h3 className="text-lg font-bold text-gray-900 mb-1 flex items-center gap-2"><Bell className="w-5 h-5 text-blue-500" />通知渠道绑定</h3>
              <p className="text-xs text-gray-500 mb-4">勾选后，该账号的 token 失效、自动恢复失败、风控验证等事件会推送到这些渠道</p>
              {bindingsLoading && <div className="flex items-center gap-2 text-sm text-gray-500"><Loader2 className="w-4 h-4 animate-spin" />正在加载通知绑定</div>}
              {bindingsLoadError && !bindingsLoading && (
                <div className="mb-3 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 flex items-center justify-between gap-3">
                  <span>{bindingsLoadError}</span>
                  <button type="button" className="font-bold whitespace-nowrap" onClick={handleRetryBindings}>重试</button>
                </div>
              )}
              <div className="space-y-2">
                {notifChannels.map(renderNotificationChannel)}
              </div>
            </div>
          )}
        </div>

        <div className="modal-footer">
          <div className="flex gap-3 w-full">
            <button onClick={handleClose} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors" disabled={saving}>取消</button>
            <button onClick={handleSave} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2" disabled={saving}>
              {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}
              {saving ? '保存中...' : '保存'}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
};
