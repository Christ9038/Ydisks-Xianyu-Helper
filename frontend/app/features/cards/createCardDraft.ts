import type { CardMutation } from './api';
import { apiConfigFromAddForm } from './editConfig';
import type { AddCardForm } from './types';

// emptyAddForm 创建新增卡密组表单的初始值。
export const emptyAddForm = (): AddCardForm => ({
  name: '',
  type: 'data',
  content: '',
  library_image_id: 0,
  description: '',
  enabled: true,
  delay_seconds: 0,
  api_method: 'GET',
  api_timeout: 10,
  api_headers: '',
  api_params: '',
  api_content_type: 'application/json',
  api_body: '',
  api_response_path: '',
  api_retry_enabled: false,
});

// validateAddForm 校验新增卡密表单；返回空串表示通过，否则返回可直接展示的原因。
export const validateAddForm = (form: AddCardForm): string => {
  if (!form.name.trim()) return '请输入卡密名称';
  if (!form.content.trim() && !(form.type === 'image' && form.library_image_id > 0)) {
    return form.type === 'api' ? '请输入 API 地址' : '请输入卡密内容';
  }
  return '';
};

// buildCreatePayload 把已通过校验的新增表单映射为创建接口载荷。
export const buildCreatePayload = (form: AddCardForm): CardMutation => {
  // payload 是创建卡密组的接口载荷，仅填充当前类型对应的内容字段。
  const payload: CardMutation = {
    name: form.name.trim(),
    type: form.type,
    description: form.description.trim(),
    enabled: form.enabled,
    delay_seconds: form.delay_seconds,
  };
  if (form.type === 'text') payload.text_content = form.content.trim();
  if (form.type === 'data') payload.data_content = form.content.trim();
  if (form.type === 'image') {
    payload.image_url = form.library_image_id ? '' : form.content.trim();
    payload.library_image_id = form.library_image_id;
  }
  if (form.type === 'api') payload.api_config = apiConfigFromAddForm(form);
  return payload;
};
