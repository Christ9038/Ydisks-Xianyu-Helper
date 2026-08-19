import {
AccountDetail,
ChatMessage,
ChatSession,
OperationResponse
} from '../../../shared/api-contract/chat';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
export type * from '../../../shared/api-contract/chat';
import type { ChatReadReceipt } from './types';

/** ChatImageFormContract 描述图片发送 operation 的 multipart 字段，仅用于将原生 FormData 交给生成客户端。 */
type ChatImageFormContract = {
  /** 账号标识。 */ account_id: string;
  /** 会话标识。 */ chat_id: string;
  /** 买家标识。 */ buyer_id: string;
  /** 买家展示名称。 */ buyer_name?: string;
  /** 买家头像地址。 */ buyer_avatar_url?: string;
  /** 关联商品标识。 */ item_id?: string;
  /** 关联商品标题。 */ item_title?: string;
  /** 二进制图片字段。 */ image: string;
};

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
export interface ChatSessionPage { /** sessions 表示聊天会话列表。 */ sessions: ChatSession[]; /** has_more 表示是否存在更多数据。 */ has_more: boolean; /** next_cursor 表示下一页游标。 */ next_cursor?: number }

// getChatSessionPage 分页读取聊天会话。
export const getChatSessionPage = async (accountId: string, cursor?: number, options?: RequestControlOptions, refresh = false): Promise<ChatSessionPage> => {
	// result 接口响应结果，用于当前 API 处理流程。
	const response = await runContractRequest(
    /* signal 是本次聊天会话分页请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/chat/sessions', {
      params: { query: { account_id: accountId, cursor, refresh: refresh ? 1 : undefined } },
      signal,
    }),
		{ timeoutMs: refresh ? 60_000 : options?.timeoutMs, signal: options?.signal },
	);
	return response;
};

// getChatSessions 读取聊天会话列表。
export const getChatSessions = async (accountId: string, options?: RequestControlOptions): Promise<ChatSession[]> =>
	(await getChatSessionPage(accountId, undefined, options)).sessions;

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

// sendChatMessage 发送聊天文本消息。
export const sendChatMessage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** buyer_id 表示买家标识。 */ buyer_id: string; /** buyer_name 表示买家名称。 */ buyer_name?: string;
  /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** text 表示文本。 */ text: string;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> =>
  runContractRequest(/* signal 是本次聊天文本发送请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/messages', { body: input, signal }), options);

// sendChatImage 发送聊天图片消息。
export const sendChatImage = async (input: {
  /** account_id 表示账号标识。 */ account_id: string; /** chat_id 表示聊天标识。 */ chat_id: string; /** buyer_id 表示买家标识。 */ buyer_id: string; /** buyer_name 表示买家名称。 */ buyer_name?: string;
  /** buyer_avatar_url 表示买家头像地址。 */ buyer_avatar_url?: string; /** item_id 表示商品标识。 */ item_id?: string; /** item_title 表示商品标题。 */ item_title?: string; /** image 表示图片数据。 */ image: File;
}, options?: RequestControlOptions): Promise<{/** message 表示消息数据。 */ message: ChatMessage}> => {
	// form 消息表单，用于当前 API 处理流程。
	const form = new FormData();
	Object.entries(input).forEach(/* 当前回调用于处理集合元素或接口响应。 */ ([key, value]) => form.append(key, value));
	return runContractRequest(/* signal 是本次聊天图片发送请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/images', {
    // body 保持原生 FormData；类型转换只把 multipart 运行时载荷交给生成 operation。
    body: form as unknown as ChatImageFormContract,
    signal,
  }), { timeoutMs: 120_000, ...options });
};

/** 向平台确认指定会话中的入站消息已读。 */
export const markChatRead = async (accountId: string, chatId: string, messageIDs: ChatReadReceipt[], options?: RequestControlOptions): Promise<OperationResponse> =>
	runContractRequest(/* signal 是本次聊天已读上报请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/chat/read', {
    body: { account_id: accountId, chat_id: chatId, message_ids: messageIDs.map(/* receipt 是当前待序列化的平台已读回执。 */ receipt => ({ messageId: receipt.messageId, sessionId: receipt.sessionId, cid: receipt.cid, conversationType: receipt.conversationType })) },
    signal,
  }), options);
