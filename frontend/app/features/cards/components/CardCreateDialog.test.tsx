// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

// dialogMocks 保存创建接口替身。
const dialogMocks = vi.hoisted(/* 创建替身工厂。 */ () => ({ createCard: vi.fn(), testCardAPI: vi.fn() }));

vi.mock('../api', /* 提供卡密 API 替身。 */ () => ({ createCard: dialogMocks.createCard, testCardAPI: dialogMocks.testCardAPI }));

import { CardCreateDialog } from './CardCreateDialog';

// fillText 填写名称并切换到文本类型填写内容。
const fillText = (): void => {
  fireEvent.change(screen.getByPlaceholderText('例如：VIP会员卡密'), { target: { value: '新文案' } });
  fireEvent.click(screen.getByRole('button', { name: '文本' }));
  fireEvent.change(screen.getByPlaceholderText('请输入每次发货时发送的固定文字'), { target: { value: '欢迎' } });
};

describe('CardCreateDialog 新建卡密弹窗', /* 覆盖成功、校验失败、请求失败、重复提交与卸载后迟到响应。 */ () => {
  beforeEach(/* 重置替身。 */ () => { vi.clearAllMocks(); dialogMocks.createCard.mockResolvedValue({ success: true, id: 9 }); });
  afterEach(/* 清理 DOM。 */ () => cleanup());

  test('创建成功后回传新卡密', /* 成功路径。 */ async () => {
    // onCreated 是成功回调替身。
    const onCreated = vi.fn();
    render(<CardCreateDialog onClose={vi.fn()} onCreated={onCreated} />);
    fillText();
    fireEvent.click(screen.getByText('添加卡密'));
    await waitFor(/* 等待回调。 */ () => expect(onCreated).toHaveBeenCalledWith({ id: 9, name: '新文案' }));
    expect(dialogMocks.createCard).toHaveBeenCalledWith(expect.objectContaining({ type: 'text', text_content: '欢迎' }));
  });

  test('校验失败时不发请求并在弹窗内提示', /* 校验分支。 */ () => {
    render(<CardCreateDialog onClose={vi.fn()} onCreated={vi.fn()} />);
    fireEvent.click(screen.getByText('添加卡密'));
    expect(screen.getByRole('alert').textContent).toBe('请输入卡密名称');
    expect(dialogMocks.createCard).not.toHaveBeenCalled();
  });

  test('请求失败时展示原因且可重试', /* 失败后恢复可提交。 */ async () => {
    dialogMocks.createCard.mockRejectedValueOnce(new Error('名称已存在'));
    // onCreated 是成功回调替身。
    const onCreated = vi.fn();
    render(<CardCreateDialog onClose={vi.fn()} onCreated={onCreated} />);
    fillText();
    fireEvent.click(screen.getByText('添加卡密'));
    await screen.findByText('名称已存在');
    expect(onCreated).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('添加卡密'));
    await waitFor(/* 重试成功。 */ () => expect(onCreated).toHaveBeenCalledTimes(1));
  });

  test('非 Error 异常使用兜底文案', /* 兜底提示。 */ async () => {
    dialogMocks.createCard.mockRejectedValueOnce('boom');
    render(<CardCreateDialog onClose={vi.fn()} onCreated={vi.fn()} />);
    fillText();
    fireEvent.click(screen.getByText('添加卡密'));
    await screen.findByText('添加失败，请重试');
  });

  test('提交期间禁用按钮避免重复创建', /* 在途保护。 */ async () => {
    // release 用于手动结束挂起的创建请求。
    let release: (value: { /** success 表示成功。 */ success: boolean; /** id 是新主键。 */ id: number }) => void = () => undefined;
    dialogMocks.createCard.mockReturnValueOnce(new Promise(/* resolver 捕获 resolve。 */ resolve => { release = resolve; }));
    render(<CardCreateDialog onClose={vi.fn()} onCreated={vi.fn()} />);
    fillText();
    fireEvent.click(screen.getByText('添加卡密'));
    // pending 是在途时的提交按钮。
    const pending = screen.getByText('添加中...').closest('button') as HTMLButtonElement;
    expect(pending.disabled).toBe(true);
    fireEvent.click(pending);
    expect(dialogMocks.createCard).toHaveBeenCalledTimes(1);
    await act(/* 结束请求。 */ async () => release({ success: true, id: 1 }));
  });

  test('卸载后迟到的成功响应不再回调', /* 关闭后丢弃响应。 */ async () => {
    // release 用于手动结束挂起的创建请求。
    let release: (value: { /** success 表示成功。 */ success: boolean; /** id 是新主键。 */ id: number }) => void = () => undefined;
    dialogMocks.createCard.mockReturnValueOnce(new Promise(/* resolver 捕获 resolve。 */ resolve => { release = resolve; }));
    // onCreated 是成功回调替身。
    const onCreated = vi.fn();
    // view 用于卸载弹窗。
    const view = render(<CardCreateDialog onClose={vi.fn()} onCreated={onCreated} />);
    fillText();
    fireEvent.click(screen.getByText('添加卡密'));
    view.unmount();
    await act(/* 卸载后才返回。 */ async () => release({ success: true, id: 2 }));
    expect(onCreated).not.toHaveBeenCalled();
  });

  test('切换类型会清空已填写内容，关闭按钮触发 onClose', /* 类型切换与关闭。 */ () => {
    // onClose 是关闭回调替身。
    const onClose = vi.fn();
    render(<CardCreateDialog onClose={onClose} onCreated={vi.fn()} />);
    fillText();
    fireEvent.click(screen.getByRole('button', { name: '批量库存' }));
    expect((screen.getByPlaceholderText(/CODE-123456/) as HTMLTextAreaElement).value).toBe('');
    fireEvent.click(screen.getByText('取消'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
