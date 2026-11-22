import { describe, expect, test } from 'vitest';
import { buildCreatePayload, emptyAddForm, validateAddForm } from './createCardDraft';

describe('新增卡密草稿', /* 验证校验分支与四种类型的载荷映射。 */ () => {
  test('名称为空、内容为空时返回对应原因', /* 校验失败分支。 */ () => {
    expect(validateAddForm(emptyAddForm())).toBe('请输入卡密名称');
    expect(validateAddForm({ ...emptyAddForm(), name: '卡' })).toBe('请输入卡密内容');
    expect(validateAddForm({ ...emptyAddForm(), name: '接口', type: 'api' })).toBe('请输入 API 地址');
  });

  test('图片卡密只选图片库图片也算通过', /* 图库图片与内容互斥。 */ () => {
    expect(validateAddForm({ ...emptyAddForm(), name: '图', type: 'image', library_image_id: 5 })).toBe('');
    expect(validateAddForm({ ...emptyAddForm(), name: '文', type: 'text', content: '好' })).toBe('');
  });

  test('四种类型映射为各自的内容字段', /* 载荷映射。 */ () => {
    expect(buildCreatePayload({ ...emptyAddForm(), name: ' 库存 ', type: 'data', content: ' A\nB ' })).toMatchObject({ name: '库存', type: 'data', data_content: 'A\nB' });
    expect(buildCreatePayload({ ...emptyAddForm(), name: '文', type: 'text', content: ' 谢谢 ' })).toMatchObject({ type: 'text', text_content: '谢谢' });
    expect(buildCreatePayload({ ...emptyAddForm(), name: '图', type: 'image', content: 'https://x/a.png', library_image_id: 7 })).toMatchObject({ image_url: '', library_image_id: 7 });
    expect(buildCreatePayload({ ...emptyAddForm(), name: '图', type: 'image', content: ' https://x/a.png ' })).toMatchObject({ image_url: 'https://x/a.png', library_image_id: 0 });
    expect(buildCreatePayload({ ...emptyAddForm(), name: '接口', type: 'api', content: 'https://x/api' }).api_config).toMatchObject({ url: 'https://x/api', headers_action: 'replace' });
  });
});
