/** chatRoutePath 是聊天页的规范地址；共享解析器不反向依赖 app 路由表。 */
const chatRoutePath = '/app/chat';

/** ChatDeepLink 描述通知深链要求打开的账号与会话，两个标识都来自浏览器查询参数。 */
export interface ChatDeepLink {
  /** accountID 是深链指定的闲鱼账号标识；只有该账号仍启用时聊天页才会优先选中它。 */
  accountID: string;
  /** chatID 是深链指定的会话标识；对应会话尚未加载时会等待后续会话更新再匹配。 */
  chatID: string;
}

/** chatDeepLinkFromLocation 从当前浏览器地址解析聊天深链；仅聊天页且账号、会话参数均非空时返回目标。 */
export const chatDeepLinkFromLocation = (): ChatDeepLink | undefined => {
  if (window.location.pathname !== chatRoutePath) return undefined;
  // params 保存浏览器已解码的查询参数；同名参数重复出现时 get 返回第一个值。
  const params = new URLSearchParams(window.location.search);
  // accountID 保存深链目标账号标识；空值或仅空白值视为无效目标而不改变聊天页选择。
  const accountID = (params.get('account') ?? '').trim();
  // chatID 保存深链目标会话标识；空值或仅空白值视为无效目标而不改变聊天页选择。
  const chatID = (params.get('chat') ?? '').trim();
  if (!accountID || !chatID) return undefined;
  return { accountID, chatID };
};
