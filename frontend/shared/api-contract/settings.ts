// settings 只公开 OpenAPI 生成的设置传输类型；表单状态属于 settings feature。
import type { components } from './generated/schema';

/** SystemSettingsTransport 表示受约束动态键的设置响应。 */
export type SystemSettingsTransport = components['schemas']['SystemSettingsResponse'];
/** AIModelsTransport 表示生成的模型发现响应。 */
export type AIModelsTransport = components['schemas']['AIModelsResponse'];
