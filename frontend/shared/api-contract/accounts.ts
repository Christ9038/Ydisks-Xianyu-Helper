// accounts 只公开 OpenAPI 生成的账号传输类型；UI 模型属于 accounts feature。
import type { components } from './generated/schema';

/** AccountTransport 表示生成的非敏感账号响应，adapter 负责转换为页面模型。 */
export type AccountTransport = components['schemas']['AccountDetailResponse'];
/** QRLoginStatusTransport 表示生成的二维码风控状态响应。 */
export type QRLoginStatusTransport = components['schemas']['QRLoginStatusResponse'];
