import {
Card,
CardAppendResponse,
CardBatchResponse,
	CardMutation,
	CardAPITestResult,
	CardAPIConfigInput,
MutationIDResponse,OperationResponse
} from './models';
import { type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, contractMultipartBody, runContractRequest } from '../../../shared/api-contract/client';
import { collectionFrom } from '../../../shared/http/contract';
export type * from './models';
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
  if (apiConfig && typeof apiConfig === 'object' && apiConfig.timeout_seconds === undefined && apiConfig.timeout !== undefined) {
    // normalizedTimeout 将历史 timeout 字段迁移到新版摘要字段。
    apiConfig.timeout_seconds = Number(apiConfig.timeout) || 10;
  }
  if (apiConfig && typeof apiConfig === 'object' && !apiConfig.content_type) {
    apiConfig.content_type = 'application/json';
  }
  return {...item, api_config: apiConfig || undefined} as Card;
};

// parseTemplateJSON 将表单中的 JSON 模板转换为请求对象，空模板交给服务端执行保留语义。
const parseTemplateJSON = (value: unknown): Record<string, unknown> | undefined => {
  if (value && typeof value === 'object' && !Array.isArray(value)) return value as Record<string, unknown>;
  if (typeof value !== 'string' || !value.trim()) return undefined;
  // parsed 保存表单 JSON 文本解析后的未知值，随后只接受对象。
  const parsed: unknown = JSON.parse(value);
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('请求头和请求参数必须是 JSON 对象');
  return parsed as Record<string, unknown>;
};

// cardPayload 将 feature 表单映射为新版具名 API 请求载荷，并兼容旧字符串配置。
const cardPayload = (data: CardMutation) => ({
  name: data.name,
  type: data.type,
  api_config: typeof data.api_config === 'string' ? data.api_config : data.api_config ? {
    ...data.api_config,
    headers: parseTemplateJSON(data.api_config.headers),
    params: parseTemplateJSON(data.api_config.params),
    body: parseTemplateJSON(data.api_config.body),
    content_type: data.api_config.content_type || 'application/json',
  } : undefined,
  text_content: data.text_content,
  data_content: data.data_content,
  image_url: data.image_url,
  description: data.description,
  enabled: data.enabled,
  delay_seconds: data.delay_seconds,
  is_multi_spec: data.is_multi_spec,
  spec_name: data.spec_name,
  spec_value: data.spec_value,
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
export const createCard = async (data: CardMutation): Promise<MutationIDResponse> => {
  return runContractRequest(/* signal 控制卡券创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/cards', { body: cardPayload(data), signal }));
};

// updateCard 更新卡密组。
export const updateCard = async (cardId: string | number, data: CardMutation): Promise<OperationResponse> => {
  return runContractRequest(/* signal 控制卡券更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/cards/{card_id}', { params: { path: { card_id: String(cardId) } }, body: cardPayload(data), signal }));
};

// deleteCard 删除卡密组。
export const deleteCard = async (cardId: string | number): Promise<OperationResponse> => {
  return runContractRequest(/* signal 控制卡券删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/cards/{card_id}', { params: { path: { card_id: String(cardId) } }, signal }));
};

// getCardDetails 读取卡密组详情。
export const getCardDetails = async (cardId: string | number): Promise<Card> => {
  // card 卡密详情，用于当前 API 处理流程。
  const card = await runContractRequest(/* signal 控制卡券详情请求的取消和超时。 */ signal => contractClient.GET('/api/v1/cards/{card_id}/details', { params: { path: { card_id: String(cardId) } }, signal }));
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
  return runContractRequest(/* signal 控制卡券批量上传的取消和超时。 */ signal => contractClient.POST('/api/v1/cards/batch', { body: contractMultipartBody(body), signal }), options);
};

// 往 data 类型卡密组批量追加卡密号
export const appendCardData = async (cardId: string | number, content: string, options?: RequestControlOptions): Promise<CardAppendResponse> => {
  return runContractRequest(/* signal 控制卡券追加数据请求的取消和超时。 */ signal => contractClient.POST('/api/v1/cards/{card_id}/append-data', { params: { path: { card_id: String(cardId) } }, body: { content }, signal }), options);
};

// testCardAPI 发送一次临时 API 配置测试请求，不保存卡密配置。
export const testCardAPI = async (data: CardAPIConfigInput): Promise<CardAPITestResult> => {
	return runContractRequest(/* signal 控制 API 测试请求的取消和超时。 */ signal => contractClient.POST('/api/v1/cards/test-api', {
		body: { api_config: cardPayload({ name: '', type: 'api', api_config: data }).api_config } as never,
		signal,
	}));
};
