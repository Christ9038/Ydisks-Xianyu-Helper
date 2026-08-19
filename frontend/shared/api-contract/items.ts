// items 只公开 OpenAPI 生成的商品传输类型；发布和兼容模型属于 items feature。
import type { components } from './generated/schema';

/** ItemTransport 表示生成的商品列表行。 */
export type ItemTransport = components['schemas']['ItemListResponse'];
