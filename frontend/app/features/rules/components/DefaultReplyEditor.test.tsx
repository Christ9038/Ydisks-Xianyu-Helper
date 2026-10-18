// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { getDefaultReply,getItemDefaultReplies,getItemDefaultReply,updateDefaultReply,updateItemDefaultReply } from '../api';
import { useDefaultReplyActions } from '../defaultReplyActions';
import type { AccountDetail,DefaultReply,Item } from '../models';
import DefaultRepliesPanel from './DefaultRepliesPanel';
import DefaultReplyEditor from './DefaultReplyEditor';

vi.mock('../api', /* 当前工厂隔离编辑器和列表的所有 HTTP 副作用。 */ () => ({
  clearDefaultReplyRecords: vi.fn(), deleteDefaultReply: vi.fn(), deleteItemDefaultReply: vi.fn(),
  getDefaultReply: vi.fn(), getItemDefaultReplies: vi.fn(), getItemDefaultReply: vi.fn(), updateDefaultReply: vi.fn(), updateItemDefaultReply: vi.fn(),
}));

/** accounts 提供两个可切换账号，验证同名商品不会跨账号展示。 */
const accounts = [{ id: 'a', remark: '账号甲' }, { id: 'b', remark: '账号乙' }] as AccountDetail[];
/** items 是两个账号各自拥有的已同步商品。 */
const items = [{ id: 1, cookie_id: 'a', item_id: 'one', item_title: '甲的商品' }, { id: 2, cookie_id: 'b', item_id: 'one', item_title: '乙的商品' }] as Item[];
/** accountReply 是初始账号兜底，旧 URL 应原样进入表单。 */
const accountReply: DefaultReply = { cookie_id: 'a', enabled: true, reply_content: '账号甲正文', reply_once: true, reply_image_url: 'https://image.test/old.jpg' };
/** refreshAccounts 是不会发出真实请求的列表刷新器。 */
const refreshAccounts = vi.fn(async () => undefined);
/** notify 记录用户可见提示，不依赖浏览器原生 alert。 */
const notify = vi.fn();

/** Harness 使用真实动作 Hook 驱动列表和编辑器，测试用户可感知的完整行为。 */
function Harness() {
  /** actions 拥有本用例独立的配置表单、加载与提交状态。 */
  const actions = useDefaultReplyActions({ selectedAccountId: 'a', loadDefaultReplies: refreshAccounts, notify });
  return <><DefaultRepliesPanel selectedAccountId="a" accounts={accounts} replies={{ a: accountReply }} items={items} actions={actions} /><DefaultReplyEditor accounts={accounts} items={items} actions={actions} /></>;
}

beforeEach(/* 当前回调建立可预测的成功响应。 */ () => {
  vi.resetAllMocks();
  vi.mocked(getDefaultReply).mockImplementation(/* cookieID 是当前编辑器请求的账号，不复用其他账号内容。 */ async cookieID => cookieID === 'a' ? accountReply : { ...accountReply, cookie_id: 'b', reply_content: '账号乙正文', reply_image_url: '', reply_image_path: '乙/图片.png' });
  vi.mocked(getItemDefaultReplies).mockResolvedValue([]);
  vi.mocked(getItemDefaultReply).mockImplementation(/* cookieID/itemID 定位当前编辑目标，返回独立空配置。 */ async (cookieID, itemID) => ({ cookie_id: cookieID, item_id: itemID, reply_content: '', reply_image_url: '', reply_image_path: '' }));
  vi.mocked(updateDefaultReply).mockResolvedValue({ success: true });
  vi.mocked(updateItemDefaultReply).mockResolvedValue({ success: true });
  refreshAccounts.mockResolvedValue(undefined);
});
afterEach(/* 当前回调清理 portal 和全局替身。 */ () => { cleanup(); vi.unstubAllGlobals(); });

describe('默认回复列表和编辑器', /* 当前回调验证可访问控件与账号商品用户流程。 */ () => {
  test('旧 URL 切换为本地图片并保存，浏览器不加载本地图片', /* 当前回调覆盖本地图片操作及安全提示。 */ async () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: '编辑账号 a 默认回复' }));
    await waitFor(/* 当前回调等待真正的旧配置已读取。 */ () => expect((screen.getByRole('textbox', { name: '回复图片 URL' }) as HTMLInputElement).value).toBe('https://image.test/old.jpg'));
    fireEvent.change(screen.getByRole('combobox', { name: '图片来源' }), { target: { value: 'local' } });
    fireEvent.change(screen.getByRole('textbox', { name: '本地图片相对路径' }), { target: { value: '商品/默认.jpg' } });
    expect(screen.getByText(/XIANYU_UPLOAD_DIR\/reply-images\/a\//)).toBeTruthy();
    expect(screen.getByText(/PNG、JPEG 或 GIF/)).toBeTruthy();
    expect(screen.queryByRole('img')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '保存默认回复' }));
    await waitFor(/* 当前回调等待图文保存写入。 */ () => expect(updateDefaultReply).toHaveBeenCalledExactlyOnceWith('a', { enabled: true, reply_once: true, reply_content: '账号甲正文', reply_image_url: '', reply_image_path: '商品/默认.jpg' }));
    await waitFor(/* 当前回调等待成功后关闭编辑器。 */ () => expect(screen.queryByRole('dialog')).toBeNull());
  });

  test('商品选择按账号隔离，商品独立语义清晰，只有图片也能保存', /* 当前回调覆盖新增商品、范围切换和无文字图片回复。 */ async () => {
    vi.mocked(getItemDefaultReplies).mockResolvedValue([{ cookie_id: 'a', item_id: 'one', reply_content: '', reply_image_url: '', reply_image_path: 'one.png', reply_once: true }]);
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: '新增商品默认回复' }));
    expect(screen.getByRole('option', { name: '甲的商品 · one' })).toBeTruthy();
    expect(screen.queryByRole('option', { name: '乙的商品 · one' })).toBeNull();
    expect((screen.getByRole('checkbox', { name: /单个会话只回复一次/ }) as HTMLInputElement).checked).toBe(false);
    expect(screen.queryByRole('checkbox', { name: '启用账号默认回复' })).toBeNull();
    expect(screen.getByText(/商品配置独立生效，不继承账号启用或去重开关/)).toBeTruthy();
    fireEvent.change(screen.getByRole('combobox', { name: '关联商品' }), { target: { value: 'one' } });
    await waitFor(/* 当前回调等待商品配置读取完成后解锁来源选择器。 */ () => expect((screen.getByRole('combobox', { name: '图片来源' }) as HTMLSelectElement).closest('fieldset')?.disabled).toBe(false));
    fireEvent.change(screen.getByRole('combobox', { name: '图片来源' }), { target: { value: 'local' } });
    fireEvent.change(screen.getByRole('textbox', { name: '本地图片相对路径' }), { target: { value: 'one.png' } });
    fireEvent.click(screen.getByRole('checkbox', { name: /单个会话只回复一次/ }));
    fireEvent.click(screen.getByRole('button', { name: '保存默认回复' }));
    await waitFor(/* 当前回调确认只向当前账号商品发送图文及独立去重，不发送账号启用字段。 */ () => expect(updateItemDefaultReply).toHaveBeenCalledExactlyOnceWith('a', 'one', { reply_content: '', reply_image_url: '', reply_image_path: 'one.png', reply_once: true }));
    expect(await screen.findByText(/商品专属 · 单个会话只回复一次/)).toBeTruthy();
  });

  test('商品已勾选可关闭，失败保留选择，取消重开恢复服务端值', /* 当前回调覆盖独立开关的回显、失败保护、取消和显式关闭。 */ async () => {
    vi.mocked(getItemDefaultReply).mockResolvedValue({ cookie_id: 'a', item_id: 'one', reply_content: '商品正文', reply_image_url: '', reply_image_path: '', reply_once: true });
    vi.mocked(updateItemDefaultReply).mockRejectedValueOnce(new Error('写入失败'));
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: '新增商品默认回复' }));
    fireEvent.change(screen.getByRole('combobox', { name: '关联商品' }), { target: { value: 'one' } });
    await waitFor(/* 当前断言确保开关值已从商品详情加载，而非来自账号兜底。 */ () => expect((screen.getByRole('checkbox', { name: /单个会话只回复一次/ }) as HTMLInputElement).checked).toBe(true));
    fireEvent.click(screen.getByRole('checkbox', { name: /单个会话只回复一次/ }));
    fireEvent.click(screen.getByRole('button', { name: '保存默认回复' }));
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect((screen.getByRole('checkbox', { name: /单个会话只回复一次/ }) as HTMLInputElement).checked).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    fireEvent.click(screen.getByRole('button', { name: '新增商品默认回复' }));
    fireEvent.change(screen.getByRole('combobox', { name: '关联商品' }), { target: { value: 'one' } });
    await waitFor(/* 当前断言确认取消未把未保存false写回服务端配置。 */ () => expect((screen.getByRole('checkbox', { name: /单个会话只回复一次/ }) as HTMLInputElement).checked).toBe(true));
    fireEvent.click(screen.getByRole('checkbox', { name: /单个会话只回复一次/ }));
    fireEvent.click(screen.getByRole('button', { name: '保存默认回复' }));
    await waitFor(/* 当前断言确认显式关闭值和图文字段一起提交。 */ () => expect(updateItemDefaultReply).toHaveBeenLastCalledWith('a', 'one', { reply_content: '商品正文', reply_image_url: '', reply_image_path: '', reply_once: false }));
    await waitFor(/* 当前断言等待有效保存结束，不遗留后台刷新。 */ () => expect(screen.queryByRole('dialog')).toBeNull());
  });

  test('键盘焦点留在弹窗，Escape 关闭后恢复触发按钮', /* 当前回调验证编辑器可用的键盘生命周期。 */ async () => {
    render(<Harness />);
    // trigger 是打开账号编辑器前的可聚焦操作按钮。
    const trigger = screen.getByRole('button', { name: '编辑账号 a 默认回复' });
    trigger.focus();
    fireEvent.click(trigger);
    await waitFor(/* 当前回调等待配置读取完成。 */ () => expect((screen.getByRole('button', { name: '保存默认回复' }) as HTMLButtonElement).disabled).toBe(false));
    expect(document.activeElement).toBe(screen.getByRole('combobox', { name: '配置范围' }));
    // save/close 是 Tab 循环的两个边界。
    const save = screen.getByRole('button', { name: '保存默认回复' });
    // close 是弹窗内第一个可操作控件。
    const close = screen.getByRole('button', { name: '关闭默认回复编辑器' });
    save.focus();
    fireEvent.keyDown(save, { key: 'Tab' });
    expect(document.activeElement).toBe(close);
    fireEvent.keyDown(close, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(save);
    fireEvent.keyDown(save, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  test('读取失败后键盘重试仍保持弹窗焦点并允许 Escape 关闭', /* 当前回调验证重试按钮被移除后的焦点恢复。 */ async () => {
    vi.mocked(getDefaultReply).mockRejectedValueOnce(new Error('首次读取失败'));
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: '编辑账号 a 默认回复' }));
    // retry 是加载失败后出现、重试时会被移除的焦点控件。
    const retry = await screen.findByRole('button', { name: '重新加载' });
    // resolveRetry 用于在关闭后结算慢读取，确保测试不遗留等待任务。
    let resolveRetry!: (value: DefaultReply) => void;
    vi.mocked(getDefaultReply).mockReturnValueOnce(new Promise<DefaultReply>(/* resolve 保存慢读取的结算器。 */ resolve => { resolveRetry = resolve; }));
    retry.focus();
    fireEvent.click(retry);
    expect(screen.queryByRole('button', { name: '重新加载' })).toBeNull();
    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true);
    fireEvent.keyDown(document.activeElement || document.body, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    resolveRetry(accountReply);
  });

  test('编辑器切换账号重新加载，失败时禁止保存并支持重试或取消', /* 当前回调验证不把旧账号内容复制到新账号。 */ async () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: '编辑账号 a 默认回复' }));
    await waitFor(/* 当前回调等待甲账号配置。 */ () => expect((screen.getByRole('textbox', { name: '回复内容' }) as HTMLTextAreaElement).value).toBe('账号甲正文'));
    vi.mocked(getDefaultReply).mockRejectedValueOnce(new Error('网络异常'));
    fireEvent.change(screen.getByRole('combobox', { name: '闲鱼账号' }), { target: { value: 'b' } });
    await screen.findByRole('alert');
    expect((screen.getByRole('textbox', { name: '回复内容' }) as HTMLTextAreaElement).value).toBe('');
    expect((screen.getByRole('button', { name: '保存默认回复' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }));
    await waitFor(/* 当前回调等待乙账号自己的配置，而不是甲账号旧草稿。 */ () => expect((screen.getByRole('textbox', { name: '回复内容' }) as HTMLTextAreaElement).value).toBe('账号乙正文'));
    expect((screen.getByRole('textbox', { name: '本地图片相对路径' }) as HTMLInputElement).value).toBe('乙/图片.png');
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(updateDefaultReply).not.toHaveBeenCalled();
  });
});
