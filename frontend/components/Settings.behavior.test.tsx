// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import Settings from './Settings';
import { useSettings } from '../app/features/settings/hooks';

vi.mock('../app/features/settings/hooks', /* settingsHookMockFactory 提供设置页面的可控状态。 */ () => ({ useSettings: vi.fn() }));

// useSettingsMock 是设置页面 Hook 的可控替身。
const useSettingsMock = vi.mocked(useSettings);

describe('Settings 页面加载边界', /* 当前回调覆盖设置页面无配置和加载失败分支。 */ () => {
  test('配置加载失败时展示错误并触发重新加载', /* 当前回调验证设置页面错误提示和重试动作。 */ () => {
    // loadSettings 是系统设置重新加载动作替身。
    const loadSettings = vi.fn();
    useSettingsMock.mockReturnValue({ settings: null, loading: false, loadError: '配置读取失败', saving: false, saveError: '', aiModels: [], modelsLoading: false, modelError: '', modelDropdownOpen: false, showApiKey: false, showCaptchaSecret: false, showCurrentPassword: false, showNewPassword: false, credentialsSaving: false, credentialsMessage: null, credentials: {}, modelPickerRef: { current: null }, loadSettings, loadAIModels: vi.fn(), handleSave: vi.fn(), handleCredentialsSave: vi.fn(), setSettings: vi.fn(), setModelDropdownOpen: vi.fn(), setShowApiKey: vi.fn(), setShowCaptchaSecret: vi.fn(), setShowCurrentPassword: vi.fn(), setShowNewPassword: vi.fn(), setCredentials: vi.fn(), setCredentialsMessage: vi.fn() } as never);
    render(<Settings />);
    expect(screen.getByText('配置读取失败')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }));
    expect(loadSettings).toHaveBeenCalled();
  });

  test('加载中没有配置时展示加载文案', /* 当前回调验证设置页面初始加载占位。 */ () => {
    useSettingsMock.mockReturnValue({ settings: null, loading: true, loadError: '', saving: false, saveError: '', aiModels: [], modelsLoading: false, modelError: '', modelDropdownOpen: false, showApiKey: false, showCaptchaSecret: false, showCurrentPassword: false, showNewPassword: false, credentialsSaving: false, credentialsMessage: null, credentials: {}, modelPickerRef: { current: null }, loadSettings: vi.fn(), loadAIModels: vi.fn(), handleSave: vi.fn(), handleCredentialsSave: vi.fn(), setSettings: vi.fn(), setModelDropdownOpen: vi.fn(), setShowApiKey: vi.fn(), setShowCaptchaSecret: vi.fn(), setShowCurrentPassword: vi.fn(), setShowNewPassword: vi.fn(), setCredentials: vi.fn(), setCredentialsMessage: vi.fn() } as never);
    render(<Settings />);
    expect(screen.getByText('加载配置中...')).toBeTruthy();
  });
});
