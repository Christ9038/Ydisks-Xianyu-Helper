// cards 只公开 OpenAPI 生成的卡券传输类型；卡券展示模型属于 cards feature。
import type { components } from './generated/schema';

/** CardTransport 表示生成的卡券响应。 */
export type CardTransport = components['schemas']['CardResponse'];
