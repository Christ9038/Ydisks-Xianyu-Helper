// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { clearDefaultReplyRecords,deleteDefaultReply,deleteItemDefaultReply,getDefaultReply,getItemDefaultReplies,getItemDefaultReply,updateDefaultReply,updateItemDefaultReply } from './api';
import { useDefaultReplyActions } from './defaultReplyActions';
import type { DefaultReply,ItemDefaultReply } from './models';

vi.mock('./api', /* 当前工厂提供默认回复用例的独立 API 替身。 */ () => ({
  clearDefaultReplyRecords: vi.fn(), deleteDefaultReply: vi.fn(), deleteItemDefaultReply: vi.fn(),
  getDefaultReply: vi.fn(), getItemDefaultReplies: vi.fn(), getItemDefaultReply: vi.fn(),
  updateDefaultReply: vi.fn(), updateItemDefaultReply: vi.fn(),
}));

/** accountReply 是兼容旧接口、未携带本地图片字段的账号配置。 */
const accountReply: DefaultReply = { cookie_id: 'a', enabled: true, reply_content: '账号欢迎', reply_once: true, reply_image_url: 'https://image.test/old.jpg' };
/** itemReply 是覆盖账号图文的商品配置。 */
const itemReply: ItemDefaultReply = { cookie_id: 'a', item_id: 'item-1', reply_content: '商品欢迎', reply_image_url: '', reply_image_path: '商品/欢迎.png' };
/** notify 记录本地轻提示，迟到操作不能写入新的账号界面。 */
const notify = vi.fn();
/** refreshAccounts 记录已保存账号配置后的列表刷新。 */
const refreshAccounts = vi.fn(async () => undefined);

/** deferred 构造可控请求，resolve/reject 由每个测试明确触发以验证乱序和取消。 */
function deferred<T>() {
  /** resolve 完成可控成功请求。 */
  let resolve!: (value: T) => void;
  /** reject 完成可控失败请求。 */
  let reject!: (error: Error) => void;
  /** promise 是交给 API 替身的待定请求。 */
  const promise = new Promise<T>(/* onResolve/onReject 是原生 Promise 结算器。 */ (onResolve, onReject) => { resolve = onResolve; reject = onReject; });
  return { promise, resolve, reject };
}

/** setup 为单次测试创建可切换账号的真实 Hook，所有网络动作均被隔离。 */
const setup = () => renderHook(/* props 只包含当前账号筛选，rerender 用于模拟用户切换。 */ ({ account }: { /** account 是当前规则页面的账号筛选。 */ account: string }) => useDefaultReplyActions({ selectedAccountId: account, loadDefaultReplies: refreshAccounts, notify }), { initialProps: { account: 'a' } });

beforeEach(/* 当前回调重置成功基线，错误用例再局部覆盖。 */ () => {
  vi.resetAllMocks();
  vi.mocked(getDefaultReply).mockResolvedValue(accountReply);
  vi.mocked(getItemDefaultReply).mockResolvedValue(itemReply);
  vi.mocked(getItemDefaultReplies).mockResolvedValue([itemReply]);
  vi.mocked(updateDefaultReply).mockResolvedValue({ success: true });
  vi.mocked(updateItemDefaultReply).mockResolvedValue({ success: true });
  vi.mocked(deleteDefaultReply).mockResolvedValue({ success: true });
  vi.mocked(deleteItemDefaultReply).mockResolvedValue({ success: true });
  vi.mocked(clearDefaultReplyRecords).mockResolvedValue({ success: true });
  refreshAccounts.mockResolvedValue(undefined);
  vi.stubGlobal('confirm', vi.fn(() => true));
});
afterEach(/* 当前回调清理确认弹窗替身，不污染其他 Hook 测试。 */ () => vi.unstubAllGlobals());

describe('默认回复动作与异步隔离', /* 当前回调覆盖图文配置、失败、切换、取消及去重。 */ () => {
  test('账号旧 URL 可编辑并切换本地，商品保存携带独立once但不携带账号启用开关', /* 当前回调验证两类保存载荷及刷新目标。 */ async () => {
    /** hook 拥有本用例的草稿及请求代次。 */
    const hook = setup();
    await act(/* 当前回调读取账号配置。 */ async () => hook.result.current.openDefaultReplyModal());
    expect(hook.result.current.defaultForm.image_source).toBe('url');
    act(/* 当前回调切换为一张本地图片。 */ () => hook.result.current.setDefaultForm(/* current 是已有账号草稿。 */ current => ({ ...current, image_source: 'local', reply_image_path: '商品/新图.jpg' })));
    await act(/* 当前回调保存账号图文。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateDefaultReply).toHaveBeenCalledExactlyOnceWith('a', { enabled: true, reply_once: true, reply_content: '账号欢迎', reply_image_url: '', reply_image_path: '商品/新图.jpg' });
    expect(refreshAccounts).toHaveBeenCalledOnce();
    await act(/* 当前回调打开具体商品。 */ async () => hook.result.current.openItemDefaultReplyModal('a', 'item-1'));
    await act(/* 当前回调保存商品配置。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateItemDefaultReply).toHaveBeenCalledExactlyOnceWith('a', 'item-1', { reply_content: '商品欢迎', reply_image_url: '', reply_image_path: '商品/欢迎.png', reply_once: false });
    expect(getItemDefaultReplies).toHaveBeenCalledOnce();
    expect(hook.result.current.showDefaultModal).toBe(false);
    hook.unmount();
  });

  test('详情读取失败不能覆盖为空，重试成功后才可保存', /* 当前回调验证失败、重试和保存边界。 */ async () => {
    vi.mocked(getDefaultReply).mockRejectedValueOnce(new Error('读取中断'));
    /** hook 拥有本用例的错误及加载状态。 */
    const hook = setup();
    await act(/* 当前回调加载会失败的账号。 */ async () => hook.result.current.openDefaultReplyModal());
    expect(hook.result.current.defaultReplyLoadFailed).toBe(true);
    expect(hook.result.current.defaultReplyError).toContain('读取中断');
    await act(/* 当前回调尝试保存尚未取得的配置。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateDefaultReply).not.toHaveBeenCalled();
    await act(/* 当前回调重试并取得服务端配置。 */ async () => hook.result.current.openDefaultReplyModal());
    expect(hook.result.current.defaultReplyLoadFailed).toBe(false);
    expect(hook.result.current.defaultForm.reply_content).toBe('账号欢迎');
    hook.unmount();
  });

  test('切换商品和账号后旧详情不覆盖新草稿，关闭后不会重新打开', /* 当前回调交错结算三个不同编辑器代次。 */ async () => {
    /** first/second 分别控制旧商品与关闭后的账号读取。 */
    const first = deferred<ItemDefaultReply>();
    /** second 控制关闭后的账号读取。 */
    const second = deferred<DefaultReply>();
    vi.mocked(getItemDefaultReply).mockReturnValueOnce(first.promise);
    /** hook 是支持范围切换的动作实例。 */
    const hook = setup();
    /** oldRequest 保留旧商品打开动作，后续才结算。 */
    let oldRequest!: Promise<void>;
    act(/* 当前回调发起旧商品读取。 */ () => { oldRequest = hook.result.current.openItemDefaultReplyModal('a', 'old'); });
    await act(/* 当前回调选择新商品但先不选具体项目。 */ async () => hook.result.current.openItemDefaultReplyModal('a'));
    expect(hook.result.current.defaultForm.item_id).toBe('');
    expect(hook.result.current.defaultForm.reply_once).toBe(false);
    await act(/* 当前回调让旧详情迟到。 */ async () => { first.resolve({ ...itemReply, reply_once: true }); await oldRequest; });
    expect(hook.result.current.defaultForm.item_id).toBe('');
    expect(hook.result.current.defaultForm.reply_once).toBe(false);
    vi.mocked(getDefaultReply).mockReturnValueOnce(second.promise);
    /** closedRequest 是随后会被关闭撤销的读取。 */
    let closedRequest!: Promise<void>;
    act(/* 当前回调发起账号读取。 */ () => { closedRequest = hook.result.current.openDefaultReplyModal('a'); });
    act(/* 当前回调关闭编辑器。 */ () => hook.result.current.setShowDefaultModal(false));
    await act(/* 当前回调结算已被取消的读取。 */ async () => { second.resolve(accountReply); await closedRequest; });
    expect(hook.result.current.showDefaultModal).toBe(false);
    await act(/* 当前回调重新打开旧账号。 */ async () => hook.result.current.openDefaultReplyModal());
    hook.rerender({ account: 'b' });
    expect(hook.result.current.showDefaultModal).toBe(false);
    hook.unmount();
  });

  test('连续保存只写一次，失败保留草稿可重试，旧成功不关闭新编辑器', /* 当前回调覆盖提交锁和写入迟到结果隔离。 */ async () => {
    /** save 是暂未结算的第一次保存。 */
    const save = deferred<{ /** success 是模拟接口的操作成功标记。 */ success: boolean }>();
    vi.mocked(updateDefaultReply).mockReturnValueOnce(save.promise);
    /** hook 是当前编辑会话。 */
    const hook = setup();
    await act(/* 当前回调读取账号配置。 */ async () => hook.result.current.openDefaultReplyModal());
    /** pending 保存首次提交的异步完成句柄。 */
    let pending!: Promise<void>;
    act(/* 当前回调在 React 重渲染前连续触发保存。 */ () => { pending = hook.result.current.handleSaveDefaultReply(); void hook.result.current.handleSaveDefaultReply(); });
    expect(updateDefaultReply).toHaveBeenCalledOnce();
    await act(/* 当前回调使第一次保存失败。 */ async () => { save.reject(new Error('写入中断')); await pending; });
    expect(hook.result.current.showDefaultModal).toBe(true);
    expect(hook.result.current.defaultForm.reply_content).toBe('账号欢迎');
    expect(hook.result.current.defaultReplySubmitState.submitting).toBe(false);
    expect(hook.result.current.defaultReplyError).toContain('写入中断');
    /** retry 控制重试的迟到成功。 */
    const retry = deferred<{ /** success 是模拟接口的操作成功标记。 */ success: boolean }>();
    vi.mocked(updateDefaultReply).mockReturnValueOnce(retry.promise);
    act(/* 当前回调发出重试。 */ () => { pending = hook.result.current.handleSaveDefaultReply(); });
    await act(/* 当前回调打开不同商品的编辑器。 */ async () => hook.result.current.openItemDefaultReplyModal('a', 'item-1'));
    await act(/* 当前回调结算旧保存，不得关闭商品编辑器。 */ async () => { retry.resolve({ success: true }); await pending; });
    expect(hook.result.current.showDefaultModal).toBe(true);
    expect(hook.result.current.defaultForm.item_id).toBe('item-1');
    expect(refreshAccounts).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('保存锁跨关闭和账号切换保留，旧请求结束前不能再次写入同一配置', /* 当前回调复现旧保存晚落库覆盖新配置的竞态。 */ async () => {
    // save 控制第一次 PUT 的完成时间，不把关闭弹窗当成网络取消。
    const save = deferred<{ /** success 是模拟接口的操作结果。 */ success: boolean }>();
    vi.mocked(updateDefaultReply).mockReturnValueOnce(save.promise);
    // hook 在切换账号和重新打开时保留同一写入生命周期。
    const hook = setup();
    await act(/* 当前回调加载首次编辑目标。 */ async () => hook.result.current.openDefaultReplyModal('a'));
    // pending 保存首次写入，直到断言第二次被阻止后才结算。
    let pending!: Promise<void>;
    act(/* 当前回调开始慢保存。 */ () => { pending = hook.result.current.handleSaveDefaultReply(); });
    act(/* 当前回调关闭编辑器但原 PUT 仍在服务端执行。 */ () => hook.result.current.setShowDefaultModal(false));
    hook.rerender({ account: 'b' });
    await act(/* 当前回调重新打开相同账号，不能解除原写入锁。 */ async () => hook.result.current.openDefaultReplyModal('a'));
    expect(hook.result.current.defaultReplySubmitState.submitting).toBe(true);
    await act(/* 当前回调尝试保存同一配置，应被在途锁拒绝。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateDefaultReply).toHaveBeenCalledOnce();
    await act(/* 当前回调完成原请求后才允许新保存。 */ async () => { save.resolve({ success: true }); await pending; });
    expect(hook.result.current.showDefaultModal).toBe(true);
    expect(hook.result.current.defaultReplySubmitState.submitting).toBe(false);
    await act(/* 当前回调在旧请求完成后再次保存。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateDefaultReply).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  test('列表删除锁跨账号切换保留，删除未完成时不会保存或再次删除', /* 当前回调验证列表和编辑器共用实际写入边界。 */ async () => {
    // deletion 控制尚未在服务端完成的账号配置删除。
    const deletion = deferred<{ /** success 是模拟删除结果。 */ success: boolean }>();
    vi.mocked(deleteDefaultReply).mockReturnValueOnce(deletion.promise);
    // hook 用账号筛选切换复现旧操作代次被撤销。
    const hook = setup();
    // pending 保存当前删除的完成句柄。
    let pending!: Promise<void>;
    act(/* 当前回调开始慢删除。 */ () => { pending = hook.result.current.handleDeleteDefaultReply('a'); });
    hook.rerender({ account: 'b' });
    await act(/* 当前回调读取被删除目标，原操作仍然阻止新写入。 */ async () => hook.result.current.openDefaultReplyModal('a'));
    expect(hook.result.current.defaultReplyMutationBusy).toBe(true);
    await act(/* 当前回调尝试在删除中保存或再删除。 */ async () => {
      await hook.result.current.handleSaveDefaultReply();
      await hook.result.current.handleDeleteDefaultReply('a');
    });
    expect(updateDefaultReply).not.toHaveBeenCalled();
    expect(deleteDefaultReply).toHaveBeenCalledOnce();
    await act(/* 当前回调结束删除后释放真实副作用锁。 */ async () => { deletion.resolve({ success: true }); await pending; });
    expect(hook.result.current.defaultReplyMutationBusy).toBe(false);
    await act(/* 当前回调在删除完成后保存新配置。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateDefaultReply).toHaveBeenCalledOnce();
    hook.unmount();
  });

  test('商品列表只采纳最新响应，失败可见；删除取消不触发网络，确认后恢复继承', /* 当前回调验证列表乱序、错误与删除副作用。 */ async () => {
    /** oldList 是较早发出的列表读取。 */
    const oldList = deferred<ItemDefaultReply[]>();
    vi.mocked(getItemDefaultReplies).mockReturnValueOnce(oldList.promise);
    /** hook 是商品列表状态容器。 */
    const hook = setup();
    /** pending 是旧刷新动作。 */
    let pending!: Promise<void>;
    act(/* 当前回调开始慢刷新。 */ () => { pending = hook.result.current.loadItemDefaultReplies(); });
    await act(/* 当前回调立即开始并完成新刷新。 */ async () => hook.result.current.loadItemDefaultReplies());
    await act(/* 当前回调结算旧空列表。 */ async () => { oldList.resolve([]); await pending; });
    expect(hook.result.current.itemDefaultReplies).toEqual([itemReply]);
    vi.mocked(getItemDefaultReplies).mockRejectedValueOnce(new Error('列表失败'));
    await act(/* 当前回调读取失败应保留错误而非伪装为空。 */ async () => hook.result.current.loadItemDefaultReplies());
    expect(hook.result.current.itemRepliesError).toContain('列表失败');
    vi.mocked(confirm).mockReturnValueOnce(false);
    await act(/* 当前回调取消删除。 */ async () => hook.result.current.handleDeleteItemDefaultReply('a', 'item-1'));
    expect(deleteItemDefaultReply).not.toHaveBeenCalled();
    await act(/* 当前回调确认删除商品覆盖。 */ async () => hook.result.current.handleDeleteItemDefaultReply('a', 'item-1'));
    expect(deleteItemDefaultReply).toHaveBeenCalledExactlyOnceWith('a', 'item-1');
    expect(notify).toHaveBeenCalledWith('success', '已恢复账号兜底');
    hook.unmount();
  });

  test('账号记录清理和删除遵循确认，列表操作串行且失败可重试', /* 当前回调覆盖列表副作用的成功、失败、取消和并发保护。 */ async () => {
    // hook 是列表操作的状态容器。
    const hook = setup();
    await act(/* 当前回调拒绝未指定账号的编辑请求。 */ async () => hook.result.current.openDefaultReplyModal(''));
    expect(notify).toHaveBeenCalledWith('error', '请先选择账号');
    vi.mocked(confirm).mockReturnValueOnce(false);
    await act(/* 当前回调取消账号删除。 */ async () => hook.result.current.handleDeleteDefaultReply('a'));
    expect(deleteDefaultReply).not.toHaveBeenCalled();
    vi.mocked(confirm).mockReturnValueOnce(false);
    await act(/* 当前回调取消会话记录清理。 */ async () => hook.result.current.handleClearDefaultReplyRecords('a'));
    expect(clearDefaultReplyRecords).not.toHaveBeenCalled();
    await act(/* 当前回调确认清空账号会话记录。 */ async () => hook.result.current.handleClearDefaultReplyRecords('a'));
    expect(clearDefaultReplyRecords).toHaveBeenCalledExactlyOnceWith('a');
    // removal 控制第一次删除，让连续点击落在同一在途窗口。
    const removal = deferred<{ /** success 是模拟接口删除结果。 */ success: boolean }>();
    vi.mocked(deleteDefaultReply).mockReturnValueOnce(removal.promise);
    // pending 是第一次已确认删除的操作。
    let pending!: Promise<void>;
    act(/* 当前回调连续触发两次删除，第二次必须被同步锁拒绝。 */ () => { pending = hook.result.current.handleDeleteDefaultReply('a'); void hook.result.current.handleDeleteDefaultReply('a'); });
    expect(deleteDefaultReply).toHaveBeenCalledOnce();
    await act(/* 当前回调结算失败删除并解除忙碌状态。 */ async () => { removal.reject(new Error('删除失败')); await pending; });
    expect(hook.result.current.defaultReplyMutationBusy).toBe(false);
    expect(notify).toHaveBeenCalledWith('error', '操作失败：删除失败');
    refreshAccounts.mockRejectedValueOnce(new Error('刷新失败'));
    await act(/* 当前回调重试删除成功，但列表刷新失败须明确提示。 */ async () => hook.result.current.handleDeleteDefaultReply('a'));
    expect(notify).toHaveBeenCalledWith('success', '删除成功');
    expect(notify).toHaveBeenCalledWith('error', '操作已完成，但刷新失败：刷新失败');
    hook.unmount();
  });

  test('保存成功后的刷新失败不重新打开编辑器，切换账号后旧删除不再反馈', /* 当前回调验证已完成副作用与失败读取、迟到反馈分别处理。 */ async () => {
    // hook 是当前账号的编辑和列表动作容器。
    const hook = setup();
    await act(/* 当前回调读取已有账号配置。 */ async () => hook.result.current.openDefaultReplyModal());
    refreshAccounts.mockRejectedValueOnce(new Error('刷新失败'));
    await act(/* 当前回调完成真实保存，后续只允许报告刷新失败。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(hook.result.current.showDefaultModal).toBe(false);
    expect(hook.result.current.defaultReplySubmitState.result).toBe('success');
    expect(notify).toHaveBeenCalledWith('error', '已保存，但刷新列表失败：刷新失败');
    notify.mockClear();
    // deletion 在用户切换账号后才完成。
    const deletion = deferred<{ /** success 是模拟删除结果。 */ success: boolean }>();
    vi.mocked(deleteDefaultReply).mockReturnValueOnce(deletion.promise);
    // pending 保存旧账号删除的完成句柄。
    let pending!: Promise<void>;
    act(/* 当前回调开始旧账号删除。 */ () => { pending = hook.result.current.handleDeleteDefaultReply('a'); });
    hook.rerender({ account: 'b' });
    await act(/* 当前回调结算已经离开的账号操作。 */ async () => { deletion.resolve({ success: true }); await pending; });
    expect(notify).not.toHaveBeenCalled();
    expect(hook.result.current.defaultReplyMutationBusy).toBe(false);
    hook.unmount();
  });

  test('保存输入校验拒绝无商品和穿越路径，卸载后读取结果不通知', /* 当前回调覆盖新建校验及卸载取消。 */ async () => {
    /** hook 是用于校验新建商品配置的实例。 */
    const hook = setup();
    await act(/* 当前回调打开尚未选择商品的新建草稿。 */ async () => hook.result.current.openItemDefaultReplyModal());
    await act(/* 当前回调提交缺少商品的草稿。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(hook.result.current.defaultReplyError).toBe('请选择关联商品');
    await act(/* 当前回调读取已有商品。 */ async () => hook.result.current.openItemDefaultReplyModal('a', 'item-1'));
    act(/* 当前回调写入不能越界的相对路径。 */ () => hook.result.current.setDefaultForm(/* form 是商品草稿。 */ form => ({ ...form, reply_image_path: '../private.png' })));
    await act(/* 当前回调校验本地路径。 */ async () => hook.result.current.handleSaveDefaultReply());
    expect(updateItemDefaultReply).not.toHaveBeenCalled();
    /** pendingList 在组件卸载后才失败。 */
    const pendingList = deferred<ItemDefaultReply[]>();
    vi.mocked(getItemDefaultReplies).mockReturnValueOnce(pendingList.promise);
    /** request 是卸载前发出的列表请求。 */
    let request!: Promise<void>;
    act(/* 当前回调开始最终一次列表刷新。 */ () => { request = hook.result.current.loadItemDefaultReplies(); });
    hook.unmount();
    pendingList.reject(new Error('卸载后失败'));
    await request;
    expect(notify).not.toHaveBeenCalled();
  });
});
