import {
Card,
CardAppendResponse,
CardBatchResponse,
MutationIDResponse,OperationResponse
} from '../../../shared/api-contract/cards';
import { type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { collectionFrom } from '../../../shared/http/contract';
export type * from '../../../shared/api-contract/cards';
// Cards
// normalizeCard 归一化卡密数据。
const normalizeCard = (item: any): Card => {
  // apiConfig 卡密接口配置，用于当前 API 处理流程。
  let apiConfig = item.api_config;
  if (typeof apiConfig === 'string' && apiConfig.trim()) {
    try {
      apiConfig = JSON.parse(apiConfig);
    } catch {
      apiConfig = undefined;
    }
  }
  return {...item, api_config: apiConfig || undefined} as Card;
};

// cardPayload 卡密请求载荷，用于当前 API 处理流程。
const cardPayload = (data: Partial<Card>): Record<string, unknown> => ({
  ...data,
  api_config: data.api_config ? JSON.stringify(data.api_config) : '',
});

// getCards 读取卡密列表。
export const getCards = async (options?: RequestControlOptions): Promise<Card[]> => {
  // res 接口响应结果，用于当前 API 处理流程。
  const res = await runContractRequest(/* signal 控制卡券列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/cards', { signal }), options) as unknown;
  // cards 卡密列表，用于当前 API 处理流程。
  const cards = collectionFrom<Card>(res, ['cards', 'data', 'items']);
  return cards.map(normalizeCard);
};

// createCard 创建卡密组。
export const createCard = async (data: Partial<Card>): Promise<MutationIDResponse> => {
  return runContractRequest(/* signal 控制卡券创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/cards', { body: cardPayload(data) as never, signal })) as unknown as Promise<MutationIDResponse>;
};

// updateCard 更新卡密组。
export const updateCard = async (cardId: string | number, data: Partial<Card>): Promise<OperationResponse> => {
  return runContractRequest(/* signal 控制卡券更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/cards/{card_id}', { params: { path: { card_id: String(cardId) } }, body: cardPayload(data) as never, signal }));
};

// deleteCard 删除卡密组。
export const deleteCard = async (cardId: string | number): Promise<OperationResponse> => {
  return runContractRequest(/* signal 控制卡券删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/cards/{card_id}', { params: { path: { card_id: String(cardId) } }, signal }));
};

// getCardDetails 读取卡密组详情。
export const getCardDetails = async (cardId: string | number): Promise<Card> => {
  // card 卡密详情，用于当前 API 处理流程。
  const card = await runContractRequest(/* signal 控制卡券详情请求的取消和超时。 */ signal => contractClient.GET('/api/v1/cards/{card_id}/details', { params: { path: { card_id: String(cardId) } }, signal })) as unknown as Card;
  return normalizeCard(card);
};

// 批量创建卡密组（上传表格）
export const batchCreateCards = async (file: File, options?: RequestControlOptions): Promise<CardBatchResponse> => {
  // 批量创建接口返回总行数、成功数、失败数和逐行结果。
  // CardBatchResponse 保留旧字段名称，调用方无需转换统计字段。
  // rows 中的 id 只在创建成功时返回。
  // rows 中的 error 只在对应行失败时返回。
  // 表单上传方式和接口路径保持不变。
  // 此处只收紧 TypeScript 响应契约。
  const body = new FormData();
  body.append('file', file);
  return runContractRequest(/* signal 控制卡券批量上传的取消和超时。 */ signal => contractClient.POST('/api/v1/cards/batch', { body: body as never, signal }), options) as Promise<CardBatchResponse>;
};

// 往 data 类型卡密组批量追加卡密号
export const appendCardData = async (cardId: string | number, content: string, options?: RequestControlOptions): Promise<CardAppendResponse> => {
  return runContractRequest(/* signal 控制卡券追加数据请求的取消和超时。 */ signal => contractClient.POST('/api/v1/cards/{card_id}/append-data', { params: { path: { card_id: String(cardId) } }, body: { content } as never, signal }), options) as Promise<CardAppendResponse>;
};
