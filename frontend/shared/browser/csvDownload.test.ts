// @vitest-environment jsdom
// 本测试需要 Blob 与 URL.createObjectURL，因此使用 jsdom 环境。
import { afterEach, describe, expect, test, vi } from 'vitest';
import { buildCsv, downloadCsv } from './csvDownload';

// CSV 共享工具：单元格转义规则与带 BOM 的下载内容。
describe('csvDownload', /* 测试组回调覆盖 CSV 生成与下载。 */ () => {
  afterEach(/* 恢复浏览器替身。 */ () => { vi.restoreAllMocks(); });

  test('每个单元格加引号并转义内部双引号', /* 当前回调验证转义规则。 */ () => {
    expect(buildCsv([['a', 'b"c'], ['1,2', '']])).toBe('"a","b""c"\n"1,2",""');
  });

  test('下载内容带 UTF-8 BOM 并使用建议文件名', /* 当前回调验证下载文件内容。 */ async () => {
    // createObjectURL 记录生成的文件对象。
    const createObjectURL = vi.fn(/* fakeUrl 返回固定临时地址。 */ (_blob: Blob) => 'blob:csv');
    URL.createObjectURL = createObjectURL;
    URL.revokeObjectURL = vi.fn();
    // click 记录触发下载的链接。
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(/* 屏蔽真实下载。 */ function (this: HTMLAnchorElement) { expect(this.download).toBe('模板.csv'); });
    downloadCsv('模板.csv', [['标题'], ['商品']]);
    expect(click).toHaveBeenCalled();
    // bytes 是下载文件的原始字节；按规范 Blob.text() 会去掉 BOM，因此直接检查前三个字节。
    const bytes = new Uint8Array(await createObjectURL.mock.calls[0][0].arrayBuffer());
    expect(Array.from(bytes.slice(0, 3))).toEqual([0xef, 0xbb, 0xbf]);
    expect(new TextDecoder().decode(bytes.slice(3))).toBe('"标题"\n"商品"');
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:csv');
  });
});
