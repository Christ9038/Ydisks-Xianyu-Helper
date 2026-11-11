import { beforeEach, describe, expect, test, vi } from 'vitest';

// postMock、deleteMock 是契约客户端方法替身；runMock 记录请求控制参数。
const { postMock, deleteMock, runMock } = vi.hoisted(/* 创建契约客户端方法替身。 */ () => ({ postMock: vi.fn(), deleteMock: vi.fn(), runMock: vi.fn() }));

vi.mock('../../../shared/api-contract/client', /* 提供导入 API 适配测试所需的契约依赖。 */ () => ({
  contractClient: { GET: vi.fn(), POST: postMock, PUT: vi.fn(), DELETE: deleteMock },
  contractMultipartBody: <T>(form: FormData) => form as unknown as T,
  runContractRequest: /* 执行请求回调并记录控制参数。 */ async (execute: (signal: AbortSignal) => Promise<{ /** data 是解码后的成功响应。 */ data?: unknown }>, options?: unknown) => {
    runMock(options);
    return (await execute(new AbortController().signal)).data;
  },
}));

import { commitTemplateImport, discardTemplateImport, previewTemplateImport } from './api';

// Excel 导入 API 适配器：multipart 字段、路径参数、超时与 DTO 到 UI 模型的转换。
describe('template import api adapter', /* 测试组回调覆盖预检、提交与丢弃。 */ () => {
  beforeEach(/* 重置替身。 */ () => { vi.clearAllMocks(); });

  test('预检上传表格与可选压缩包并转换逐行结果', /* 当前回调验证预检请求与映射。 */ async () => {
    postMock.mockResolvedValue({ data: { import_id: 'import_1', total: 2, valid: 1, invalid: 1, remote_images: 3,
      rows: [{ row_no: 2, valid: true, name: 'A', title: 'a', price: '1', image_count: 3, remote_image_count: 3, errors: [], warnings: ['重名'] }] } });
    // sheet、zip 是用户选择的文件。
    const sheet = new File(['x'], 't.csv');
    // zip 是可选图片压缩包。
    const zip = new File(['z'], 'i.zip');
    // preview 是转换后的预检结果。
    const preview = await previewTemplateImport({ sheet, imagesZip: zip });
    expect(preview).toEqual({ importId: 'import_1', total: 2, valid: 1, invalid: 1, remoteImages: 3,
      rows: [{ rowNo: 2, valid: true, name: 'A', title: 'a', price: '1', imageCount: 3, remoteImageCount: 3, errors: [], warnings: ['重名'] }] });
    // body 是提交的 multipart 表单。
    const body = postMock.mock.calls[0][1].body as FormData;
    expect(postMock.mock.calls[0][0]).toBe('/api/v1/library/product-template-imports');
    expect(body.get('file')).toBe(sheet);
    expect(body.get('images_zip')).toBe(zip);
    expect(runMock).toHaveBeenCalledWith({ timeoutMs: 300_000 });
    postMock.mockClear();
    await previewTemplateImport({ sheet, imagesZip: null });
    expect((postMock.mock.calls[0][1].body as FormData).has('images_zip')).toBe(false);
  });

  test('分段提交按任务路径调用并转换进度，丢弃调用删除接口', /* 当前回调验证提交与丢弃。 */ async () => {
    postMock.mockResolvedValue({ data: { import_id: 'import_1', done: false, created: 1, failed: 1, pending: 2, invalid: 0, created_template_ids: [7],
      rows: [{ row_no: 2, status: 'created', template_id: 7, errors: [] }, { row_no: 3, status: 'failed', template_id: 0, errors: ['图片下载失败'] }] } });
    // progress 是转换后的进度。
    const progress = await commitTemplateImport('import_1');
    expect(progress).toEqual({ importId: 'import_1', done: false, created: 1, failed: 1, pending: 2, invalid: 0, createdTemplateIds: [7],
      rows: [{ rowNo: 2, status: 'created', templateId: 7, errors: [] }, { rowNo: 3, status: 'failed', templateId: 0, errors: ['图片下载失败'] }] });
    expect(postMock.mock.calls[0][0]).toBe('/api/v1/library/product-template-imports/{import_id}/commit');
    expect(postMock.mock.calls[0][1].params).toEqual({ path: { import_id: 'import_1' } });
    expect(runMock).toHaveBeenCalledWith({ timeoutMs: 150_000 });
    deleteMock.mockResolvedValue({ data: { success: true } });
    await discardTemplateImport('import_1');
    expect(deleteMock.mock.calls[0][0]).toBe('/api/v1/library/product-template-imports/{import_id}');
    expect(deleteMock.mock.calls[0][1].params).toEqual({ path: { import_id: 'import_1' } });
  });
});
