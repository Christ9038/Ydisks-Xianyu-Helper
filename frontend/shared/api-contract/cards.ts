// cards 只公开 OpenAPI 生成的卡券传输类型；卡券展示模型属于 cards feature。
import type { components } from './generated/schema';

/** CardTransport 表示生成的卡券响应。 */
export type CardTransport = components['schemas']['CardResponse'];

/** CardSaleRecordTransport 表示生成的卡密售出记录。 */
export type CardSaleRecordTransport = components['schemas']['CardSaleRecordResponse'];
/** CardSaleRecordListTransport 表示生成的售出记录分页响应。 */
export type CardSaleRecordListTransport = components['schemas']['CardSaleRecordListResponse'];
