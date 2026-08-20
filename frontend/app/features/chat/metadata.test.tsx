// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import { createChatQuickReply,deleteChatQuickReply,getChatBuyerNote,getChatQuickReplies,saveChatBuyerNote } from './api';
import { useChatMetadata } from './metadata';
import type { ChatSession } from './models';

vi.mock('./api', /* chatMetadataApiMockFactory 提供快捷回复和买家备注 Hook 的确定性 API 替身。 */ () => ({
  createChatQuickReply: vi.fn(),
  deleteChatQuickReply: vi.fn(),
  getChatBuyerNote: vi.fn(),
  getChatQuickReplies: vi.fn(),
  saveChatBuyerNote: vi.fn(),
}));

// createQuickReplyMock 是创建快捷回复请求的可控替身。
const createQuickReplyMock = vi.mocked(createChatQuickReply);
// deleteQuickReplyMock 是删除快捷回复请求的可控替身。
const deleteQuickReplyMock = vi.mocked(deleteChatQuickReply);
// getBuyerNoteMock 是读取买家备注请求的可控替身。
const getBuyerNoteMock = vi.mocked(getChatBuyerNote);
// getQuickRepliesMock 是读取账号快捷回复请求的可控替身。
const getQuickRepliesMock = vi.mocked(getChatQuickReplies);
// saveBuyerNoteMock 是保存买家备注请求的可控替身。
const saveBuyerNoteMock = vi.mocked(saveChatBuyerNote);

// sessionFixture 是当前账号下供备注隔离测试使用的会话摘要。
const sessionFixture: ChatSession = { account_id: 'account-1', chat_id: 'chat-1', buyer_id: 'buyer-1', buyer_name: '买家', last_message: '', last_message_at: 1, unread_count: 0 };

/** QuickReplyFixture 是延迟响应测试使用的最小快捷回复形状。 */
type QuickReplyFixture = {
  /** id 是快捷回复稳定标识。 */
  id: number;
  /** account_id 是快捷回复所属账号。 */
  account_id: string;
  /** content 是快捷回复文本模板。 */
  content: string;
  /** created_at 是快捷回复创建时间。 */
  created_at: number;
};

/** MetadataHookProps 是可重渲染元数据 Hook 的账号与会话输入。 */
type MetadataHookProps = {
  /** accountID 是当前选择的闲鱼账号标识。 */
  accountID: string;
  /** session 是当前选中会话；缺失时不查询买家备注。 */
  session: ChatSession | undefined;
};

describe('useChatMetadata', /* 当前回调覆盖账号快捷回复与买家备注的异步状态和用户交互。 */ () => {
  beforeEach(/* 当前回调重置所有元数据 API 替身并配置默认成功响应。 */ () => {
    vi.clearAllMocks();
    getQuickRepliesMock.mockResolvedValue([{ id: 1, account_id: 'account-1', content: '您好', created_at: 1 }]);
    getBuyerNoteMock.mockResolvedValue({ account_id: 'account-1', buyer_id: 'buyer-1', content: '偏好顺丰', updated_at: 1 });
    createQuickReplyMock.mockResolvedValue({ id: 2, account_id: 'account-1', content: '现货可拍', created_at: 2 });
    deleteQuickReplyMock.mockResolvedValue({ success: true });
    saveBuyerNoteMock.mockResolvedValue({ account_id: 'account-1', buyer_id: 'buyer-1', content: '重点客户', updated_at: 2 });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });

  test('新增、复制和确认删除快捷回复，且保存买家备注', /* 当前回调验证元数据常用成功路径与编辑状态分离。 */ async () => {
    // hook 是聊天元数据 Hook 的渲染结果。
    const hook = renderHook(
      // metadataHookFactory 以稳定账号和会话创建元数据 Hook。
      () => useChatMetadata('account-1', sessionFixture),
    );
    await waitFor(
      // dataAssertion 等待账号快捷回复和买家备注的初始请求完成。
      () => expect(hook.result.current.quickReplies).toHaveLength(1),
    );
    expect(hook.result.current.buyerNote?.content).toBe('偏好顺丰');
    await act(
      // draftAction 写入新增快捷回复输入框。
      () => hook.result.current.setQuickReplyDraft('现货可拍'),
    );
    await act(
      // addAction 创建新的账号快捷回复。
      async () => hook.result.current.addQuickReply(),
    );
    expect(createQuickReplyMock).toHaveBeenCalledWith('account-1', '现货可拍');
    expect(hook.result.current.quickReplies.map(/* reply 保存当前列表中的快捷回复。 */ reply => reply.id)).toEqual([2, 1]);
    await act(
      // copyAction 将新创建快捷回复复制到系统剪贴板。
      async () => hook.result.current.copyQuickReply(hook.result.current.quickReplies[0]),
    );
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('现货可拍');
    await act(
      // openDeleteAction 打开新创建快捷回复的删除确认状态。
      () => hook.result.current.requestQuickReplyDelete(hook.result.current.quickReplies[0]),
    );
    await act(
      // deleteAction 确认并删除当前待确认快捷回复。
      async () => hook.result.current.confirmQuickReplyDelete(),
    );
    expect(deleteQuickReplyMock).toHaveBeenCalledWith('account-1', 2);
    expect(hook.result.current.quickReplies.map(/* reply 保存删除后剩余的快捷回复。 */ reply => reply.id)).toEqual([1]);
    await act(
      // openNoteAction 打开默认只读的买家备注弹窗。
      () => hook.result.current.openNoteDialog(),
    );
    expect(hook.result.current.noteEditing).toBe(false);
    await act(
      // editNoteAction 进入备注编辑态并填写新的文本。
      () => { hook.result.current.beginNoteEditing(); hook.result.current.setNoteDraft('重点客户'); },
    );
    await act(
      // saveNoteAction 保存当前买家备注。
      async () => hook.result.current.saveBuyerNote(),
    );
    expect(saveBuyerNoteMock).toHaveBeenCalledWith('account-1', 'buyer-1', '重点客户');
    expect(hook.result.current.buyerNote?.content).toBe('重点客户');
    expect(hook.result.current.noteEditing).toBe(false);
    hook.unmount();
  });

  test('账号切换后拒绝旧快捷回复响应', /* 当前回调验证请求代次保护不会让慢旧响应覆盖新账号。 */ async () => {
    // resolveOldReplies 保存稍后手动完成旧账号请求的 Promise 解析函数。
    let resolveOldReplies: ((replies: QuickReplyFixture[]) => void) | undefined;
    // oldRepliesPromise 是模拟慢旧账号快捷回复请求的受控 Promise。
    const oldRepliesPromise = new Promise<QuickReplyFixture[]>(/* resolve 保存外部可调用的旧请求完成函数。 */ resolve => { resolveOldReplies = resolve; });
    getQuickRepliesMock.mockImplementationOnce(/* 当前回调让首次账号请求保持 pending，模拟网络晚到。 */ () => oldRepliesPromise).mockResolvedValueOnce([{ id: 9, account_id: 'account-2', content: '新账号回复', created_at: 1 }]);
    // hook 是可通过 rerender 切换账号的聊天元数据 Hook。
    const hook = renderHook(
      // metadataHookFactory 根据渲染参数创建元数据 Hook。
      // props 保存当前账号和选中会话的测试输入。
      ({ accountID, session }: MetadataHookProps) => useChatMetadata(accountID, session),
      { initialProps: { accountID: 'account-1', session: sessionFixture } },
    );
    hook.rerender({ accountID: 'account-2', session: { ...sessionFixture, account_id: 'account-2', buyer_id: 'buyer-2' } });
    await waitFor(
      // newAccountAssertion 等待新账号请求先写入列表。
      () => expect(hook.result.current.quickReplies[0]?.id).toBe(9),
    );
    await act(
      // oldResponseAction 在新账号已生效后完成旧账号请求。
      () => resolveOldReplies?.([{ id: 8, account_id: 'account-1', content: '旧账号回复', created_at: 1 }]),
    );
    expect(hook.result.current.quickReplies[0]?.id).toBe(9);
    hook.unmount();
  });
});
