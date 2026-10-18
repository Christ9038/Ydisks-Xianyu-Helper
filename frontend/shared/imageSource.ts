/** 图片引用草稿；显式来源用于保留尚未填写的本地输入状态。 */
export interface ImageSourceDraft {
  /** 图片来源；历史记录可由非空路径推断。 */
  image_source?: 'url' | 'local';
  /** 远程图片地址。 */
  image_url?: string;
  /** 实际发送账号目录中的相对路径。 */
  image_path?: string;
}

/** imageSource 推断历史图片配置，不将本地路径当成浏览器 URL。 */
export const imageSource = (draft: ImageSourceDraft): 'url' | 'local' => draft.image_source || (draft.image_path ? 'local' : 'url');

/** imageSourceFields 显式清空未选来源，避免服务端残留旧值。 */
export const imageSourceFields = (draft: ImageSourceDraft): {
  /** 选中远程来源时填写，否则显式清空旧 URL。 */
  image_url: string;
  /** 选中本地来源时填写，否则显式清空旧路径。 */
  image_path: string;
} => ({
  image_url: imageSource(draft) === 'url' ? (draft.image_url || '').trim() : '',
  image_path: imageSource(draft) === 'local' ? (draft.image_path || '').trim() : '',
});

/** validateImageSource 校验发货图片的静态来源；文件存在性和真实图片格式由服务端验证。 */
export const validateImageSource = (draft: ImageSourceDraft): string => {
  // fields 只检查当前会提交的来源，不被隐藏旧字段干扰。
  const fields = imageSourceFields(draft);
  if ((fields.image_url + fields.image_path).includes('{{') || (fields.image_url + fields.image_path).includes('}}')) return '图片来源不支持模板变量';
  if (imageSource(draft) === 'url') return /^https?:\/\/\S+$/i.test(fields.image_url) ? '' : '请填写有效的 HTTP(S) 图片 URL';
  // segments 拒绝绝对路径、空分段和目录穿越，统一使用正斜杠。
  const segments = fields.image_path.split('/');
  if (!fields.image_path || /[\\:\x00-\x1f]/.test(fields.image_path) || segments.some(/* segment 是目录中的单级名称。 */ segment => !segment || segment === '.' || segment === '..')) {
    return '请填写图片目录内的相对路径，使用 / 分隔，不能包含 ..、绝对路径或反斜杠';
  }
  return '';
};
