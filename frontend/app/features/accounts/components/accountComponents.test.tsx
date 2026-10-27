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
  ai_mode: 'bargain_only',
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

  test('风控账号提示粘贴无风控 Cookie 而不是重新扫码', /* 当前回调验证风控与登录失效使用不同的恢复入口。 */ () => {
    // onEdit 是打开编辑弹窗的测试替身。
    const onEdit = vi.fn();
    // onReauthorize 是二维码授权的测试替身。
    const onReauthorize = vi.fn();
    // riskAccount 是处于风控验证状态的账号。
    const riskAccount = { ...accountFixture, runtime_state: 'verification_required' as const, runtime_message: '闲鱼要求安全验证' };
    // view 用于后续切换为登录失效账号。
    const view = render(<AccountCard account={riskAccount} refreshing={false} deleting={false} onRefreshProfile={noopAccountAction} onReauthorize={onReauthorize} onEdit={onEdit} onAI={noopAccountAction} onTasks={noopAccountAction} onToggle={noopAccountAction} onDelete={noopAccountAction} />);
    expect(screen.queryByText('重新授权')).toBeNull();
    expect(screen.getByText('查看操作教程').closest('a')?.getAttribute('href')).toContain('/issues/92');
    fireEvent.click(screen.getByText('粘贴无风控 Cookie'));
    expect(onEdit).toHaveBeenCalledWith(riskAccount);
    expect(onReauthorize).not.toHaveBeenCalled();
    // expiredAccount 是登录凭证失效的账号，仍应走二维码授权。
    const expiredAccount = { ...accountFixture, runtime_state: 'auth_expired' as const, runtime_message: '登录凭证已失效' };
    view.rerender(<AccountCard account={expiredAccount} refreshing={false} deleting={false} onRefreshProfile={noopAccountAction} onReauthorize={onReauthorize} onEdit={onEdit} onAI={noopAccountAction} onTasks={noopAccountAction} onToggle={noopAccountAction} onDelete={noopAccountAction} />);
    expect(screen.queryByText('查看操作教程')).toBeNull();
    fireEvent.click(screen.getByText('重新授权'));
    expect(onReauthorize).toHaveBeenCalledWith(expiredAccount);
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

  test('AI 工作模式切换更新草稿，并按模式显示说明与提示词标签', /* 当前回调验证仅砍价与全场景客服两种模式的文案与草稿补丁。 */ () => {
    // onChange 是 AI 设置草稿更新测试替身。
    const onChange = vi.fn();
    // view 允许在受控草稿变化后重新渲染同一弹窗。
    const view = render(<AccountAISettingsModal account={accountFixture} settings={aiSettingsFixture} saving={false} onChange={onChange} onClose={noopAccountAction} onSave={noopAccountAction} />);
    expect(screen.getByText('仅在识别到砍价意图时调用 AI，其他消息继续使用默认回复。')).toBeTruthy();
    expect(screen.getByText('自定义提示词（可选）')).toBeTruthy();
    fireEvent.click(screen.getByText('全场景客服'));
    expect(onChange).toHaveBeenLastCalledWith({ ...aiSettingsFixture, ai_mode: 'full_service' });
    // fullServiceSettings 是切换到全场景客服后的受控草稿。
    const fullServiceSettings = { ...aiSettingsFixture, ai_mode: 'full_service' as const };
    view.rerender(<AccountAISettingsModal account={accountFixture} settings={fullServiceSettings} saving={false} onChange={onChange} onClose={noopAccountAction} onSave={noopAccountAction} />);
    expect(screen.getByText('未命中关键词时，AI 会处理商品咨询、发货、售后和砍价消息。')).toBeTruthy();
    expect(screen.getByText('店铺客服规则（可选）')).toBeTruthy();
    fireEvent.click(screen.getByText('仅砍价'));
    expect(onChange).toHaveBeenLastCalledWith({ ...fullServiceSettings, ai_mode: 'bargain_only' });
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
