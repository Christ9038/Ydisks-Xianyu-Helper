import { imageSourceFields, validateImageSource } from '../../../shared/imageSource';
import type { DeliveryTemplateDraft, DeliveryTemplateMessageDraft } from './types';

/** nextMessageKey 只分配当前浏览器进程内的草稿身份，不用作数据库主键或发送顺序。 */
let nextMessageKey = 0;

/** emptyTemplateMessage 每次创建独立消息与稳定编辑身份，类型切换时不携带旧正文或来源。 */
export const emptyTemplateMessage = (type: 'text' | 'image' = 'text'): DeliveryTemplateMessageDraft => ({ editor_key: ++nextMessageKey, type, content: '', image_url: '', image_path: '' });

/** deliveryTemplatePayload 显式提交类型和两来源字段；图片内容不进入变量兼容转换。 */
export const deliveryTemplatePayload = (draft: DeliveryTemplateDraft): DeliveryTemplateDraft => ({
  name: draft.name.trim(),
  enabled: draft.enabled,
  messages: draft.messages.map(/* message 按原有顺序映射为接口消息，不发送 UI 来源标记。 */ message => message.type === 'image'
    ? { type: 'image', content: '', ...imageSourceFields(message) }
    : { type: 'text', content: message.content.trim(), image_url: '', image_path: '' }),
});

/** validateTemplateImages 不丢弃空图片条目；选中图片就必须提供一个有效来源。 */
export const validateTemplateImages = (messages: DeliveryTemplateMessageDraft[]): string => {
  for (let /* index 是图片在发送计划中的位置，用于定位校验错误。 */ index = 0; index < messages.length; index += 1) {
    if (messages[index].type !== 'image') continue;
    // error 是当前顺序图片消息的输入错误。
    const error = validateImageSource(messages[index]);
    if (error) return `第 ${index + 1} 条消息：${error}`;
  }
  return '';
};
