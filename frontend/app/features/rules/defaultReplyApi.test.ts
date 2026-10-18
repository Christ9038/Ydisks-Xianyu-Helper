import { afterEach,describe,expect,test,vi } from 'vitest';
import { deleteItemDefaultReply,getDefaultReply,getItemDefaultReplies,getItemDefaultReply,updateDefaultReply,updateItemDefaultReply } from './api';

/** replyFetch 用真实 Request 捕获 adapter 生成的路径和 JSON，不绕过共享契约客户端。 */
const replyFetch = (response: unknown) => {
  /** requests 保存每次请求的克隆，允许测试读取请求体而不消费客户端对象。 */
  const requests: Request[] = [];
  vi.stubGlobal('fetch', vi.fn(/* input/init 是共享契约客户端发出的真实 HTTP 请求。 */ async (input: RequestInfo | URL, init?: RequestInit) => {
    /** request 是本次捕获的 HTTP 请求。 */
    const request = input instanceof Request ? input : new Request(input, init);
    requests.push(request.clone());
    return new Response(JSON.stringify(response), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }));
  return requests;
};

afterEach(/* 当前回调恢复全局网络入口。 */ () => vi.unstubAllGlobals());

describe('默认回复 API adapter', /* 当前回调验证具名契约与 UI 归一，不触达真实服务。 */ () => {
  test('旧账号配置缺少本地图片字段时回填空值，保留 URL', /* 当前回调验证旧配置仍可直接显示编辑。 */ async () => {
    replyFetch({ enabled: true, reply_content: '旧回复', reply_once: true, reply_image_url: 'https://image.test/a.jpg' });
    expect(await getDefaultReply('a')).toEqual({ cookie_id: 'a', enabled: true, reply_content: '旧回复', reply_once: true, reply_image_url: 'https://image.test/a.jpg', reply_image_path: '' });
  });

  test('账号保存显式提供两类图片字段，移除本地图片不会遗留旧路径', /* 当前回调检查真实序列化请求，不依赖源码字符串。 */ async () => {
    /** requests 是本轮记录的账号写入。 */
    const requests = replyFetch({ success: true });
    await updateDefaultReply('account a', { enabled: true, reply_content: '欢迎', reply_image_path: 'a.jpg' });
    expect(new URL(requests[0].url).pathname).toBe('/api/v1/default-replies/account%20a');
    expect(await requests[0].json()).toEqual({ enabled: true, reply_content: '欢迎', reply_once: false, reply_image_url: '', reply_image_path: 'a.jpg' });
    await updateDefaultReply('account a', { reply_image_url: 'https://image.test/a.jpg' });
    expect(await requests[1].json()).toEqual(expect.objectContaining({ reply_image_url: 'https://image.test/a.jpg', reply_image_path: '' }));
  });

  test('商品列表和单项兼容历史纯文字以及未配置空正文', /* 当前回调覆盖列表和单项的账号商品字段回填。 */ async () => {
    replyFetch([{ cookie_id: 'a', item_id: 'one', reply_content: '商品' }]);
    expect(await getItemDefaultReplies()).toEqual([{ cookie_id: 'a', item_id: 'one', reply_content: '商品', reply_image_url: '', reply_image_path: '', reply_once: false }]);
    replyFetch({ reply_content: '' });
    expect(await getItemDefaultReply('b', 'two')).toEqual({ cookie_id: 'b', item_id: 'two', reply_content: '', reply_image_url: '', reply_image_path: '', reply_once: false });
  });

  test('商品独立只回复一次可读写，显式关闭不会被缺省保留吞掉', /* 当前回调检查开关来自商品DTO而非账号配置，并验证请求布尔值。 */ async () => {
    replyFetch({ reply_content: '商品', reply_once: true });
    expect((await getItemDefaultReply('a', 'one')).reply_once).toBe(true);
    // requests 收集开启与关闭商品去重的两个独立写入请求。
    const requests = replyFetch({ success: true });
    await updateItemDefaultReply('a', 'one', { reply_content: '商品', reply_image_url: '', reply_image_path: '', reply_once: true });
    await updateItemDefaultReply('a', 'one', { reply_content: '商品', reply_image_url: '', reply_image_path: '', reply_once: false });
    expect(await requests[0].json()).toEqual({ reply_content: '商品', reply_image_url: '', reply_image_path: '', reply_once: true });
    expect(await requests[1].json()).toEqual({ reply_content: '商品', reply_image_url: '', reply_image_path: '', reply_once: false });
  });

  test('商品保存只发送图文字段，删除使用同账号同商品路由', /* 当前回调验证不会把账号开关误发到商品配置。 */ async () => {
    /** requests 记录商品写入和删除的真实请求。 */
    const requests = replyFetch({ success: true });
    await updateItemDefaultReply('a', 'one', { reply_content: '商品', reply_image_url: '', reply_image_path: '商品/图.jpg' });
    expect(new URL(requests[0].url).pathname).toBe('/api/v1/reply-rules/items/a/one');
    expect(requests[0].method).toBe('PUT');
    expect(await requests[0].json()).toEqual({ reply_content: '商品', reply_image_url: '', reply_image_path: '商品/图.jpg' });
    await deleteItemDefaultReply('a', 'one');
    expect(new URL(requests[1].url).pathname).toBe('/api/v1/reply-rules/items/a/one');
    expect(requests[1].method).toBe('DELETE');
  });
});
