// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import { FeishuAppBindingView } from './FeishuAppBindingView';

describe('FeishuAppBindingView', /* 当前测试组验证内嵌扫码视图的二维码展示、按钮位置和重试动作。 */ () => {
  afterEach(/* 当前回调清理渲染结果，避免污染后续用例。 */ () => {
    cleanup();
  });

  test('开始前只有步骤和“生成二维码”按钮，没有绑定全部账号开关', /* 当前回调验证初始阶段的内容与动作。 */ () => {
    // onStart 记录生成二维码动作。
    const onStart = vi.fn();
    render(<FeishuAppBindingView status="starting" qrCodeUrl="" message="" requesting={false} onStart={onStart} onClose={vi.fn()} />);

    expect(screen.getByText('扫码授权步骤')).not.toBeNull();
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.queryByText('绑定全部账号')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /生成二维码/ }));
    expect(onStart).toHaveBeenCalledTimes(1);
  });

  test('操作按钮位于二维码卡片正下方并与卡片同属一列、等宽', /* 当前回调验证“卡片 + 按钮”是同一列，步骤卡片是另一列。 */ () => {
    render(<FeishuAppBindingView status="starting" qrCodeUrl="" message="" requesting={false} onStart={vi.fn()} onClose={vi.fn()} />);
    // button 是生成二维码按钮，应带有等宽样式 w-full。
    const button = screen.getByRole('button', { name: /生成二维码/ });
    expect(button.className).toContain('w-full');
    // leftColumn 是二维码卡片与按钮共同所在的左列。
    const leftColumn = button.parentElement as HTMLElement;
    expect(leftColumn.querySelector('section')).not.toBeNull();
    expect(leftColumn.className).toContain('flex-col');
    // stepsCard 是右列步骤卡片，不应与按钮在同一列。
    const stepsCard = screen.getByText('扫码授权步骤').closest('section') as HTMLElement;
    expect(leftColumn.contains(stepsCard)).toBe(false);
    // grid 是两列共同的网格行，拉伸对齐保证两列总高度一致。
    const grid = leftColumn.parentElement as HTMLElement;
    expect(grid.className).toContain('items-stretch');
    expect(grid.contains(stepsCard)).toBe(true);
  });

  test('请求中禁用按钮，等待扫码时展示二维码并保留关闭', /* 当前回调验证开始后的按钮状态和二维码展示。 */ () => {
    // onClose 是关闭动作替身，用于确认等待扫码阶段保留关闭入口。
    const onClose = vi.fn();
    // rerender 用于从请求中切换到等待扫码状态。
    const { rerender } = render(<FeishuAppBindingView status="starting" qrCodeUrl="" message="" requesting onStart={vi.fn()} onClose={onClose} />);
    expect((screen.getByRole('button', { name: /正在生成二维码/ }) as HTMLButtonElement).disabled).toBe(true);

    rerender(<FeishuAppBindingView status="waiting" qrCodeUrl="data:image/png;base64,qr" message="等待飞书扫码确认" requesting={false} onStart={vi.fn()} onClose={onClose} />);
    expect(screen.getByRole('img', { name: '飞书扫码绑定二维码' }).getAttribute('src')).toBe('data:image/png;base64,qr');
    expect(screen.getByRole('status').textContent).toBe('等待飞书扫码确认');
    // closeButton 是二维码下方的等宽关闭按钮。
    const closeButton = screen.getByRole('button', { name: '关闭' });
    expect(closeButton.className).toContain('w-full');
    fireEvent.click(closeButton);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('成功后按钮变为等宽“完成”', /* 当前回调验证终态按钮。 */ () => {
    // onClose 记录完成动作。
    const onClose = vi.fn();
    render(<FeishuAppBindingView status="success" qrCodeUrl="" message="飞书应用绑定成功" requesting={false} onStart={vi.fn()} onClose={onClose} />);
    // done 是成功后的完成按钮。
    const done = screen.getByRole('button', { name: '完成' });
    expect(done.className).toContain('w-full');
    fireEvent.click(done);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('错误和过期阶段允许重新生成二维码', /* 当前回调验证可重试终态不会让用户卡在失败视图。 */ () => {
    // onStart 是重试动作替身。
    const onStart = vi.fn();
    // rerender 是切换终态的渲染函数。
    const { rerender } = render(<FeishuAppBindingView status="error" qrCodeUrl="" message="授权被拒绝" requesting={false} onStart={onStart} onClose={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toContain('授权被拒绝');
    fireEvent.click(screen.getByRole('button', { name: /重新生成二维码/ }));
    expect(onStart).toHaveBeenCalledTimes(1);

    rerender(<FeishuAppBindingView status="expired" qrCodeUrl="" message="二维码已过期" requesting={false} onStart={onStart} onClose={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toContain('二维码已过期');
  });
});
