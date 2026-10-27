// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { downloadBlob, filenameFromContentDisposition } from './download';

// 套件：从 Content-Disposition 解析下载文件名。
describe('filenameFromContentDisposition', () => {
  // 用例：优先解析 RFC 5987 编码的中文文件名。
  it('优先解析 RFC 5987 编码的中文文件名', () => {
    expect(filenameFromContentDisposition("attachment; filename*=utf-8''%E7%BB%84A-%E6%9C%AA%E5%94%AE.txt", 'x.txt')).toBe('组A-未售.txt');
  });

  // 用例：编码损坏时退回普通 filename，仍失败则用兜底名。
  it('编码损坏时退回普通 filename，仍失败则用兜底名', () => {
    expect(filenameFromContentDisposition('attachment; filename*=utf-8\'\'%E0%A4%A; filename="plain.csv"', 'x.csv')).toBe('plain.csv');
    expect(filenameFromContentDisposition("attachment; filename*=utf-8''%E0%A4%A", 'fallback.csv')).toBe('fallback.csv');
  });

  // 用例：解析带引号、转义引号和不带引号的 filename。
  it('解析带引号、转义引号和不带引号的 filename', () => {
    expect(filenameFromContentDisposition('attachment; filename="a b.csv"', 'x')).toBe('a b.csv');
    expect(filenameFromContentDisposition('attachment; filename="a\\"b.csv"', 'x')).toBe('a"b.csv');
    expect(filenameFromContentDisposition('attachment; filename=plain.txt', 'x')).toBe('plain.txt');
  });

  // 用例：缺少响应头或文件名时返回兜底名。
  it('缺少响应头或文件名时返回兜底名', /* 兜底名用例回调。 */ () => {
    expect(filenameFromContentDisposition(null, 'fb')).toBe('fb');
    expect(filenameFromContentDisposition(undefined, 'fb')).toBe('fb');
    expect(filenameFromContentDisposition('attachment', 'fb')).toBe('fb');
    expect(filenameFromContentDisposition('attachment; filename=""', 'fb')).toBe('fb');
  });
});

// 套件：浏览器端触发 Blob 下载。
describe('downloadBlob', /* downloadBlob 套件回调。 */ () => {
  // 每个用例结束后还原被替换的全局对象与 spy。
  afterEach(() => {
    vi.restoreAllMocks();
  });

  // 用例：创建临时链接触发下载并释放对象地址。
  it('创建临时链接触发下载并释放对象地址', /* 临时链接下载用例回调。 */ () => {
    // createUrl 与 revokeUrl 替换 jsdom 缺失的对象地址 API。
    // createUrl 是 URL.createObjectURL 的替身，返回固定地址。
    const createUrl = vi.fn(() => 'blob:fake');
    // revokeUrl 是 URL.revokeObjectURL 的替身，用于断言对象地址被释放。
    const revokeUrl = vi.fn();
    Object.assign(URL, { createObjectURL: createUrl, revokeObjectURL: revokeUrl });
    // click 捕获临时链接被点击时的下载属性。
    // clicked 记录每次点击时链接的 href 与 download 属性。
    const clicked: Array<{ /* href 是被点击链接的地址。 */ href: string; /* download 是被点击链接的下载文件名。 */ download: string }> = [];
    // 点击替身：记录被点击链接的属性，不触发真实导航；this 是被点击的链接元素。
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(/* 点击替身：记录属性，不触发真实导航。 */ function (this: HTMLAnchorElement) {
      clicked.push({ href: this.href, download: this.download });
    });

    downloadBlob(new Blob(['k1\n']), '组A.txt');

    expect(createUrl).toHaveBeenCalledTimes(1);
    expect(clicked).toEqual([{ href: 'blob:fake', download: '组A.txt' }]);
    expect(revokeUrl).toHaveBeenCalledWith('blob:fake');
    expect(document.querySelector('a[download]')).toBeNull();
  });
});
