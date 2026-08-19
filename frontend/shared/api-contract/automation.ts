// automation 只公开 OpenAPI 生成的自动化传输类型；规则编辑模型属于 rules feature。
import type { components } from './generated/schema';

/** OperationTransport 表示生成的通用成功响应。 */
export type OperationTransport = components['schemas']['OperationResponse'];
