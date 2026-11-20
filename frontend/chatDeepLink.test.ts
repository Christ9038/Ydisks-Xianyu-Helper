import { afterEach,describe,expect,test } from 'vitest';
import { chatDeepLinkFromLocation } from './app/router/routes';

/** setTestLocation 用给定路径与查询替换全局 window，模拟浏览器地址而不依赖真实导航。 */
const setTestLocation = (pathname: string, search: string): void => {
  Object.defineProperty(globalThis, 'window', { value: { location: { pathname, search } }, configurable: true });
};

/** resetWindow 清理每个用例替换的全局 window，避免深链解析结果跨用例泄漏。 */
afterEach(/* resetAction 在下一个用例开始前移除模拟浏览器地址。 */ () => {
  Reflect.deleteProperty(globalThis, 'window');
});

describe('chatDeepLinkFromLocation', /* 当前回调汇总聊天通知深链的纯解析契约。 */ () => {
  test('解析聊天页的账号与会话参数', /* 当前回调验证有效深链返回两个非空标识。 */ () => {
    setTestLocation('/app/chat', '?account=account-1&chat=chat-2');
    expect(chatDeepLinkFromLocation()).toEqual({ accountID: 'account-1', chatID: 'chat-2' });
  });

  test('缺少任一参数或参数为空时返回 undefined', /* 当前回调验证不完整深链不会改变聊天页选择。 */ () => {
    setTestLocation('/app/chat', '?account=account-1');
    expect(chatDeepLinkFromLocation()).toBeUndefined();
    setTestLocation('/app/chat', '?chat=chat-2');
    expect(chatDeepLinkFromLocation()).toBeUndefined();
    setTestLocation('/app/chat', '?account=&chat=chat-2');
    expect(chatDeepLinkFromLocation()).toBeUndefined();
    setTestLocation('/app/chat', '?account=%20&chat=chat-2');
    expect(chatDeepLinkFromLocation()).toBeUndefined();
  });

  test('非聊天页地址即使带参数也返回 undefined', /* 当前回调验证深链只对规范聊天路由生效。 */ () => {
    setTestLocation('/app/dashboard', '?account=account-1&chat=chat-2');
    expect(chatDeepLinkFromLocation()).toBeUndefined();
    setTestLocation('/app/chat/extra', '?account=account-1&chat=chat-2');
    expect(chatDeepLinkFromLocation()).toBeUndefined();
  });

  test('URL 编码的账号与会话值由 URLSearchParams 解码', /* 当前回调验证特殊字符不会原样进入聊天选择状态。 */ () => {
    setTestLocation('/app/chat', '?account=account%2D1&chat=a%20b%26c');
    expect(chatDeepLinkFromLocation()).toEqual({ accountID: 'account-1', chatID: 'a b&c' });
  });
});
