// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { createDeliveryTemplate, listDeliveryTemplates, updateDeliveryTemplate } from '../api';
import type { DeliveryTemplate } from '../types';
import DeliveryTemplates from './DeliveryTemplates';

vi.mock('../api', /* deliveryTemplatesApiMockFactory 提供发货模板页面的列表请求替身。 */ () => ({
  createDeliveryTemplate: vi.fn(),
  deleteDeliveryTemplate: vi.fn(),
  listDeliveryTemplates: vi.fn(),
  updateDeliveryTemplate: vi.fn(),
}));

// listDeliveryTemplatesMock 是模板列表读取接口的可控替身。
const listDeliveryTemplatesMock = vi.mocked(listDeliveryTemplates);

// emptyTemplateFixture 是空模板列表请求返回的稳定测试数据。
const emptyTemplateFixture: DeliveryTemplate[] = [];

// originalScrollIntoView 保留环境原方法；jsdom 替身只验证调用，不冒充真实滚动布局。
const originalScrollIntoView = HTMLElement.prototype.scrollIntoView;

describe('DeliveryTemplates 页面组合行为', /* 当前回调验证模板编辑器的打开状态和空白新建流程。 */ () => {
  beforeEach(/* 当前回调重置模板接口替身。 */ () => {
    HTMLElement.prototype.scrollIntoView = vi.fn();
    listDeliveryTemplatesMock.mockResolvedValue(emptyTemplateFixture);
    vi.spyOn(window, 'alert').mockImplementation(/* 当前替身记录表单校验提示。 */ () => undefined);
  });

  afterEach(/* 当前回调清理模板页面 DOM 和接口替身。 */ () => {
    cleanup();
    HTMLElement.prototype.scrollIntoView = originalScrollIntoView;
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  test('图文消息可排序且保存显式类型与单一图片来源', /* 当前回调验证文本变量保留、图片来源清除和实际发送顺序。 */ async () => {
    vi.mocked(createDeliveryTemplate).mockResolvedValue(undefined);
    render(<DeliveryTemplates />);
    await waitFor(/* 当前断言等待列表完成。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '图文发货' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息正文'), { target: { value: '您的卡密 {{cards.main}}' } });
    fireEvent.click(screen.getByRole('button', { name: '添加消息' }));
    fireEvent.change(screen.getByLabelText('第 2 条消息类型'), { target: { value: 'image' } });
    fireEvent.change(screen.getByLabelText('图片 URL'), { target: { value: 'https://example.com/old.png' } });
    fireEvent.change(screen.getByLabelText('图片来源'), { target: { value: 'local' } });
    fireEvent.change(screen.getByLabelText('本地图片相对路径'), { target: { value: ' guides/code.png ' } });
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.getByText(/图片不参与变量解析/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '上移第 2 条消息' }));
    expect((screen.getByLabelText('第 1 条消息类型') as HTMLSelectElement).value).toBe('image');
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    await waitFor(/* 当前断言确认消息顺序和双方来源字段明确提交。 */ () => expect(createDeliveryTemplate).toHaveBeenCalledWith({ name: '图文发货', enabled: true, messages: [
      { type: 'image', content: '', image_url: '', image_path: 'guides/code.png' },
      { type: 'text', content: '您的卡密 {{cards.main}}', image_url: '', image_path: '' },
    ] }, expect.objectContaining({ signal: expect.any(AbortSignal) })));
  });

  test('历史文本缺省类型仍可编辑，图片切回文本清除两来源', /* 当前回调验证旧模板兼容及取消不修改列表。 */ async () => {
    listDeliveryTemplatesMock.mockResolvedValue([{ id: 1, name: '历史模板', enabled: true, messages: [{ id: 1, sort_order: 0, content: '{{cards.main}}' }], keys: ['main'], created_at: '', updated_at: '' }]);
    vi.mocked(updateDeliveryTemplate).mockResolvedValue(undefined);
    render(<DeliveryTemplates />);
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }));
    expect((screen.getByLabelText('第 1 条消息类型') as HTMLSelectElement).value).toBe('text');
    fireEvent.change(screen.getByLabelText('第 1 条消息类型'), { target: { value: 'image' } });
    fireEvent.change(screen.getByLabelText('图片 URL'), { target: { value: 'https://example.com/a.png' } });
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    expect((screen.getByLabelText('第 1 条消息正文') as HTMLTextAreaElement).value).toBe('{{cards.main}}');
    fireEvent.change(screen.getByLabelText('第 1 条消息类型'), { target: { value: 'image' } });
    fireEvent.change(screen.getByLabelText('图片 URL'), { target: { value: 'https://example.com/a.png' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息类型'), { target: { value: 'text' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息正文'), { target: { value: '{{custom.note}}' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    await waitFor(/* 当前断言确认切回文本无隐藏图片字段。 */ () => expect(updateDeliveryTemplate).toHaveBeenCalledWith(1, expect.objectContaining({ messages: [{ type: 'text', content: '{{custom.note}}', image_url: '', image_path: '' }] }), expect.anything()));
  });

  test('纯图片模板可保存，空图片和非法路径不得静默过滤', /* 当前回调覆盖纯图片模板与顺序相关错误提示。 */ async () => {
    vi.mocked(createDeliveryTemplate).mockResolvedValue(undefined);
    render(<DeliveryTemplates />);
    await waitFor(/* 当前断言等待首屏列表完成。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '纯图片' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息类型'), { target: { value: 'image' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    expect(window.alert).toHaveBeenCalledWith('第 1 条消息：请填写有效的 HTTP(S) 图片 URL');
    expect(createDeliveryTemplate).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('图片来源'), { target: { value: 'local' } });
    fireEvent.change(screen.getByLabelText('本地图片相对路径'), { target: { value: '../a.png' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    expect(window.alert).toHaveBeenLastCalledWith(expect.stringContaining('相对路径'));
    expect(createDeliveryTemplate).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('本地图片相对路径'), { target: { value: 'goods/a.png' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    await waitFor(/* 当前断言确认无文本变量也能保存图片消息。 */ () => expect(createDeliveryTemplate).toHaveBeenCalledWith({ name: '纯图片', enabled: true, messages: [{ type: 'image', content: '', image_url: '', image_path: 'goods/a.png' }] }, expect.anything()));
  });

  test.each(['成功', '失败'])('取消后的旧保存%s不关闭或解锁下一份图片草稿', /* 当前回调验证 AbortSignal 与 UI 代次隔离，旧 finally 不能影响新保存。 */ async outcome => {
    // finishOld 控制取消后才返回的旧请求，替身故意忽略 AbortSignal。
    let finishOld!: () => void;
    // finishNew 控制新编辑器的有效请求。
    let finishNew!: () => void;
    vi.mocked(createDeliveryTemplate)
      .mockImplementationOnce(/* 当前替身将旧请求留在后台。 */ () => new Promise<void>(/* resolve 和 reject 模拟两种迟到结果。 */ (resolve, reject) => { finishOld = /* 当前完成器在取消后模拟迟到的保存结果。 */ () => outcome === '成功' ? resolve() : reject(new Error('旧请求失败')); }))
      .mockImplementationOnce(/* 当前替身保持新请求处于保存状态。 */ () => new Promise<void>(/* resolve 在断言完成后推进新保存。 */ resolve => { finishNew = resolve; }));
    render(<DeliveryTemplates />);
    await waitFor(/* 当前断言等待首屏列表完成。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '旧草稿' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息正文'), { target: { value: '旧消息' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    // oldSignal 是旧保存已经传到 adapter 的取消信号。
    const oldSignal = vi.mocked(createDeliveryTemplate).mock.calls[0][1]?.signal;
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(oldSignal?.aborted).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '新图片草稿' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息类型'), { target: { value: 'image' } });
    fireEvent.change(screen.getByLabelText('图片 URL'), { target: { value: 'https://example.com/new.png' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    await act(/* 当前回调令旧请求迟到，不能重置新编辑器。 */ async () => finishOld());
    expect((screen.getByRole('button', { name: '保存模板' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: '添加消息' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText('模板名称') as HTMLInputElement).value).toBe('新图片草稿');
    expect(window.alert).not.toHaveBeenCalled();
    expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1);
    await act(/* 当前回调完成唯一有效的新保存。 */ async () => finishNew());
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(2);
  });

  test('固定添加入口可连续追加并聚焦新消息，取消后重开不保留待聚焦草稿', /* 当前回调验证新增节点的焦点归属和关闭清理，不以 jsdom 代替布局验证。 */ async () => {
    render(<DeliveryTemplates />);
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    // addButton 始终使用同一个底部入口，追加十次也不依赖消息列表末尾的元素。
    const addButton = screen.getByRole('button', { name: '添加消息' });
    for (let /* number 是本次追加后应出现的消息序号。 */ number = 2; number <= 11; number += 1) {
      fireEvent.click(addButton);
      expect(document.activeElement).toBe(screen.getByLabelText(`第 ${number} 条消息正文`));
      fireEvent.change(document.activeElement!, { target: { value: `消息 ${number}` } });
    }
    expect(screen.getAllByRole('textbox')).toHaveLength(12);
    expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalledTimes(10);
    expect(HTMLElement.prototype.scrollIntoView).toHaveBeenLastCalledWith({ block: 'nearest' });
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    expect(document.activeElement).toBe(screen.getByLabelText('模板名称'));
    expect(screen.queryByLabelText('第 2 条消息正文')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '添加消息' }));
    expect(document.activeElement).toBe(screen.getByLabelText('第 2 条消息正文'));
    await waitFor(/* 当前断言收口首屏独立列表请求。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
  });

  test('保存失败后恢复固定添加入口，保存中不允许改变消息或表单', /* 当前回调验证添加按钮移出 fieldset 后仍保持禁用边界。 */ async () => {
    // rejectSave 由测试触发写入失败，失败前保持整个表单锁定。
    let rejectSave!: (error: Error) => void;
    vi.mocked(createDeliveryTemplate).mockImplementationOnce(/* 当前替身保留正在保存状态。 */ () => new Promise<void>(/* reject 用于模拟真实写入失败；resolve 不在此场景使用。 */ (_resolve, reject) => { rejectSave = reject; }));
    render(<DeliveryTemplates />);
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '失败重试模板' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息正文'), { target: { value: '已填写消息' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    // addButton 是固定区的入口；保存中显式 disabled，而正文仍由 fieldset 继承禁用。
    const addButton = screen.getByRole('button', { name: '添加消息' }) as HTMLButtonElement;
    expect(addButton.disabled).toBe(true);
    expect(screen.getByLabelText('模板名称').matches(':disabled')).toBe(true);
    fireEvent.click(addButton);
    expect(screen.queryByLabelText('第 2 条消息正文')).toBeNull();
    await act(/* 当前回调释放失败响应，验证草稿保留并解锁。 */ async () => rejectSave(new Error('测试写入失败')));
    expect(window.alert).toHaveBeenCalledWith('保存发货模板失败：测试写入失败');
    expect(addButton.disabled).toBe(false);
    expect(screen.getByLabelText('模板名称').matches(':disabled')).toBe(false);
    fireEvent.click(addButton);
    expect(document.activeElement).toBe(screen.getByLabelText('第 2 条消息正文'));
    expect((screen.getByLabelText('第 1 条消息正文') as HTMLTextAreaElement).value).toBe('已填写消息');
  });

  test('连续排序时键盘焦点跟随同一条消息', /* 当前回调验证移动后 DOM 身份不因消息位置改变而复用到另一行。 */ async () => {
    render(<DeliveryTemplates />);
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    fireEvent.change(screen.getByLabelText('第 1 条消息正文'), { target: { value: 'A' } });
    fireEvent.click(screen.getByRole('button', { name: '添加消息' }));
    fireEvent.change(screen.getByLabelText('第 2 条消息正文'), { target: { value: 'B' } });
    fireEvent.click(screen.getByRole('button', { name: '添加消息' }));
    fireEvent.change(screen.getByLabelText('第 3 条消息正文'), { target: { value: 'C' } });
    // moveButton 对应用户从键盘聚焦的 A 消息下移按钮。
    const moveButton = screen.getByRole('button', { name: '下移第 1 条消息' });
    moveButton.focus();
    fireEvent.click(moveButton);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '下移第 2 条消息' }));
    fireEvent.click(document.activeElement!);
    expect((screen.getByLabelText('第 1 条消息正文') as HTMLTextAreaElement).value).toBe('B');
    expect((screen.getByLabelText('第 2 条消息正文') as HTMLTextAreaElement).value).toBe('C');
    expect((screen.getByLabelText('第 3 条消息正文') as HTMLTextAreaElement).value).toBe('A');
    await waitFor(/* 当前断言收口独立的首屏请求。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));
  });

  test('首屏加载中保存失败不会丢失原模板列表', /* 当前回调让保存先失败、首屏列表后成功，验证两者生命周期独立。 */ async () => {
    // finishList 延迟首屏响应直到新模板保存失败。
    let finishList!: (templates: DeliveryTemplate[]) => void;
    listDeliveryTemplatesMock.mockImplementationOnce(/* 当前替身保留首屏列表等待状态。 */ () => new Promise<DeliveryTemplate[]>(/* resolve 交由测试在保存失败后调用。 */ resolve => { finishList = resolve; }));
    vi.mocked(createDeliveryTemplate).mockRejectedValueOnce(new Error('保存失败'));
    render(<DeliveryTemplates />);
    fireEvent.click(screen.getByRole('button', { name: '新建模板' }));
    fireEvent.change(screen.getByLabelText('模板名称'), { target: { value: '新模板' } });
    fireEvent.change(screen.getByLabelText('第 1 条消息正文'), { target: { value: '消息' } });
    fireEvent.click(screen.getByRole('button', { name: '保存模板' }));
    await waitFor(/* 当前断言等待写入错误完成。 */ () => expect(window.alert).toHaveBeenCalledWith('保存发货模板失败：保存失败'));
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    await act(/* 当前回调返回仍有效的首屏列表，而不是重发请求。 */ async () => finishList([{ id: 8, name: '原有模板', enabled: true, messages: [], keys: [], created_at: '', updated_at: '' }]));
    expect(screen.getByText('原有模板')).toBeTruthy();
    expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1);
  });

  test('点击新建模板会显示空白编辑器', /* 当前回调验证空白新建草稿不会因为名称为空而被条件渲染隐藏。 */ async () => {
    render(<DeliveryTemplates />);
    await waitFor(/* loadAssertion 等待初始模板列表请求完成后再触发用户操作。 */ () => expect(listDeliveryTemplatesMock).toHaveBeenCalledTimes(1));

    // createButton 是页面标题区域触发新建模板的用户操作入口。
    const createButton = screen.getByRole('button', { name: '新建模板' });
    expect(screen.queryByText('新建发货模板')).toBeNull();
    fireEvent.click(createButton);

    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByText('新建发货模板')).toBeTruthy();
    expect(screen.getByPlaceholderText('例如：数字产品发货')).toBeTruthy();
    expect(screen.getByText('内置、卡密、自定义变量均用双大括号')).toBeTruthy();
    expect(screen.getByText('{{buyer_nickname}}')).toBeTruthy();
    expect(screen.getByText('{{custom.<变量名>}}')).toBeTruthy();
    expect(screen.getByText('如何声明和使用')).toBeTruthy();

    // cancelButton 是浮窗底部放弃当前未保存模板草稿的操作入口。
    const cancelButton = screen.getByRole('button', { name: '取消' });
    fireEvent.click(cancelButton);
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
