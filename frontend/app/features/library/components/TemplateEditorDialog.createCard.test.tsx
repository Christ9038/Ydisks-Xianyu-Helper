// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import type { CardCreateDialogProps } from '../../../../shared/ui/cardCreate';
import type { AutomationOptions } from '../hooks';
import { TemplateEditorDialog } from './TemplateEditorDialog';

// options 含一个已存在的卡密组，用于验证回填后的下拉选中。
const options: AutomationOptions = { cards: [{ id: 7, name: '新建的卡', type: 'data' }], deliveryTemplates: [], accounts: [], error: '' };

/** FakeCardCreateDialog 模拟注入的新建卡密弹窗：点“创建”回传卡密 7，点“放弃”关闭。 */
const FakeCardCreateDialog = ({ onClose, onCreated }: CardCreateDialogProps) => (
  <div role="dialog" aria-label="新建卡密替身">
    <button type="button" onClick={/* 回传新卡密。 */ () => onCreated({ id: 7, name: '新建的卡' })}>创建</button>
    <button type="button" onClick={onClose}>放弃</button>
  </div>
);

// openCardAction 勾选付款发货并添加一个发卡动作。
const openCardAction = (): void => {
  fireEvent.click(screen.getByLabelText('付款后自动发货'));
  fireEvent.click(screen.getAllByText('发送卡密').find(/* node 选择“添加动作”按钮而非动作标签。 */ node => node.tagName === 'BUTTON') as HTMLElement);
};

describe('TemplateEditorDialog 就地新建卡密', /* 打开、回填、放弃与未注入。 */ () => {
  afterEach(/* 清理渲染树。 */ () => cleanup());

  test('新建成功后回填发卡动作、刷新选项并关闭弹窗，草稿保留', /* 成功路径。 */ () => {
    // onReloadCards 是刷新卡密选项替身。
    const onReloadCards = vi.fn().mockResolvedValue(undefined);
    render(<TemplateEditorDialog template={null} busy={false} options={options} cardCreateDialog={FakeCardCreateDialog} onReloadCards={onReloadCards} onSave={vi.fn()} onClose={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText('仅用于识别模板'), { target: { value: '我的模板' } });
    openCardAction();
    fireEvent.click(screen.getByRole('button', { name: '新建卡密组' }));
    fireEvent.click(screen.getByText('创建'));
    expect((screen.getByLabelText('卡密组') as HTMLSelectElement).value).toBe('7');
    expect(onReloadCards).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog', { name: '新建卡密替身' })).toBeNull();
    expect((screen.getByPlaceholderText('仅用于识别模板') as HTMLInputElement).value).toBe('我的模板');
  });

  test('放弃新建时不回填也不刷新', /* 取消路径。 */ () => {
    // onReloadCards 是刷新卡密选项替身。
    const onReloadCards = vi.fn();
    render(<TemplateEditorDialog template={null} busy={false} options={options} cardCreateDialog={FakeCardCreateDialog} onReloadCards={onReloadCards} onSave={vi.fn()} onClose={vi.fn()} />);
    openCardAction();
    fireEvent.click(screen.getByRole('button', { name: '新建卡密组' }));
    fireEvent.click(screen.getByText('放弃'));
    expect((screen.getByLabelText('卡密组') as HTMLSelectElement).value).toBe('0');
    expect(onReloadCards).not.toHaveBeenCalled();
  });

  test('未注入弹窗时不显示新建入口', /* 向后兼容。 */ () => {
    render(<TemplateEditorDialog template={null} busy={false} options={options} onSave={vi.fn()} onClose={vi.fn()} />);
    openCardAction();
    expect(screen.queryByRole('button', { name: '新建卡密组' })).toBeNull();
  });
});
