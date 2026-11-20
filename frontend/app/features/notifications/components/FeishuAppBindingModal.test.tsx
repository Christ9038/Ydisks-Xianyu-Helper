// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import { FeishuAppBindingModal } from './FeishuAppBindingModal';

describe('FeishuAppBindingModal', /* 当前测试组验证扫码弹窗的开关锁定、二维码展示和重试动作。 */ () => {
  afterEach(/* 当前回调清理 Portal 渲染结果，避免污染后续用例。 */ () => {
    cleanup();
  });

  test('开始前三个设置开关可以修改并生成二维码', /* 当前回调验证初始阶段的账号、警告和普通提示开关。 */ () => {
    // onBindAllAccountsChange、onNotifyWarningChange 和 onNotifyInfoChange 是三个设置开关的动作替身。
    const onBindAllAccountsChange = vi.fn();
    // onNotifyWarningChange 记录警告类通知开关动作。
    const onNotifyWarningChange = vi.fn();
    // onNotifyInfoChange 记录普通提示类通知开关动作。
    const onNotifyInfoChange = vi.fn();
    // onStart 记录生成二维码动作。
    const onStart = vi.fn();
    render(<FeishuAppBindingModal status="starting" qrCodeUrl="" message="" bindAllAccounts notifyWarning notifyInfo requesting={false} onBindAllAccountsChange={onBindAllAccountsChange} onNotifyWarningChange={onNotifyWarningChange} onNotifyInfoChange={onNotifyInfoChange} onStart={onStart} onClose={vi.fn()} />);

    expect(screen.getByText('扫码绑定步骤')).not.toBeNull();
    // bindAllSwitch 是初始阶段可修改的“绑定全部账号”开关。
    const bindAllSwitch = screen.getByRole('switch', { name: '绑定全部账号' });
    expect(bindAllSwitch.getAttribute('aria-checked')).toBe('true');
    expect((bindAllSwitch as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(bindAllSwitch);
    expect(onBindAllAccountsChange).toHaveBeenCalledWith(false);
    fireEvent.click(screen.getByRole('switch', { name: '警告类通知' }));
    expect(onNotifyWarningChange).toHaveBeenCalledWith(false);
    fireEvent.click(screen.getByRole('switch', { name: '普通提示类通知' }));
    expect(onNotifyInfoChange).toHaveBeenCalledWith(false);
    fireEvent.click(screen.getByRole('button', { name: /生成二维码/ }));
    expect(onStart).toHaveBeenCalledTimes(1);
  });

  test('请求中和等待扫码时锁定设置并展示二维码', /* 当前回调验证开始后不能改变会话参数且二维码可见。 */ () => {
    // onClose 是关闭动作替身，用于确认弹窗保留关闭入口。
    const onClose = vi.fn();
    // renderResult 提供 rerender 以切换请求中和等待状态。
    const { rerender } = render(<FeishuAppBindingModal status="starting" qrCodeUrl="" message="" bindAllAccounts={false} notifyWarning notifyInfo requesting onBindAllAccountsChange={vi.fn()} onNotifyWarningChange={vi.fn()} onNotifyInfoChange={vi.fn()} onStart={vi.fn()} onClose={onClose} />);
    // bindAllSwitch 是请求进行中的账号绑定开关。
    const bindAllSwitch = screen.getByRole('switch', { name: '绑定全部账号' });
    expect((bindAllSwitch as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('switch', { name: '警告类通知' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('switch', { name: '普通提示类通知' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: /正在生成二维码/ }) as HTMLButtonElement).disabled).toBe(true);

    rerender(<FeishuAppBindingModal status="waiting" qrCodeUrl="data:image/png;base64,qr" message="等待飞书扫码确认" bindAllAccounts={false} notifyWarning={false} notifyInfo requesting={false} onBindAllAccountsChange={vi.fn()} onNotifyWarningChange={vi.fn()} onNotifyInfoChange={vi.fn()} onStart={vi.fn()} onClose={onClose} />);
    expect((screen.getByRole('switch', { name: '绑定全部账号' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByRole('img', { name: '飞书扫码绑定二维码' }).getAttribute('src')).toBe('data:image/png;base64,qr');
    expect(screen.getByRole('status').textContent).toBe('等待飞书扫码确认');
    fireEvent.click(screen.getByRole('button', { name: '关闭飞书绑定弹窗' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('错误和过期阶段允许重新生成二维码', /* 当前回调验证可重试终态不会让用户卡在失败弹窗。 */ () => {
    // onStart 是重试动作替身。
    const onStart = vi.fn();
    // rerender 是切换弹窗终态的渲染函数。
    const { rerender } = render(<FeishuAppBindingModal status="error" qrCodeUrl="" message="授权被拒绝" bindAllAccounts notifyWarning notifyInfo requesting={false} onBindAllAccountsChange={vi.fn()} onNotifyWarningChange={vi.fn()} onNotifyInfoChange={vi.fn()} onStart={onStart} onClose={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toContain('授权被拒绝');
    fireEvent.click(screen.getByRole('button', { name: /重新生成二维码/ }));
    expect(onStart).toHaveBeenCalledTimes(1);

    rerender(<FeishuAppBindingModal status="expired" qrCodeUrl="" message="二维码已过期" bindAllAccounts notifyWarning notifyInfo requesting={false} onBindAllAccountsChange={vi.fn()} onNotifyWarningChange={vi.fn()} onNotifyInfoChange={vi.fn()} onStart={onStart} onClose={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toContain('二维码已过期');
  });
});
