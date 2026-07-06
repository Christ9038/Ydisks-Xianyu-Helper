import React, { useEffect, useRef, useState } from 'react';
import {
  fetchAIModels,
  getSystemSettings,
  updateLoginCredentials,
  updateSystemSettings,
  verifySession,
} from '../services/api';
import { SystemSettings } from '../types';
import {
  Save, Sparkles, Settings as SettingsIcon,
  Eye, EyeOff, RefreshCw, Database, ChevronDown, Check,
  LockKeyhole, UserRound, ShieldCheck
} from 'lucide-react';

const DEFAULT_AI_API_URL = 'https://dashscope.aliyuncs.com/compatible-mode/v1';

const Settings: React.FC = () => {
  const [settings, setSettings] = useState<SystemSettings | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [aiModels, setAiModels] = useState<string[]>([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelError, setModelError] = useState('');
  const [modelDropdownOpen, setModelDropdownOpen] = useState(false);
  const modelPickerRef = useRef<HTMLDivElement>(null);

  // Password visibility states
  const [showApiKey, setShowApiKey] = useState(false);
  const [showCurrentPassword, setShowCurrentPassword] = useState(false);
  const [showNewPassword, setShowNewPassword] = useState(false);
  const [credentialsSaving, setCredentialsSaving] = useState(false);
  const [credentialsMessage, setCredentialsMessage] = useState<{type: 'success' | 'error'; text: string} | null>(null);
  const [credentials, setCredentials] = useState({
    new_username: '',
    current_password: '',
    new_password: '',
    confirm_password: '',
  });

  useEffect(() => {
    loadSettings();
    verifySession().then(session => {
      if (session.username) {
        setCredentials(current => ({...current, new_username: session.username || ''}));
      }
    }).catch(() => undefined);
  }, []);

  useEffect(() => {
    const handlePointerDown = (event: MouseEvent) => {
      if (!modelPickerRef.current?.contains(event.target as Node)) {
        setModelDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handlePointerDown);
    return () => document.removeEventListener('mousedown', handlePointerDown);
  }, []);

  const loadAIModels = async (source?: SystemSettings | null, openAfterLoad = false) => {
    const current = source || settings;
    const baseUrl = current?.ai_api_url || current?.ai_base_url || DEFAULT_AI_API_URL;
    setModelsLoading(true);
    setModelError('');
    try {
      const models = await fetchAIModels(baseUrl, current?.ai_api_key || '');
      setAiModels(models);
      setModelDropdownOpen(openAfterLoad && models.length > 0);
      if (!current?.ai_model && models.length > 0) {
        setSettings(prev => prev ? { ...prev, ai_model: models[0] } : prev);
      }
    } catch (e) {
      setAiModels([]);
      setModelDropdownOpen(false);
      setModelError((e as Error).message || '读取模型失败');
    } finally {
      setModelsLoading(false);
    }
  };

  const loadSettings = () => {
    setLoading(true);
    getSystemSettings()
      .then(data => {
        setSettings(data);
        loadAIModels(data);
      })
      .finally(() => setLoading(false));
  };

  const handleSave = async () => {
      if(!settings) return;
      setSaving(true);
      try {
        // SMTP 字段已移至「通知设置」页面，这里不保存，避免覆盖那边已存的值。
        const { smtp_server, smtp_port, smtp_user, smtp_password, smtp_from, ...rest } = settings;
        await updateSystemSettings(rest);
        alert('系统配置已保存');
      } catch (e) {
        alert('保存失败：' + (e as Error).message);
      } finally {
        setSaving(false);
      }
  };

  const handleCredentialsSave = async (event: React.FormEvent) => {
    event.preventDefault();
    setCredentialsMessage(null);
    const username = credentials.new_username.trim();
    if (username.length < 3) {
      setCredentialsMessage({type: 'error', text: '用户名至少需要 3 个字符'});
      return;
    }
    if (!credentials.current_password) {
      setCredentialsMessage({type: 'error', text: '请输入当前密码确认身份'});
      return;
    }
    if (credentials.new_password && credentials.new_password.length < 8) {
      setCredentialsMessage({type: 'error', text: '新密码至少需要 8 个字符'});
      return;
    }
    if (credentials.new_password !== credentials.confirm_password) {
      setCredentialsMessage({type: 'error', text: '两次输入的新密码不一致'});
      return;
    }
    setCredentialsSaving(true);
    try {
      const result = await updateLoginCredentials({
        current_password: credentials.current_password,
        new_username: username,
        new_password: credentials.new_password || undefined,
      });
      if (!result.success) {
        setCredentialsMessage({type: 'error', text: result.message || '登录凭据更新失败'});
        return;
      }
      setCredentialsMessage({type: 'success', text: result.message || '登录凭据已更新'});
      window.setTimeout(() => window.location.reload(), 1400);
    } catch (error) {
      setCredentialsMessage({type: 'error', text: (error as Error).message || '登录凭据更新失败'});
    } finally {
      setCredentialsSaving(false);
    }
  };

  if (!settings) return <div className="p-8 text-center text-gray-400">加载配置中...</div>;

  const currentModel = settings.ai_model || '';
  const visibleAIModels = aiModels;

  return (
    <div className="max-w-6xl mx-auto space-y-8 animate-fade-in pb-24">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="w-12 h-12 bg-gray-100 rounded-2xl flex items-center justify-center">
              <SettingsIcon className="w-6 h-6 text-gray-600" />
          </div>
          <div>
              <h2 className="text-3xl font-extrabold text-gray-900">系统设置</h2>
              <p className="text-gray-500 mt-1 text-sm font-medium">配置全局自动化规则与系统参数</p>
          </div>
        </div>
        <button
          onClick={loadSettings}
          className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
        >
          <RefreshCw className="w-4 h-4" />
          刷新
        </button>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* Left Column */}
        <div className="space-y-8">
          {/* Basic Settings */}
          <section className="space-y-4">
            <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
                <div className="p-1.5 rounded-lg bg-gray-100 text-gray-600">
                    <Database className="w-4 h-4" />
                </div>
                基础设置
            </h3>

            <div className="ios-card rounded-xl p-6 bg-white space-y-4">
              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900">允许用户注册</div>
                  <div className="text-xs text-gray-500 mt-1">开启后允许新用户注册账号</div>
                </div>
                <button
                  onClick={() => setSettings({...settings, registration_enabled: !settings.registration_enabled})}
                  className={`w-14 h-8 rounded-full transition-all relative ${
                    settings.registration_enabled ? 'bg-[#0094f7]' : 'bg-gray-300'
                  }`}
                >
                  <div
                    className={`w-6 h-6 bg-white rounded-full absolute top-1 transition-all shadow-md ${
                      settings.registration_enabled ? 'left-7' : 'left-1'
                    }`}
                  />
                </button>
              </div>

              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900">显示默认登录信息</div>
                  <div className="text-xs text-gray-500 mt-1">登录页面显示默认账号密码提示</div>
                </div>
                <button
                  onClick={() => setSettings({...settings, show_default_login_info: !settings.show_default_login_info})}
                  className={`w-14 h-8 rounded-full transition-all relative ${
                    settings.show_default_login_info ? 'bg-[#0094f7]' : 'bg-gray-300'
                  }`}
                >
                  <div
                    className={`w-6 h-6 bg-white rounded-full absolute top-1 transition-all shadow-md ${
                      settings.show_default_login_info ? 'left-7' : 'left-1'
                    }`}
                  />
                </button>
              </div>

              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900">登录滑动验证码</div>
                  <div className="text-xs text-gray-500 mt-1">开启后账号密码登录需要完成滑动验证</div>
                </div>
                <button
                  onClick={() => setSettings({...settings, login_captcha_enabled: !settings.login_captcha_enabled})}
                  className={`w-14 h-8 rounded-full transition-all relative ${
                    settings.login_captcha_enabled ? 'bg-[#0094f7]' : 'bg-gray-300'
                  }`}
                >
                  <div
                    className={`w-6 h-6 bg-white rounded-full absolute top-1 transition-all shadow-md ${
                      settings.login_captcha_enabled ? 'left-7' : 'left-1'
                    }`}
                  />
                </button>
              </div>

              <div className="flex items-center justify-between p-4 bg-gray-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900">启用商品自动同步</div>
                  <div className="text-xs text-gray-500 mt-1">定时自动获取商品信息到本地数据库</div>
                </div>
                <button
                  onClick={() => setSettings({...settings, item_sync_enabled: !settings.item_sync_enabled})}
                  className={`w-14 h-8 rounded-full transition-all relative ${
                    settings.item_sync_enabled ? 'bg-[#0094f7]' : 'bg-gray-300'
                  }`}
                >
                  <div
                    className={`w-6 h-6 bg-white rounded-full absolute top-1 transition-all shadow-md ${
                      settings.item_sync_enabled ? 'left-7' : 'left-1'
                    }`}
                  />
                </button>
              </div>

              <div className="space-y-3 px-4">
                <label className="block text-sm font-bold text-gray-800">商品同步间隔（分钟）</label>
                <input
                  type="number"
                  value={Math.round((settings.item_sync_interval || 600) / 60)}
                  onChange={(e) => {
                    const minutes = parseInt(e.target.value) || 10;
                    setSettings({...settings, item_sync_interval: minutes * 60});
                  }}
                  className="w-full ios-input px-4 py-3 rounded-xl"
                  min="1"
                  max="1440"
                />
                <p className="text-xs text-gray-500">建议：10-60分钟</p>
              </div>

              <div className="space-y-3 px-4">
                <label className="block text-sm font-bold text-gray-800">每次最多同步页数</label>
                <input
                  type="number"
                  value={settings.item_sync_max_pages || 5}
                  onChange={(e) => setSettings({...settings, item_sync_max_pages: parseInt(e.target.value) || 5})}
                  className="w-full ios-input px-4 py-3 rounded-xl"
                  min="1"
                  max="50"
                />
                <p className="text-xs text-gray-500">每页20个商品</p>
              </div>
            </div>
          </section>

          {/* AI Configuration */}
          <section className="space-y-4">
            <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
                <div className="p-1.5 rounded-lg bg-[#0094f7] text-white">
                    <Sparkles className="w-4 h-4" />
                </div>
                AI 智能回复配置
            </h3>

            <div className="ios-card rounded-xl p-6 bg-white space-y-6">
              <div className="space-y-3">
                <label className="block text-sm font-bold text-gray-800">API 地址</label>
                <input
                  type="text"
                  value={settings.ai_api_url || DEFAULT_AI_API_URL}
                  onChange={e => setSettings({...settings, ai_api_url: e.target.value})}
                  className="w-full ios-input px-4 py-3 rounded-xl text-sm"
                  placeholder="https://api.openai.com/v1"
                />
                <p className="text-xs text-gray-500">无需补全 /chat/completions</p>
              </div>

              <div className="space-y-3">
                <label className="block text-sm font-bold text-gray-800">API Key</label>
                <div className="relative">
                  <input
                    type={showApiKey ? 'text' : 'password'}
                    value={settings.ai_api_key || ''}
                    onChange={e => setSettings({...settings, ai_api_key: e.target.value})}
                    className="w-full ios-input px-4 py-3 pr-12 rounded-xl font-mono text-sm"
                    placeholder="sk-..."
                  />
                  <button
                    type="button"
                    onClick={() => setShowApiKey(!showApiKey)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600 transition-colors"
                  >
                    {showApiKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>

              <div className="space-y-3">
                <label className="block text-sm font-bold text-gray-800">模型</label>
                <div ref={modelPickerRef} className="relative flex flex-col sm:flex-row gap-2">
                  <div className="relative flex-1">
                    <input
                      value={currentModel}
                      onFocus={() => aiModels.length > 0 && setModelDropdownOpen(true)}
                      onChange={e => {
                        setSettings({...settings, ai_model: e.target.value});
                        if (aiModels.length > 0) setModelDropdownOpen(true);
                      }}
                      onKeyDown={e => {
                        if (e.key === 'Escape') setModelDropdownOpen(false);
                        if (e.key === 'ArrowDown' && aiModels.length > 0) setModelDropdownOpen(true);
                      }}
                      className="w-full ios-input px-4 py-3 pr-10 rounded-xl"
                      placeholder="从接口读取或手动输入模型名"
                    />
                    <button
                      type="button"
                      onClick={() => aiModels.length > 0 && setModelDropdownOpen(open => !open)}
                      disabled={aiModels.length === 0}
                      className="absolute right-2 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600 disabled:opacity-30"
                      aria-label="展开模型列表"
                    >
                      <ChevronDown className={`w-4 h-4 transition-transform ${modelDropdownOpen ? 'rotate-180' : ''}`} />
                    </button>
                    {modelDropdownOpen && (
                      <div className="absolute left-0 right-0 top-[calc(100%+6px)] z-40 max-h-64 overflow-y-auto rounded-xl border border-gray-200 bg-white shadow-xl shadow-gray-200/70 py-1">
                        {visibleAIModels.length > 0 ? (
                          visibleAIModels.map(model => (
                            <button
                              key={model}
                              type="button"
                              onClick={() => {
                                setSettings({...settings, ai_model: model});
                                setModelDropdownOpen(false);
                              }}
                              className="w-full px-4 py-2.5 text-left text-sm text-gray-700 hover:bg-blue-50 hover:text-[#0094f7] flex items-center justify-between gap-3"
                            >
                              <span className="truncate">{model}</span>
                              {model === currentModel && <Check className="w-4 h-4 shrink-0 text-[#0094f7]" />}
                            </button>
                          ))
                        ) : (
                          <div className="px-4 py-3 text-sm text-gray-400">没有匹配的模型</div>
                        )}
                      </div>
                    )}
                  </div>
                  <button
                    type="button"
                    onClick={() => loadAIModels(undefined, true)}
                    disabled={modelsLoading}
                    className="px-4 py-3 rounded-xl bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-60 font-bold flex items-center justify-center gap-2 whitespace-nowrap"
                  >
                    <RefreshCw className={`w-4 h-4 ${modelsLoading ? 'animate-spin' : ''}`} />
                    读取模型
                  </button>
                </div>
                {modelError ? (
                  <p className="text-xs text-red-500">{modelError}</p>
                ) : (
                  <p className="text-xs text-gray-500">
                    {aiModels.length > 0 ? `已从当前 API 地址读取到 ${aiModels.length} 个模型` : '模型列表从当前 API 地址读取，也可以手动输入模型名'}
                  </p>
                )}
              </div>

              <div className="space-y-3">
                <label className="block text-sm font-bold text-gray-800">默认自动回复内容</label>
                <textarea
                  className="w-full ios-input px-4 py-3 rounded-xl min-h-[100px] text-sm resize-none"
                  value={settings.default_reply || ''}
                  onChange={e => setSettings({...settings, default_reply: e.target.value})}
                  placeholder="设置默认的自动回复内容..."
                ></textarea>
              </div>

              <div className="p-3 bg-blue-50 rounded-xl text-xs text-blue-700">
                <strong>常见 AI 服务:</strong>
                <ul className="list-disc list-inside mt-1 space-y-0.5">
                  <li>阿里云通义千问: https://dashscope.aliyuncs.com/compatible-mode/v1</li>
                  <li>OpenAI: https://api.openai.com/v1</li>
                </ul>
              </div>
            </div>
          </section>
        </div>

        {/* Right Column */}
        <div className="space-y-8">
          <section className="space-y-4">
            <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
              <div className="p-1.5 rounded-lg bg-gray-900 text-white">
                <LockKeyhole className="w-4 h-4" />
              </div>
              登录凭据
            </h3>

            <form onSubmit={handleCredentialsSave} className="ios-card rounded-xl p-6 bg-white space-y-5">
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-800">登录用户名</label>
                <div className="relative">
                  <UserRound className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
                  <input
                    type="text"
                    value={credentials.new_username}
                    onChange={event => setCredentials({...credentials, new_username: event.target.value})}
                    className="w-full ios-input pl-11 pr-4 py-3 rounded-xl text-sm"
                    autoComplete="username"
                  />
                </div>
              </div>

              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-800">当前密码</label>
                <div className="relative">
                  <input
                    type={showCurrentPassword ? 'text' : 'password'}
                    value={credentials.current_password}
                    onChange={event => setCredentials({...credentials, current_password: event.target.value})}
                    className="w-full ios-input px-4 py-3 pr-12 rounded-xl text-sm"
                    placeholder="用于确认当前身份"
                    autoComplete="current-password"
                  />
                  <button type="button" onClick={() => setShowCurrentPassword(!showCurrentPassword)} className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600" title={showCurrentPassword ? '隐藏密码' : '显示密码'}>
                    {showCurrentPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-800">新密码</label>
                  <div className="relative">
                    <input
                      type={showNewPassword ? 'text' : 'password'}
                      value={credentials.new_password}
                      onChange={event => setCredentials({...credentials, new_password: event.target.value})}
                      className="w-full ios-input px-4 py-3 pr-11 rounded-xl text-sm"
                      placeholder="不修改则留空"
                      autoComplete="new-password"
                    />
                    <button type="button" onClick={() => setShowNewPassword(!showNewPassword)} className="absolute right-2 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600" title={showNewPassword ? '隐藏密码' : '显示密码'}>
                      {showNewPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
                <div className="space-y-2">
                  <label className="block text-sm font-bold text-gray-800">确认新密码</label>
                  <input
                    type={showNewPassword ? 'text' : 'password'}
                    value={credentials.confirm_password}
                    onChange={event => setCredentials({...credentials, confirm_password: event.target.value})}
                    className="w-full ios-input px-4 py-3 rounded-xl text-sm"
                    placeholder="再次输入新密码"
                    autoComplete="new-password"
                  />
                </div>
              </div>

              {credentialsMessage && (
                <div className={`flex items-start gap-2 rounded-xl px-3 py-2.5 text-sm font-medium ${credentialsMessage.type === 'success' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}`}>
                  <ShieldCheck className="w-4 h-4 mt-0.5 flex-shrink-0" />
                  <span>{credentialsMessage.text}</span>
                </div>
              )}

              <button
                type="submit"
                disabled={credentialsSaving || !credentials.new_username || !credentials.current_password}
                className="w-full bg-gray-900 hover:bg-black text-white px-5 py-3 rounded-xl font-bold text-sm flex items-center justify-center gap-2 transition-colors disabled:opacity-40"
              >
                <LockKeyhole className="w-4 h-4" />
                {credentialsSaving ? '正在更新...' : '更新登录凭据'}
              </button>
            </form>
          </section>

          {/* SMTP 配置已移至「通知设置」页面 */}
        </div>
      </div>

      {/* Save Button */}
      <div className="fixed bottom-10 right-10 z-30">
        <button
            onClick={handleSave}
            disabled={saving}
            className="ios-btn-primary px-10 py-5 rounded-xl text-lg shadow-2xl shadow-blue-200 flex items-center gap-3 transform hover:scale-105 active:scale-95 transition-all disabled:opacity-70"
        >
            <Save className="w-6 h-6" />
            {saving ? '保存中...' : '保存所有配置'}
        </button>
      </div>
    </div>
  );
};

export default Settings;
