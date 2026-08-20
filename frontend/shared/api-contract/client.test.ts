import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';

import { afterEach, expect, test } from 'vitest';

import { contractFetch } from './client';

// multipartRecord 保存本地 HTTP 服务实际收到的请求路径、声明边界和原始表单字节，防止测试替身掩盖浏览器传输差异。
type multipartRecord = {
  // path 是当前 multipart 调用到达的版本化 API 路径。
  path: string;
  // contentType 是客户端实际发送的 multipart Content-Type 响应头。
  contentType: string;
  // rawBody 是服务端未解析前读取的完整请求字节。
  rawBody: Buffer;
};

// multipartServer 是当前测试启动的本地 HTTP 服务；afterEach 必须关闭它避免占用测试进程端口。
let multipartServer: Server | undefined;

afterEach(async () => {
  // server 保存当前测试可能仍在监听的本地 HTTP 服务。
  const server = multipartServer;
  multipartServer = undefined;
  if (server) {
    await new Promise<void>(/* resolve 在服务停止并释放端口后继续测试清理。 */ resolve => server.close(() => resolve()));
  }
} /* 测试回调确保 multipart 本地 HTTP 服务不会泄漏到其他测试。 */);

test('contractFetch keeps multipart boundary, file bytes, filenames, and repeated image fields over real HTTP', async () => {
  // records 收集服务端从原始网络请求读取的 multipart 内容，用于校验声明 boundary 与请求体一致。
  const records: multipartRecord[] = [];
  // server 模拟浏览器实际请求到达的 HTTP 服务，故意不通过 fetch mock 重建 FormData。
  const server = createServer(
    // HTTP 回调按原始字节采集契约客户端的网络请求，再返回空成功响应。
    (request, response) => {
    // chunks 以字节块保存当前请求体，避免 UTF-8 转换损坏二进制文件内容。
    const chunks: Buffer[] = [];
    request.on('data', /* chunk 是当前网络读取到的原始请求字节。 */ chunk => chunks.push(Buffer.from(chunk)));
    request.on('end',
      // end 回调在请求体读取完成后记录原始 multipart 字节。
      () => {
      // rawBody 合并当前请求的全部原始字节，后续断言直接检查 multipart framing。
      const rawBody = Buffer.concat(chunks);
      records.push({
        path: request.url || '',
        contentType: request.headers['content-type'] || '',
        rawBody,
      });
      response.writeHead(204);
      response.end();
      },
    );
  },
  );
  multipartServer = server;
  await new Promise<void>(/* resolve 在操作系统分配 loopback 端口后继续发送请求。 */ resolve => server.listen(0, '127.0.0.1', () => resolve()));
  // address 是本地服务监听地址，端口由操作系统动态分配以支持测试并发运行。
  const address = server.address() as AddressInfo;
  // paths 覆盖商品发布、批量预检、卡密导入、聊天图片、订单导入和旧版订单刷新这六条 multipart 传输路径。
  const paths = [
    '/api/v1/items/publish',
    '/api/v1/items/publish-batches/preview',
    '/api/v1/cards/batch',
    '/api/v1/chat/images',
    '/api/v1/orders/import',
    '/api/v1/orders/refresh',
  ];

  for (const /* path 是当前覆盖的 multipart 版本化 API 路径。 */ path of paths) {
    // form 是调用方构造的原生 FormData，必须由 contractFetch 连同浏览器生成的 boundary 原样发送。
    const form = new FormData();
    form.append('title', '测试商品');
    form.append('images', new Blob(['image-one'], { type: 'image/png' }), 'first image.png');
    form.append('images', new Blob(['image-two'], { type: 'image/png' }), 'second-image.png');
    // request 是 openapi-fetch 最终会交给共享 fetch 的请求形态；此处直接覆盖其网络层行为。
    const request = new Request(`http://127.0.0.1:${address.port}${path}`, { method: 'POST', body: form });
    // response 证明请求已通过实际 HTTP 往返，而非仅检查客户端构造参数。
    const response = await contractFetch(request);
    expect(response.status).toBe(204);
  }

  expect(records).toHaveLength(paths.length);
  for (const /* record 是当前服务端记录的 multipart 原始请求。 */ record of records) {
    // boundaryMatch 提取请求头声明的 boundary；缺失时说明浏览器或共享客户端错误覆盖了 Content-Type。
    const boundaryMatch = /boundary=(?:"([^"]+)"|([^;\s]+))/.exec(record.contentType);
    expect(record.contentType).toContain('multipart/form-data');
    expect(boundaryMatch).not.toBeNull();
    // boundary 是 multipart framing 必须使用的分隔符，原始 body 的首段必须包含它。
    const boundary = boundaryMatch?.[1] || boundaryMatch?.[2] || '';
    // rawText 使用 latin1 保留任意二进制字节的一对一映射，适合检查 multipart header、文件名和示例内容。
    const rawText = record.rawBody.toString('latin1');
    expect(rawText).toContain(`--${boundary}`);
    expect(rawText).toContain('filename="first image.png"');
    expect(rawText).toContain('filename="second-image.png"');
    expect((rawText.match(/name="images"/g) || []).length).toBe(2);
    expect(rawText).toContain('image-one');
    expect(rawText).toContain('image-two');
  }
} /* 真实 HTTP 回归验证：共享客户端绝不重建 FormData 或保留过期 boundary。 */);
