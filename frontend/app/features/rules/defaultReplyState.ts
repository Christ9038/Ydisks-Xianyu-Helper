import type { DefaultReply,ItemDefaultReply } from './models';
import type { DefaultReplyForm } from './types';

/** defaultReplyImageSource 从旧配置推断图片来源，保留显式选中但尚未填写的表单状态。 */
export const defaultReplyImageSource = (form: Pick<DefaultReplyForm, 'image_source' | 'reply_image_url' | 'reply_image_path'>): 'none' | 'url' | 'local' =>
  form.image_source || (form.reply_image_path ? 'local' : form.reply_image_url ? 'url' : 'none');

/** createDefaultReplyForm 为账号或商品新建独立草稿，避免切换范围时携带上一份正文和图片。 */
export const createDefaultReplyForm = (cookieID: string, itemID = '', scope: 'account' | 'item' = 'account', reply?: Partial<DefaultReply & ItemDefaultReply>): DefaultReplyForm => ({
  cookie_id: cookieID,
  item_id: itemID,
  scope,
  enabled: reply?.enabled ?? false,
  reply_once: reply?.reply_once ?? false,
  reply_content: reply?.reply_content || '',
  reply_image_url: reply?.reply_image_url || '',
  reply_image_path: reply?.reply_image_path || '',
  image_source: reply?.reply_image_path ? 'local' : reply?.reply_image_url ? 'url' : 'none',
});

/** defaultReplyImageFields 仅提交当前图片来源，同时清空另一个字段，避免服务端残留旧来源。 */
export const defaultReplyImageFields = (form: DefaultReplyForm): Pick<ItemDefaultReply, 'reply_image_url' | 'reply_image_path'> => ({
  reply_image_url: defaultReplyImageSource(form) === 'url' ? form.reply_image_url.trim() : '',
  reply_image_path: defaultReplyImageSource(form) === 'local' ? (form.reply_image_path || '').trim() : '',
});

/** validateDefaultReplyForm 返回可展示的校验错误；商品空配置允许继承账号，账号启用时必须有内容。 */
export const validateDefaultReplyForm = (form: DefaultReplyForm): string => {
  if (!form.cookie_id) return '请先选择账号';
  if (form.scope === 'item' && !form.item_id) return '请选择关联商品';
  // source 是用户当前选择的图片来源，决定哪个输入需要校验。
  const source = defaultReplyImageSource(form);
  // images 只包含本次实际提交的图片字段，隐藏的历史字段不会影响校验。
  const images = defaultReplyImageFields(form);
  if (source === 'url' && !/^https?:\/\/\S+$/i.test(images.reply_image_url)) return '请填写有效的 HTTP(S) 图片 URL';
  if (source === 'local') {
    // segments 是专用目录内的路径各级名称，不接受绝对路径或目录穿越。
    const segments = images.reply_image_path.split('/');
    if (!images.reply_image_path || /[\\:\x00-\x1f]/.test(images.reply_image_path) || segments.some(/* segment 是一个相对路径分段。 */ segment => !segment || segment === '.' || segment === '..')) {
      return '请填写图片目录内的相对路径，使用 / 分隔，不能包含 ..、绝对路径或反斜杠';
    }
  }
  if (form.scope !== 'item' && form.enabled && !form.reply_content.trim() && !images.reply_image_url && !images.reply_image_path) return '启用默认回复时，请填写回复内容或图片';
  return '';
};
