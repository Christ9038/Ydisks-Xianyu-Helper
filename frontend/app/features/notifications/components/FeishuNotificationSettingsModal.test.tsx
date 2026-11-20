// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import { FeishuNotificationSettingsModal } from './FeishuNotificationSettingsModal';

describe('FeishuNotificationSettingsModal', /* 当前测试组验证分类设置弹窗的加载、错误、开关和保存展示。 */ () => {
  afterEach(/* 当前回调清理 Portal 渲染结果，避免污染后续用例。 */ () => {
    cleanup();
  });

  test('读取中展示加载状态并隐藏保存按钮', /* 当前回调验证加载期间不会提交不可信草稿。 */ () => {
    render(<FeishuNotificationSettingsModal open channelName="飞书扫码渠道" loading saving={false} warning info error="" loadFailed={false} onWarningChange={vi.fn()} onInfoChange={vi.fn()} onSave={vi.fn()} onRetry={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByText('正在读取通知设置...')).not.toBeNull();
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.queryByRole('button', { name: /保存/ })).toBeNull();
  });

  test('读取成功展示两个开关并回传修改和保存动作', /* 当前回调验证警告类与普通提示类开关的展示和回调。 */ () => {
    // onWarningChange、onInfoChange 和 onSave 是开关与保存动作替身。
    const onWarningChange = vi.fn();
    // onInfoChange 记录普通提示类通知开关动作。
    const onInfoChange = vi.fn();
    // onSave 记录保存动作。
    const onSave = vi.fn();
    render(<FeishuNotificationSettingsModal open channelName="飞书扫码渠道" loading={false} saving={false} warning info={false} error="" loadFailed={false} onWarningChange={onWarningChange} onInfoChange={onInfoChange} onSave={onSave} onRetry={vi.fn()} onClose={vi.fn()} />);
    expect((screen.getByRole('switch', { name: '警告类通知' }) as HTMLButtonElement).getAttribute('aria-checked')).toBe('true');
    expect((screen.getByRole('switch', { name: '普通提示类通知' }) as HTMLButtonElement).getAttribute('aria-checked')).toBe('false');
    fireEvent.click(screen.getByRole('switch', { name: '警告类通知' }));
    expect(onWarningChange).toHaveBeenCalledWith(false);
    fireEvent.click(screen.getByRole('switch', { name: '普通提示类通知' }));
    expect(onInfoChange).toHaveBeenCalledWith(true);
    fireEvent.click(screen.getByRole('button', { name: /保存/ }));
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  test('读取失败展示错误与重试并隐藏开关和保存', /* 当前回调验证默认草稿不会在读取失败后继续展示。 */ () => {
    // onRetry 是重新读取动作替身。
    const onRetry = vi.fn();
    render(<FeishuNotificationSettingsModal open channelName="飞书扫码渠道" loading={false} saving={false} warning info error="读取设置失败" loadFailed onWarningChange={vi.fn()} onInfoChange={vi.fn()} onSave={vi.fn()} onRetry={onRetry} onClose={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toContain('读取设置失败');
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.queryByRole('button', { name: /保存/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /重试/ }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  test('保存失败展示错误但保留开关和保存按钮', /* 当前回调验证保存失败后用户可以修改或重试。 */ () => {
    render(<FeishuNotificationSettingsModal open channelName="飞书扫码渠道" loading={false} saving={false} warning info error="保存设置失败" loadFailed={false} onWarningChange={vi.fn()} onInfoChange={vi.fn()} onSave={vi.fn()} onRetry={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toContain('保存设置失败');
    expect(screen.getByRole('switch', { name: '警告类通知' })).not.toBeNull();
    expect(screen.getByRole('button', { name: /保存/ })).not.toBeNull();
  });
});
