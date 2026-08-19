// common 只公开跨 feature 的 OpenAPI 生成传输类型。
import type { components } from './generated/schema';

/** ApiErrorResponse 表示统一错误 envelope，供共享 HTTP 运行时识别错误。 */
export type ApiErrorResponse = components['schemas']['ErrorResponse'];
