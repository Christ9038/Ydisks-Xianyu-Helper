// library 只公开 OpenAPI 生成的本地资源库传输类型；UI 模型属于 library feature。
import type { components } from './generated/schema';

/** LibraryImageTransport 表示生成的图片库图片。 */
export type LibraryImageTransport = components['schemas']['LibraryImage'];
/** LibraryTemplateTransport 表示生成的商品模板响应。 */
export type LibraryTemplateTransport = components['schemas']['LibraryTemplate'];
/** LibraryTemplateRequestTransport 表示生成的商品模板请求。 */
export type LibraryTemplateRequestTransport = components['schemas']['LibraryTemplateRequest'];
/** LibraryActionTransport 表示生成的模板自动化动作。 */
export type LibraryActionTransport = components['schemas']['LibraryTemplateAction'];
/** LibraryActionGroupTransport 表示生成的模板自动化动作组。 */
export type LibraryActionGroupTransport = components['schemas']['LibraryTemplateCardAutomation'];
/** LibraryAutomationTransport 表示生成的模板自动化配置。 */
export type LibraryAutomationTransport = components['schemas']['LibraryTemplateAutomation'];
/** CardTransportForLibrary 表示自动发货可选卡密所需的生成卡券响应。 */
export type CardTransportForLibrary = components['schemas']['CardResponse'];
/** LibraryImportPreviewTransport 表示生成的 Excel 导入预检响应。 */
export type LibraryImportPreviewTransport = components['schemas']['LibraryTemplateImportPreviewResponse'];
/** LibraryImportProgressTransport 表示生成的 Excel 导入分段提交进度响应。 */
export type LibraryImportProgressTransport = components['schemas']['LibraryTemplateImportProgressResponse'];
