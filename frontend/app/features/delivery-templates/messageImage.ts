// imageTokenPattern 匹配整条消息就是一个图片库图片令牌的写法，与服务端 deliverytemplate.ImageToken 保持一致。
const imageTokenPattern = /^\{\{library_image:([1-9][0-9]*)\}\}$/;

// imageToken 把图片库图片主键序列化为模板消息正文，服务端据此识别图片消息。
export const imageToken = (imageId: number): string => `{{library_image:${imageId}}}`;

// parseImageToken 返回图片消息引用的图片库主键；普通文本消息返回 0。
export const parseImageToken = (content: string): number => {
  // match 是整条消息与图片令牌格式的匹配结果。
  const match = imageTokenPattern.exec(content.trim());
  return match ? Number(match[1]) : 0;
};
