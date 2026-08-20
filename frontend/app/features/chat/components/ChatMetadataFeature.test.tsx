// @vitest-environment jsdom
import { fireEvent,render,screen,within } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import ChatMetadataFeature from './ChatMetadataFeature';
import { useChatMetadata } from '../metadata';
import type { ChatMetadataState } from '../metadata';
import type { ChatQuickReply,ChatSession } from '../models';

vi.mock('../metadata', /* chatMetadataMockFactory 为抽屉组件交互测试提供受控的元数据 Hook 替身。 */ () => ({ useChatMetadata: vi.fn() }));

// useChatMetadataMock 是可为每个测试设置返回状态的元数据 Hook 替身。
const useChatMetadataMock = vi.mocked(useChatMetadata);
// quickReplyFixture 是用于验证操作顺序和关闭行为的账号级快捷回复。
const quickReplyFixture: ChatQuickReply = { id: 1, account_id: 'account-1', content: '测试话术', created_at: 1 };
// sessionFixture 是使快捷回复发送按钮可用的当前聊天会话。
const sessionFixture: ChatSession = { account_id: 'account-1', chat_id: 'chat-1', buyer_id: 'buyer-1', buyer_name: '买家', last_message: '', last_message_at: 1, unread_count: 0 };

/** metadataFixture 构造抽屉组件交互所需的完整元数据状态，并允许测试观测复制动作。 */
const metadataFixture = (copyQuickReply: ChatMetadataState['copyQuickReply']): ChatMetadataState => ({
  quickReplies: [quickReplyFixture], quickReplyDraft: '', setQuickReplyDraft: vi.fn(), quickReplyBusy: false,
  pendingDeleteQuickReply: null, requestQuickReplyDelete: vi.fn(), cancelQuickReplyDelete: vi.fn(), confirmQuickReplyDelete: vi.fn().mockResolvedValue(undefined),
  addQuickReply: vi.fn().mockResolvedValue(undefined), copyQuickReply, copiedQuickReplyID: null, buyerNote: null, noteLoading: false,
  noteDialogOpen: false, noteEditing: false, noteDraft: '', setNoteDraft: vi.fn(), noteSaving: false, openNoteDialog: vi.fn(),
  closeNoteDialog: vi.fn(), beginNoteEditing: vi.fn(), saveBuyerNote: vi.fn().mockResolvedValue(undefined), metadataError: '', clearMetadataError: vi.fn(),
});

describe('ChatMetadataFeature', /* 当前回调验证快捷回复抽屉的操作排序和关闭交互。 */ () => {
  beforeEach(/* 当前回调在每个抽屉交互场景前重置 Hook 替身调用记录。 */ () => {
    vi.clearAllMocks();
  });

  test('删除、复制、发送排序正确，复制发送和外部点击均收起抽屉', /* 当前回调验证快捷回复操作的高频关闭流程。 */ async () => {
    // copyQuickReply 保存可观测的复制动作替身。
    const copyQuickReply = vi.fn().mockResolvedValue(undefined);
    // closeQuickReplyPanel 保存抽屉关闭动作替身。
    const closeQuickReplyPanel = vi.fn();
    // sendQuickReply 保存当前会话模板发送动作替身。
    const sendQuickReply = vi.fn().mockResolvedValue(undefined);
    useChatMetadataMock.mockReturnValue(metadataFixture(copyQuickReply));
    render(<><ChatMetadataFeature activeAccountID="account-1" selectedSession={sessionFixture} quickReplyPanelOpen closeQuickReplyPanel={closeQuickReplyPanel} sendQuickReply={sendQuickReply} sending={false} accountOnline /><button type="button">抽屉外区域</button></>);
    // replyCard 是包含当前快捷回复操作按钮的卡片元素。
    const replyCard = screen.getByText('测试话术').closest('article');
    if (replyCard == null) throw new Error('未找到快捷回复卡片');
    // actionNames 保存卡片内按钮按视觉顺序的可访问名称。
    const actionNames = within(replyCard).getAllByRole('button').map(/* actionButton 保存当前待检查的快捷回复操作按钮。 */ actionButton => actionButton.getAttribute('aria-label') || actionButton.textContent);
    expect(actionNames).toEqual(['删除快捷回复', '复制', '发送']);
    fireEvent.click(within(replyCard).getByRole('button', { name: '复制' }));
    expect(copyQuickReply).toHaveBeenCalledWith(quickReplyFixture);
    expect(closeQuickReplyPanel).toHaveBeenCalledTimes(1);
    fireEvent.click(within(replyCard).getByRole('button', { name: '发送' }));
    expect(sendQuickReply).toHaveBeenCalledWith('测试话术');
    expect(closeQuickReplyPanel).toHaveBeenCalledTimes(2);
    fireEvent.pointerDown(screen.getByRole('button', { name: '抽屉外区域' }));
    expect(closeQuickReplyPanel).toHaveBeenCalledTimes(3);
  });
});
