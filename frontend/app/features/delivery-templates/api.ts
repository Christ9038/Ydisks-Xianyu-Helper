import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import type { DeliveryTemplate, DeliveryTemplateDraft } from './types';

/** 查询当前用户的全部发货模板。 */
export const listDeliveryTemplates = async (): Promise<DeliveryTemplate[]> => {
  // response 保存模板列表接口返回的业务数据。
  const response = await runContractRequest(/* signal 控制模板列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/delivery-templates', { signal }));
  return (response.data || []).map(/* item 是服务端返回的模板 DTO。 */ item => ({
    ...item,
    messages: item.messages.map(/* message 是模板中的历史或当前消息正文。 */ message => ({ ...message, content: message.content.replace(/\{\{delivery\./g, '{{') })),
    custom_keys: Array.isArray(item.custom_keys) ? item.custom_keys : [],
  })) as DeliveryTemplate[];
};

/** 创建一条发货模板。 */
export const createDeliveryTemplate = async (draft: DeliveryTemplateDraft): Promise<void> => {
  await runContractRequest(/* signal 控制模板创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/delivery-templates', { body: draft, signal }));
};

/** 更新一条发货模板。 */
export const updateDeliveryTemplate = async (id: number, draft: DeliveryTemplateDraft): Promise<void> => {
  await runContractRequest(/* signal 控制模板更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/delivery-templates/{template_id}', { params: { path: { template_id: String(id) } }, body: draft, signal }));
};

/** 删除一条未被规则引用的发货模板。 */
export const deleteDeliveryTemplate = async (id: number): Promise<void> => {
  await runContractRequest(/* signal 控制模板删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/delivery-templates/{template_id}', { params: { path: { template_id: String(id) } }, signal }));
};
