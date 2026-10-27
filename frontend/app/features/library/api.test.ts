import { beforeEach, describe, expect, test, vi } from 'vitest';

// mocks 是契约客户端各方法的共享替身。
const { getMock, postMock, putMock, deleteMock } = vi.hoisted(/* 创建契约客户端方法替身。 */ () => ({ getMock: vi.fn(), postMock: vi.fn(), putMock: vi.fn(), deleteMock: vi.fn() }));

vi.mock('../../../shared/api-contract/client', /* 提供图片库 API 适配测试所需的契约依赖。 */ () => ({
  contractClient: { GET: getMock, POST: postMock, PUT: putMock, DELETE: deleteMock },
  contractMultipartBody: <T>(form: FormData) => form as unknown as T,
  runContractRequest: /* 执行适配器传入的请求回调并返回解码数据。 */ async (execute: (signal: AbortSignal) => Promise<{ /** data 是解码后的成功响应。 */ data?: unknown }>) => (await execute(new AbortController().signal)).data,
}));

import { deleteLibraryImage, fetchLibraryImageFile, libraryErrorMessage, libraryImageUrl, listAccountOptions, listCardOptions, listDeliveryTemplateOptions, listLibraryImages, listProductTemplates, previewTemplatePublish, saveProductTemplate, startPublishBatch, uploadLibraryImages, deleteProductTemplate } from './api';
import { ApiError } from '../../../shared/http/client';

// templateDto 是服务端返回的双规格模板传输对象。
const templateDto = {
  id: 1, name: 'n', title: 't', description: 'd', price: '9.90', original_price: '', quantity: 3, postage_mode: 'fixed', postage: '5.00',
  category: { cat_id: '1', cat_name: 'c' }, image_ids: [11],
  specs: { specs: [{ property_name: '颜色', support_image: true, values: [{ value: '红', image_index: 0 }, { value: '蓝', image_index: -1 }] }], skus: [{ price: '1.00', quantity: 2, property_list: [{ property_text: '颜色', value_text: '红' }] }], spec_image_ids: [21] },
  automation: {
    paid_delivery: { enabled: true, actions: [{ action_type: 'send_template', card_id: 0, delivery_count: 1, delay_seconds: 0, delivery_template_id: 4, template_bindings: [{ variable_key: 'main', card_id: 2, delivery_count: 1 }], custom_variables: { note: 'x' }, spec_name: '颜色', spec_value: '红' }] },
    review_gift: { enabled: false, actions: [] }, review_request: { enabled: true, after_shipped_hours: 24, message: 'm', max_attempts: 2, delay_seconds: 1 },
  },
  created_at: 'c', updated_at: 'u',
};

describe('library api adapter', /* 图片库与模板的 API 适配。 */ () => {
  beforeEach(/* 重置契约客户端替身。 */ () => { vi.clearAllMocks(); });

  test('模板传输对象转换为 UI 模型并还原规格图片', /* 模板映射。 */ async () => {
    getMock.mockResolvedValue({ data: [templateDto] });
    // templates 是转换结果。
    const templates = await listProductTemplates();
    expect(templates[0].specs[0].values.map(/* value 取图片主键。 */ value => value.imageId)).toEqual([21, 0]);
    expect(templates[0].automation.paidDelivery.actions[0]).toMatchObject({ actionType: 'send_template', deliveryTemplateId: 4, customVariables: { note: 'x' }, specValue: '红' });
    expect(templates[0].postageMode).toBe('fixed');
  });

  test('保存按 id 选择创建或更新，删除调用对应路径', /* 保存分支。 */ async () => {
    postMock.mockResolvedValue({ data: templateDto });
    putMock.mockResolvedValue({ data: templateDto });
    deleteMock.mockResolvedValue({ data: { success: true } });
    await saveProductTemplate(null, {} as never);
    await saveProductTemplate(5, {} as never);
    await deleteProductTemplate(5);
    await deleteLibraryImage(2);
    expect(postMock).toHaveBeenCalledTimes(1);
    expect(putMock.mock.calls[0][1].params.path.template_id).toBe(5);
    expect(deleteMock).toHaveBeenCalledTimes(2);
  });

  test('图片列表、上传结果与内容地址', /* 图片映射。 */ async () => {
    getMock.mockResolvedValue({ data: [{ id: 1, filename: 'a.png', content_type: 'image/png', size_bytes: 3, created_at: 't' }] });
    expect((await listLibraryImages())[0]).toMatchObject({ id: 1, contentType: 'image/png', sizeBytes: 3 });
    postMock.mockResolvedValue({ data: { results: [{ filename: 'a.png', duplicate: true, error: '', image: null }, { filename: 'b', duplicate: false, error: '不是图片', image: null }] } });
    // results 是上传结果。
    const results = await uploadLibraryImages([new File(['x'], 'a.png')]);
    expect(results[0]).toMatchObject({ duplicate: true, image: null });
    expect(results[1].error).toBe('不是图片');
    // body 是 multipart 请求体。
    expect((postMock.mock.calls[0][1].body as FormData).getAll('files')).toHaveLength(1);
    expect(libraryImageUrl(7)).toBe('/api/v1/library/images/7/content');
  });

  test('取回图片文件保留文件名和类型', /* 二进制读取。 */ async () => {
    getMock.mockResolvedValue({ data: new Blob(['abc'], { type: 'image/png' }) });
    // file 是包装后的文件。
    const file = await fetchLibraryImageFile({ id: 1, filename: 'a.png', contentType: 'image/png', sizeBytes: 3, createdAt: '' });
    expect(file.name).toBe('a.png');
    expect(file.type).toBe('image/png');
    expect(getMock.mock.calls[0][1].parseAs).toBe('blob');
  });

  test('预检请求省略未提供的发货地，启动返回批次标识', /* 发布请求。 */ async () => {
    postMock.mockResolvedValueOnce({ data: { preview_id: 'p', total: 1, valid: 1, invalid: 0, rows: [{ row_no: 1, valid: true, cookie_id: 'a', title: 't', price: '1', quantity: 1 }] } });
    // preview 是预检结果。
    const preview = await previewTemplatePublish({ templateIds: [1], cookieIds: ['a'], intervalSeconds: 5 });
    expect(preview.rows[0].errors).toEqual([]);
    expect(postMock.mock.calls[0][1].body).not.toHaveProperty('location');
    postMock.mockResolvedValueOnce({ data: { batch_id: 'b1' } });
    expect(await startPublishBatch('p')).toBe('b1');
  });

  test('卡密、发货模板与账号选项仅保留可用项', /* 选项过滤。 */ async () => {
    getMock.mockResolvedValueOnce({ data: [{ id: 1, name: 'A', type: 'data', enabled: true }, { id: 2, name: 'B', type: 'text', enabled: false }] });
    expect(await listCardOptions()).toEqual([{ id: 1, name: 'A', type: 'data' }]);
    getMock.mockResolvedValueOnce({ data: { data: [{ id: 3, name: 'T', enabled: true, keys: ['k'], custom_keys: ['c'] }, { id: 4, name: 'X', enabled: false, keys: [] }] } });
    expect(await listDeliveryTemplateOptions()).toEqual([{ id: 3, name: 'T', keys: ['k'], customKeys: ['c'] }]);
    getMock.mockResolvedValueOnce({ data: [{ id: 'a', enabled: true, has_cookie: true, remark: '店', nickname: '' }, { id: 'b', enabled: false, has_cookie: true, remark: '', nickname: '' }] });
    expect(await listAccountOptions()).toEqual([{ id: 'a', label: '店' }]);
  });

  test('错误文案：冲突附带模板名，其余回退', /* 错误映射。 */ () => {
    expect(libraryErrorMessage(new ApiError(409, { code: 'conflict', message: '被引用', details: { templates: ['A', 'B'] } }), 'x')).toBe('被引用：A、B');
    expect(libraryErrorMessage(new ApiError(500, { code: 'e', message: '' }), '兜底')).toBe('兜底');
    expect(libraryErrorMessage(new Error('boom'), 'x')).toBe('boom');
    expect(libraryErrorMessage('str', 'x')).toBe('x');
  });
});
