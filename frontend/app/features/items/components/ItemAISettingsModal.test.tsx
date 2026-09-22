// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item,ItemAISettings } from '../api';

// modalAPIMocks 保存弹窗加载和保存请求的可控替身。
const modalAPIMocks = vi.hoisted(/* modalAPIMockFactory 创建商品级 AI API 替身。 */ () => ({ get: vi.fn(), update: vi.fn() }));

vi.mock('../api', /* itemAIApiMockFactory 提供弹窗测试使用的商品级 AI API。 */ async importOriginal => {
  // original 保留真实错误消息归一化逻辑和类型导出。
  const original = await importOriginal<typeof import('../api')>();
  return { ...original, getItemAISettings: modalAPIMocks.get, updateItemAISettings: modalAPIMocks.update };
});

import { ITEM_CONTEXT_LIMIT,ItemAISettingsModal } from './ItemAISettingsModal';

// firstItem 是弹窗测试使用的首个商品。
const firstItem: Item = { id: 'item-1', cookie_id: 'account-1', item_id: 'item-1', item_title: '商品一', ai_override: 'inherit' };
// secondItem 是切换商品场景使用的第二个商品。
const secondItem: Item = { id: 'item-2', cookie_id: 'account-1', item_id: 'item-2', item_title: '商品二', ai_override: 'disabled' };

describe('ItemAISettingsModal', /* itemAISettingsModalSuite 覆盖加载、保存失败保留和旧请求取消。 */ () => {
  beforeEach(/* modalMockReset 为每个用例恢复默认成功响应。 */ () => {
    vi.clearAllMocks();
    modalAPIMocks.get.mockResolvedValue({ ai_override: 'inherit', item_context: '材质：全新' });
    modalAPIMocks.update.mockResolvedValue({ ai_override: 'enabled', item_context: '材质：全新' });
  });

  afterEach(/* modalCleanup 清理弹窗 portal 和测试替身。 */ () => cleanup());

  test('加载配置、限制资料长度并保存三态选择', /* modalSaveCase 验证完整编辑与成功保存流程。 */ async () => {
    // onClose 是成功保存后关闭弹窗的断言替身。
    const onClose = vi.fn();
    // onSaved 是保存成功后回写列表状态的断言替身。
    const onSaved = vi.fn();
    render(<ItemAISettingsModal item={firstItem} open onClose={onClose} onSaved={onSaved} />);
    expect(screen.getByText('正在加载配置')).toBeTruthy();
    await waitFor(/* loadedContextAssertion 等待服务端资料写入表单。 */ () => expect(screen.getByDisplayValue('材质：全新')).toBeTruthy());
    // contextInput 是商品专属资料输入框。
    const contextInput = screen.getByLabelText('商品专属资料') as HTMLTextAreaElement;
    expect(contextInput.maxLength).toBe(-1);
    expect(contextInput.placeholder).not.toContain('价格底线');
    fireEvent.click(screen.getByRole('radio', { name: /强制启用/ }));
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
    await waitFor(/* saveRequestAssertion 等待商品级 AI 配置提交完成。 */ () => expect(modalAPIMocks.update).toHaveBeenCalledWith('account-1', 'item-1', { ai_override: 'enabled', item_context: '材质：全新' }, expect.objectContaining({ signal: expect.any(AbortSignal) })));
    expect(onSaved).toHaveBeenCalledWith({ ai_override: 'enabled', item_context: '材质：全新' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('按 Unicode 字符限制商品资料而不是按 UTF-16 code unit 截断', /* unicodeLimitCase 验证表单与 Go rune 计数保持一致。 */ async () => {
    render(<ItemAISettingsModal item={firstItem} open onClose={vi.fn()} onSaved={vi.fn()} />);
    await waitFor(/* loadedContextAssertion 等待服务端资料写入 Unicode 测试表单。 */ () => expect(screen.getByDisplayValue('材质：全新')).toBeTruthy());
    // contextInput 是用于验证表情符号不会被按 UTF-16 单元截断的资料输入框。
    const contextInput = screen.getByLabelText('商品专属资料') as HTMLTextAreaElement;
    fireEvent.change(contextInput, { target: { value: '😀'.repeat(ITEM_CONTEXT_LIMIT + 10) } });
    expect(contextInput.value).toBe('😀'.repeat(ITEM_CONTEXT_LIMIT));
    expect(screen.getByText(`${ITEM_CONTEXT_LIMIT} / ${ITEM_CONTEXT_LIMIT}`)).toBeTruthy();
  });

  test('配置读取失败时禁止保存并允许重新加载', /* loadFailureGuardCase 防止空草稿覆盖服务端已有资料。 */ async () => {
    modalAPIMocks.get.mockRejectedValueOnce(new Error('读取失败'));
    modalAPIMocks.get.mockResolvedValueOnce({ ai_override: 'enabled', item_context: '重试后的资料' });
    render(<ItemAISettingsModal item={firstItem} open onClose={vi.fn()} onSaved={vi.fn()} />);
    await waitFor(/* loadErrorAssertion 等待读取失败状态替代可编辑表单。 */ () => expect(screen.getByRole('alert').textContent).toContain('读取失败'));
    // saveButton 是读取成功前必须保持禁用的保存按钮。
    const saveButton = screen.getByRole('button', { name: '保存配置' }) as HTMLButtonElement;
    expect(saveButton.disabled).toBe(true);
    fireEvent.click(saveButton);
    expect(modalAPIMocks.update).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }));
    await waitFor(/* retrySuccessAssertion 等待重试取得服务端原始资料。 */ () => expect(screen.getByDisplayValue('重试后的资料')).toBeTruthy());
    expect(saveButton.disabled).toBe(false);
  });

  test('保存失败后保留用户草稿并允许重试', /* modalSaveFailureCase 验证失败不会清空表单或关闭弹窗。 */ async () => {
    modalAPIMocks.update.mockRejectedValueOnce(new Error('保存失败'));
    // onClose 是保存失败时不得触发的关闭替身。
    const onClose = vi.fn();
    render(<ItemAISettingsModal item={firstItem} open onClose={onClose} onSaved={vi.fn()} />);
    await waitFor(/* initialLoadAssertion 等待配置读取完成。 */ () => expect(screen.getByDisplayValue('材质：全新')).toBeTruthy());
    fireEvent.change(screen.getByLabelText('商品专属资料'), { target: { value: '发货：付款后 24 小时内寄出' } });
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
    await waitFor(/* saveErrorAssertion 等待保存错误展示。 */ () => expect(screen.getByRole('alert').textContent).toContain('保存失败'));
    expect(screen.getByDisplayValue('发货：付款后 24 小时内寄出')).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  test('切换商品会取消旧请求且忽略旧响应', /* staleLoadCase 验证旧商品响应无法覆盖新商品表单。 */ async () => {
    // resolveFirst 保存首个商品延迟请求的完成入口。
    let resolveFirst: ((value: ItemAISettings) => void) | undefined;
    // firstRequest 是首个商品尚未完成的读取 Promise。
    const firstRequest = new Promise<ItemAISettings>(/* firstRequestExecutor 暴露首个请求的完成函数。 */ resolve => { resolveFirst = resolve; });
    modalAPIMocks.get.mockImplementationOnce(/* delayedGetImplementation 返回可手动完成的首个商品请求。 */ () => firstRequest);
    modalAPIMocks.get.mockResolvedValueOnce({ ai_override: 'disabled', item_context: '第二件商品资料' });
    // onClose 是当前切换商品场景的关闭替身。
    const onClose = vi.fn();
    // view 保存可切换商品属性的 React 渲染实例。
    const view = render(<ItemAISettingsModal item={firstItem} open onClose={onClose} onSaved={vi.fn()} />);
    // firstSignal 是首个读取请求收到的取消信号。
    const firstSignal = modalAPIMocks.get.mock.calls[0][2].signal as AbortSignal;
    view.rerender(<ItemAISettingsModal item={secondItem} open onClose={onClose} onSaved={vi.fn()} />);
    await waitFor(/* secondContextAssertion 等待第二件商品资料写入表单。 */ () => expect(screen.getByDisplayValue('第二件商品资料')).toBeTruthy());
    expect(firstSignal.aborted).toBe(true);
    resolveFirst?.({ ai_override: 'inherit', item_context: '过期资料' });
    await Promise.resolve();
    expect(screen.queryByDisplayValue('过期资料')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
