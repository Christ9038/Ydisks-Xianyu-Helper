import {
AccountDetail,
ChatBuyerNote,
ChatItem,
ChatItemPage,
ChatMessage,
ChatQuickReply,
ChatSession,
OperationResponse
} from './models';
import { contractClient, contractMultipartBody, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import { ApiError } from '../../../shared/http/client';
export type * from './models';
import type { ChatReadReceipt } from './types';

/** 聊天账号选择器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => {
  // response 是账号摘要 transport DTO 集合，转换后只向聊天 UI 暴露非敏感字段。
  const response = await runContractRequest(/* signal 是本次聊天账号摘要请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);
  return response.map(/* item 是当前待转换的账号摘要 DTO。 */ item => ({
    id: item.id,
    enabled: item.enabled,
    auto_confirm: item.auto_confirm,
    remark: item.remark,
    pause_duration: item.pause_duration,
    paused_until: item.paused_until,
    paused: item.paused,
    username: item.username,
    show_browser: item.show_browser,
    nickname: item.nickname,
    avatar_url: item.avatar_url,
    profile_error: item.profile_error,
  }));
};

/** 聊天运行提示读取账号连接状态索引。 */
export const getAccountRuntimeStatuses = async (options?: RequestControlOptions): Promise<Record<string, { /** 当前连接状态。 */ state: NonNullable<AccountDetail['runtime_state']>; /** 状态说明。 */ message?: string; /** 是否已连接。 */ connected: boolean; /** 连续失败次数。 */ failures: number; /** 最近更新时间。 */ updated_at: string }>> =>
  runContractRequest(/* signal 是本次聊天运行状态请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/accounts/runtime-status', { signal }), options);
export interface ChatSessionPage { /** sessions 表示聊天会话列表。 */ sessions: ChatSession[]; /** has_more 表示任一分页来源仍有更多数据。 */ has_more: boolean; /** next_cursor 表示下一平台页游标。 */ next_cursor?: number; /** next_stored_cursor 表示下一本地缓存页游标。 */ next_stored_cursor?: string; /** platform_has_more 表示平台联系人是否仍有下一页；旧响应缺失时回退 has_more。 */ platform_has_more?: boolean; /** stored_has_more 表示本地缓存联系人是否仍有下一页；旧响应缺失时回退 has_more。 */ stored_has_more?: boolean }

// getChatSessionPage 分页读取聊天会话。
export const getChatSessionPage = async (accountId: string, cursor?: number, options?: RequestControlOptions, refresh = false, storedCursor?: string): Promise<ChatSessionPage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const response = await runContractRequest(
    /* signal 是本次聊天会话分页请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/sessions', {
      params: { query: { account_id: accountId, cursor, stored_cursor: storedCursor, refresh: refresh ? 1 : undefined } },
      signal,
    }),
		{ timeoutMs: refresh ? 60_000 : options?.timeoutMs, signal: options?.signal },
	);
	return response;
};

// getChatSessions 读取聊天会话列表。
export const getChatSessions = async (accountId: string, options?: RequestControlOptions): Promise<ChatSession[]> =>
	(await getChatSessionPage(accountId, undefined, options)).sessions;

/** 在本机隐藏指定会话并物理清空其展示消息。 */
export const deleteChatSession = async (accountId: string, chatId: string, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次会话删除请求的超时与取消控制信号。 */ signal => contractClient.DELETE('/api/v1/chat/sessions', {
    params: { query: { account_id: accountId, chat_id: chatId } },
    signal,
  }), options);

/** 判断未知值是否满足聊天消息展示模型的全部必填字段。 */
const isChatMessage = (value: unknown): value is ChatMessage => {
  if (!value || typeof value !== 'object') return false;
  // candidate 保存待验证的未知错误详情字段。
  const candidate = value as Record<string, unknown>;
  return typeof candidate.id === 'number' && typeof candidate.account_id === 'string' && typeof candidate.chat_id === 'string'
    && typeof candidate.message_key === 'string' && (candidate.direction === 'incoming' || candidate.direction === 'outgoing')
    && typeof candidate.sender_id === 'string' && typeof candidate.sender_name === 'string'
    && ['text', 'image', 'video', 'audio', 'item', 'system'].includes(String(candidate.message_type))
    && typeof candidate.content === 'string' && ['received', 'sending', 'sent', 'failed', 'uncertain'].includes(String(candidate.status))
    && typeof candidate.sent_at === 'number';
};

/** 从结果待确认错误中提取不可自动重试的外发消息。 */
export const uncertainOutgoingMessageFromError = (error: unknown): ChatMessage | undefined => {
	if (!(error instanceof ApiError) || !['chat_send_uncertain', 'chat_image_send_uncertain', 'chat_item_card_send_uncertain'].includes(error.code)) return undefined;
	// outgoingMessage 保存后端错误详情中的待确认消息 DTO。
	const outgoingMessage = error.details?.outgoing_message;
	if (!isChatMessage(outgoingMessage)) return undefined;
	return { ...outgoingMessage, status: 'uncertain' };
};

/** 从“远端已发送但状态收口失败”错误中提取不可重试的外发消息。 */
export const confirmedOutgoingMessageFromError = (error: unknown): ChatMessage | undefined => {
	if (!(error instanceof ApiError) || error.code !== 'chat_send_status_save_failed') return undefined;
  // outgoingMessage 保存后端统一错误详情中的 snake_case 消息 DTO。
  const outgoingMessage = error.details?.outgoing_message;
  if (!isChatMessage(outgoingMessage)) return undefined;
  return { ...outgoingMessage, status: 'sent' };
};

export interface ChatMessagePage {
	/** messages 表示聊天消息列表。 */ messages: ChatMessage[];
	/** has_more 表示是否存在更多数据。 */ has_more: boolean;
	/** next_cursor 表示下一页游标。 */ next_cursor?: number;
	/** session 表示会话。 */ session?: ChatSession;
}

// getChatMessagePage 分页读取聊天消息。
export const getChatMessagePage = async (accountId: string, chatId: string, cursor?: number, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessagePage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const response = await runContractRequest(/* signal 是本次聊天消息分页请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/messages', {
		params: { query: { account_id: accountId, chat_id: chatId, cursor, before_id: beforeId } },
		signal,
	}), options);
	return response;
};

// getChatMessages 读取聊天消息列表。
export const getChatMessages = async (accountId: string, chatId: string, beforeId?: number, options?: RequestControlOptions): Promise<ChatMessage[]> =>
	(await getChatMessagePage(accountId, chatId, undefined, beforeId, options)).messages;

/** 查询当前账号可复用的人工快捷回复。 */
export const getChatQuickReplies = async (accountId: string, options?: RequestControlOptions): Promise<ChatQuickReply[]> => {
  // response 保存契约客户端返回的快捷回复列表 DTO，随后转换为 feature UI 模型。
  const response = await runContractRequest(/* signal 是本次账号快捷回复查询的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/quick-replies', {
    params: { query: { account_id: accountId } },
    signal,
  }), options);
  return response.quick_replies.map(/* reply 是当前待转换的快捷回复 DTO。 */ reply => ({
    id: reply.id,
    account_id: reply.account_id,
    content: reply.content,
    library_image_id: reply.library_image_id ?? 0,
    created_at: reply.created_at,
  }));
};

/** 为当前账号新增人工快捷回复。 */
export const createChatQuickReply = async (accountId: string, content: string, options?: RequestControlOptions): Promise<ChatQuickReply> => {
  // response 保存创建接口返回的快捷回复 DTO，转换后不向组件暴露生成类型。
  const response = await runContractRequest(/* signal 是本次新增快捷回复请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/quick-replies', {
    body: { account_id: accountId, content },
    signal,
  }), options);
  return { id: response.id, account_id: response.account_id, content: response.content, library_image_id: response.library_image_id ?? 0, created_at: response.created_at };
};

/** 为当前账号新增图片快捷回复；图片来自当前用户图片库，发送时由服务端读取字节。 */
export const createChatImageQuickReply = async (accountId: string, libraryImageId: number, options?: RequestControlOptions): Promise<ChatQuickReply> => {
  // response 保存创建接口返回的快捷回复 DTO，转换后不向组件暴露生成类型。
  const response = await runContractRequest(/* signal 是本次新增图片快捷回复请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/quick-replies', {
    body: { account_id: accountId, library_image_id: libraryImageId },
    signal,
  }), options);
  return { id: response.id, account_id: response.account_id, content: response.content, library_image_id: response.library_image_id ?? 0, created_at: response.created_at };
};

/** 删除当前账号下指定的人工快捷回复。 */
export const deleteChatQuickReply = async (accountId: string, quickReplyId: number, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次删除快捷回复请求的超时与取消控制信号。 */ signal => contractClient.DELETE('/api/v1/chat/quick-replies/{quick_reply_id}', {
    params: { path: { quick_reply_id: quickReplyId }, query: { account_id: accountId } },
    signal,
  }), options);

/** 查询当前账号下指定买家的完整备注；未填写时仍返回空内容。 */
export const getChatBuyerNote = async (accountId: string, buyerId: string, options?: RequestControlOptions): Promise<ChatBuyerNote> => {
  // response 保存契约客户端返回的买家备注 DTO，转换后由聊天 feature 管理。
  const response = await runContractRequest(/* signal 是本次买家备注查询的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/buyer-notes/{buyer_id}', {
    params: { path: { buyer_id: buyerId }, query: { account_id: accountId } },
    signal,
  }), options);
  return { account_id: response.account_id, buyer_id: response.buyer_id, content: response.content, updated_at: response.updated_at };
};

/** 保存当前账号下指定买家的完整备注；空内容会清除已有备注。 */
export const saveChatBuyerNote = async (accountId: string, buyerId: string, content: string, options?: RequestControlOptions): Promise<ChatBuyerNote> => {
  // response 保存保存接口返回的最终买家备注 DTO。
  const response = await runContractRequest(/* signal 是本次买家备注保存请求的超时与取消控制信号。 */ signal => contractClient.PUT('/api/v1/chat/buyer-notes/{buyer_id}', {
    params: { path: { buyer_id: buyerId } },
    body: { account_id: accountId, content },
    signal,
  }), options);
  return { account_id: response.account_id, buyer_id: response.buyer_id, content: response.content, updated_at: response.updated_at };
};

// sendChatMessage 发送聊天文本消息。
export const sendChatMessage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** peer_user_id 表示会话对端标识。 */ peer_user_id: string; /** peer_name 表示会话对端名称。 */ peer_name?: string;
  /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** text 表示文本。 */ text: string;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> =>
  runContractRequest(/* signal 是本次聊天文本发送请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/messages', { body: input, signal }), options);

// sendChatImage 发送聊天图片消息。
export const sendChatImage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** peer_user_id 表示会话对端标识。 */ peer_user_id: string; /** peer_name 表示会话对端名称。 */ peer_name?: string;
  /** peer_avatar_url 表示会话对端头像地址。 */ peer_avatar_url?: string; /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** image 表示图片数据。 */ image: File;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> => {
	// form 保存聊天图片的原生 multipart 请求体，浏览器负责生成正确的 boundary。
	const form = new FormData();
	form.set('account_id', input.account_id);
	form.set('chat_id', input.chat_id);
	form.set('peer_user_id', input.peer_user_id);
	form.set('image', input.image);
	// optionalFields 保存可空展示字段；仅提交非空值，防止 FormData 将 undefined 字符串化后污染聊天元数据。
	const optionalFields = [
		['peer_name', input.peer_name],
		['peer_avatar_url', input.peer_avatar_url],
		['item_id', input.item_id],
		['item_title', input.item_title],
	] as const;
	for (const /* fieldName 和 fieldValue 分别表示可选表单字段名称及其当前用户可见元数据。 */ [fieldName, fieldValue] of optionalFields) {
		if (fieldValue) form.set(fieldName, fieldValue);
	}
	return runContractRequest(/* signal 是本次聊天图片发送请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/images', {
		// body 保持原生 FormData；共享契约层仅提供运行时类型适配，不定义业务传输 DTO。
		body: contractMultipartBody(form),
    signal,
  }), { timeoutMs: 120_000, ...options });
};

// sendChatLibraryImage 发送当前用户图片库中的图片；字节由服务端读取并复用手动上传相同的上传与宽高链路。
export const sendChatLibraryImage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** peer_user_id 表示会话对端标识。 */ peer_user_id: string; /** library_image_id 表示图片库图片主键。 */ library_image_id: number;
  /** peer_name 表示会话对端名称。 */ peer_name?: string; /** peer_avatar_url 表示会话对端头像地址。 */ peer_avatar_url?: string; /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> =>
	runContractRequest(/* signal 是本次图库图片发送请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/library-images', { body: input, signal }), { timeoutMs: 120_000, ...options });

/** 查询当前个人会话中对方或当前账号的在售商品。 */
export const getChatItems = async (accountId: string, chatId: string, role: 'peer' | 'self', query: string, page: number, options?: RequestControlOptions): Promise<ChatItemPage> => {
	// response 是 OpenAPI 契约客户端返回的商品分页 DTO。
	const response = await runContractRequest(/* signal 是本次商品查询的超时与取消信号。 */ signal => contractClient.GET('/api/v1/chat/items', {
		params: { query: { account_id: accountId, chat_id: chatId, role, query: query || undefined, page } },
		signal,
	}), options);
	return {
		items: response.items.map(/* item 是当前待转换的商品 transport DTO。 */ item => ({ item_id: item.item_id, title: item.title, image_url: item.image_url, price: item.price, description: item.description })),
		page: response.page,
		has_more: response.has_more,
	};
};

/** 将列表中选定的商品快照发送到当前个人会话。 */
export const sendChatItemCard = async (accountId: string, chatId: string, item: ChatItem, options?: RequestControlOptions): Promise<{ /** message 是本地状态机返回的出站商品消息。 */ message: ChatMessage }> =>
	runContractRequest(/* signal 是本次商品卡片发送的超时与取消信号。 */ signal => contractClient.POST('/api/v1/chat/item-cards', {
		body: { account_id: accountId, chat_id: chatId, item: { item_id: item.item_id, title: item.title, image_url: item.image_url, price: item.price } },
		signal,
	}), options);

/** 向平台确认指定会话中的入站消息已读。 */
export const markChatRead = async (accountId: string, chatId: string, messageIDs: ChatReadReceipt[], options?: RequestControlOptions): Promise<OperationResponse> =>
	runContractRequest(/* signal 是本次聊天已读上报请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/read', {
    body: { account_id: accountId, chat_id: chatId, message_ids: messageIDs.map(/* receipt 是当前待序列化的平台已读回执。 */ receipt => ({ messageId: receipt.messageId, sessionId: receipt.sessionId, cid: receipt.cid, conversationType: receipt.conversationType })) },
    signal,
  }), options);
