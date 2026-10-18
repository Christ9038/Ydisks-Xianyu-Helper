import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import type { RequestControlOptions } from '../../../shared/http/client';
import type { DeliveryTemplate, DeliveryTemplateDraft } from './types';
import { deliveryTemplatePayload } from './templateState';

/** normalizeTemplateMessage 仅兼容历史文本变量前缀，绝不解析图片路径或 URL 中的变量外观。 */
const normalizeTemplateMessage = (message: DeliveryTemplate['messages'][number]): DeliveryTemplate['messages'][number] => ({
  ...message,
  type: message.type || 'text',
  content: message.type === 'image' ? '' : message.content.replace(/\{\{delivery\./g, '{{'),
  image_url: message.image_url || '',
  image_path: message.image_path || '',
});

/** 查询当前用户的全部发货模板。 */
export const listDeliveryTemplates = async (options?: RequestControlOptions): Promise<DeliveryTemplate[]> => {
  // response 保存模板列表接口返回的业务数据。
  const response = await runContractRequest(/* signal 控制模板列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/delivery-templates', { signal }), options);
  return (response.data || []).map(/* item 是服务端返回的模板 DTO。 */ item => ({
    ...item,
    messages: item.messages.map(normalizeTemplateMessage),
    custom_keys: Array.isArray(item.custom_keys) ? item.custom_keys : [],
  })) as DeliveryTemplate[];
};

/** 创建一条发货模板。 */
export const createDeliveryTemplate = async (draft: DeliveryTemplateDraft, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制模板创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/delivery-templates', { body: deliveryTemplatePayload(draft), signal }), options);
};

/** 更新一条发货模板。 */
export const updateDeliveryTemplate = async (id: number, draft: DeliveryTemplateDraft, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制模板更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/delivery-templates/{template_id}', { params: { path: { template_id: String(id) } }, body: deliveryTemplatePayload(draft), signal }), options);
};

/** 删除一条未被规则引用的发货模板。 */
export const deleteDeliveryTemplate = async (id: number, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制模板删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/delivery-templates/{template_id}', { params: { path: { template_id: String(id) } }, signal }), options);
};
