// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { AccountDetail,AIReplySettings } from '../api';
import { AccountAISettingsModal } from './AccountAISettingsModal';
import { AccountCard } from './AccountCard';
import { AccountDeleteDialog } from './AccountDeleteDialog';
import { AccountQRCodeModal } from './AccountQRCodeModal';

// accountFixture 是账号组件测试使用的最小非敏感账号摘要。
const accountFixture = {
  id: 'account-1',
  nickname: '测试账号',
  remark: '测试备注',
  enabled: true,
  runtime_state: 'online',
  runtime_message: '',
  ai_enabled: true,
  auto_rate_enabled: false,
  auto_polish_enabled: false,
  auto_confirm: false,
  paused: false,
} as AccountDetail;

// aiSettingsFixture 是 AI 设置弹窗测试使用的编辑草稿。
const aiSettingsFixture: AIReplySettings = {
  ai_enabled: false,
  auto_adjust_price_enabled: false,
  max_discount_percent: 10,
  max_discount_amount: 100,
  max_bargain_rounds: 3,
  custom_prompts: '',
};

// noopAccountAction 是账号卡片测试使用的动作占位函数。
const noopAccountAction = (): void => undefined;

describe('账号 feature 展示组件', /* 当前回调覆盖账号页面子模块展示边界。 */ () => {
  afterEach(/* 当前回调清理 Portal 和测试 DOM。 */ () => cleanup());

  test('账号卡片展示状态并转发所有操作', /* 当前回调验证账号卡片的操作边界。 */ () => {
    // onDelete 是删除操作测试替身。
    const onDelete = vi.fn();
    // onAI 是 AI 设置操作测试替身。
    const onAI = vi.fn();
    // onToggle 是启停操作测试替身。
    const onToggle = vi.fn();
    render(<AccountCard account={accountFixture} refreshing={false} deleting={false} onRefreshProfile={noopAccountAction} onReauthorize={noopAccountAction} onEdit={noopAccountAction} onAI={onAI} onTasks={noopAccountAction} onToggle={onToggle} onDelete={onDelete} />);
    expect(screen.getByText('测试账号')).toBeTruthy();
    fireEvent.click(screen.getByTitle('AI设置'));
    fireEvent.click(screen.getByTitle('停用账号'));
    fireEvent.click(screen.getByTitle('删除账号 测试账号'));
    expect(onAI).toHaveBeenCalledWith(accountFixture);
    expect(onToggle).toHaveBeenCalledWith(accountFixture.id, accountFixture.enabled);
    expect(onDelete).toHaveBeenCalledWith(accountFixture);
  });

  test('AI 设置弹窗使用补丁更新并转发保存', /* 当前回调验证 AI 设置字段更新和保存动作。 */ () => {
    // onChange 是 AI 设置草稿更新测试替身。
    const onChange = vi.fn();
    // onSave 是 AI 设置保存测试替身。
    const onSave = vi.fn();
    // view 允许测试在 AI 开启后重新渲染同一受控弹窗。
    const view = render(<AccountAISettingsModal account={accountFixture} settings={aiSettingsFixture} saving={false} onChange={onChange} onClose={noopAccountAction} onSave={onSave} />);
    fireEvent.click(screen.getByLabelText('切换 AI 自动回复'));
    fireEvent.change(screen.getByDisplayValue('10'), { target: { value: '20' } });
    fireEvent.click(screen.getByText('保存'));
    expect(onChange).toHaveBeenNthCalledWith(1, { ...aiSettingsFixture, ai_enabled: true });
    expect(onChange).toHaveBeenNthCalledWith(2, { ...aiSettingsFixture, max_discount_percent: 20 });
    expect(onSave).toHaveBeenCalledTimes(1);
    // enabledSettings 是 AI 议价已开启、允许商家进一步选择真实自动改价的草稿。
    const enabledSettings = { ...aiSettingsFixture, ai_enabled: true };
    view.rerender(<AccountAISettingsModal account={accountFixture} settings={enabledSettings} saving={false} onChange={onChange} onClose={noopAccountAction} onSave={onSave} />);
    fireEvent.click(screen.getByLabelText('切换 AI 自动改价'));
    expect(onChange).toHaveBeenNthCalledWith(3, { ...enabledSettings, auto_adjust_price_enabled: true });
  });

  test('删除确认框展示错误并转发确认动作', /* 当前回调验证删除确认框的错误和提交分支。 */ () => {
    // onConfirm 是删除确认测试替身。
    const onConfirm = vi.fn();
    render(<AccountDeleteDialog account={accountFixture} deleting={false} error="删除失败" onClose={noopAccountAction} onConfirm={onConfirm} />);
    expect(screen.getByRole('alert').textContent).toContain('删除失败');
    fireEvent.click(screen.getByText('确认删除'));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  test('二维码弹窗展示风控验证面板且不生成外部链接', /* 当前回调验证二维码风控状态的安全展示边界。 */ () => {
    render(<AccountQRCodeModal target={accountFixture} status="verification" codeUrl="" errorMessage="" faceQrUrl="face-qr" verificationScreenshot="screen" onClose={noopAccountAction} />);
    expect(screen.getByText('需要完成安全风控验证')).toBeTruthy();
    expect(document.querySelectorAll('a')).toHaveLength(0);
  });
});
