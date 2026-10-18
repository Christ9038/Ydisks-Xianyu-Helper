import { describe,expect,test } from 'vitest';
import { createDefaultReplyForm,defaultReplyImageFields,defaultReplyImageSource,validateDefaultReplyForm } from './defaultReplyState';

describe('默认回复表单状态', /* 当前回调验证账号和商品图文表单的纯业务规则。 */ () => {
  test('旧 URL 配置恢复 URL 来源，切换目标不携带旧内容', /* 当前回调验证历史配置和新建范围的独立草稿。 */ () => {
    // oldForm 是只有旧 URL 字段的账号配置。
    const oldForm = createDefaultReplyForm('a', '', 'account', { enabled: true, reply_content: '欢迎', reply_image_url: 'https://image.test/a.png' });
    expect(defaultReplyImageSource(oldForm)).toBe('url');
    expect(defaultReplyImageSource({ reply_image_url: 'https://image.test/legacy.jpg' })).toBe('url');
    expect(defaultReplyImageSource({ reply_image_url: '', reply_image_path: 'a.jpg' })).toBe('local');
    expect(defaultReplyImageSource({ reply_image_url: '' })).toBe('none');
    expect(createDefaultReplyForm('b', 'item', 'item')).toEqual(expect.objectContaining({ cookie_id: 'b', item_id: 'item', reply_content: '', reply_image_url: '', reply_image_path: '', image_source: 'none' }));
  });

  test('本地来源明确清空 URL，URL 来源明确清空本地路径，无图清空全部', /* 当前回调验证隐藏字段永远不被意外提交。 */ () => {
    // form 故意保存两类旧值，提交时必须只采用用户选择的来源。
    const form = { ...createDefaultReplyForm('a'), reply_image_url: ' https://image.test/a.png ', reply_image_path: ' product/a.jpg ' };
    expect(defaultReplyImageFields({ ...form, image_source: 'local' })).toEqual({ reply_image_url: '', reply_image_path: 'product/a.jpg' });
    expect(defaultReplyImageFields({ ...form, image_source: 'url' })).toEqual({ reply_image_url: 'https://image.test/a.png', reply_image_path: '' });
    expect(defaultReplyImageFields({ ...form, image_source: 'none' })).toEqual({ reply_image_url: '', reply_image_path: '' });
  });

  test.each(['', '/a.jpg', '../a.jpg', 'a/../b.jpg', 'a/./b.jpg', 'a//b.jpg', 'C:/a.jpg', 'a\\b.jpg', 'a/'])('拒绝越界或无效本地路径 %j', /* path 是不能表示专用目录内单个普通文件的路径。 */ path => {
    expect(validateDefaultReplyForm({ ...createDefaultReplyForm('a'), image_source: 'local', reply_image_path: path })).toContain('相对路径');
  });

  test('商品空配置允许继承，账号启用时必须有内容；单张本地图合法', /* 当前回调验证兼容兜底与新来源校验。 */ () => {
    expect(validateDefaultReplyForm(createDefaultReplyForm(''))).toBe('请先选择账号');
    expect(validateDefaultReplyForm(createDefaultReplyForm('a', '', 'item'))).toBe('请选择关联商品');
    expect(validateDefaultReplyForm(createDefaultReplyForm('a', 'item', 'item'))).toBe('');
    expect(validateDefaultReplyForm({ ...createDefaultReplyForm('a'), enabled: true })).toContain('请填写');
    expect(validateDefaultReplyForm({ ...createDefaultReplyForm('a'), enabled: true, image_source: 'local', reply_image_path: '商品一/欢迎.jpg' })).toBe('');
    expect(validateDefaultReplyForm({ ...createDefaultReplyForm('a'), image_source: 'url', reply_image_url: 'file:///a.jpg' })).toContain('HTTP(S)');
    expect(validateDefaultReplyForm({ ...createDefaultReplyForm('a'), image_source: 'url', reply_image_url: 'https://image.test/a.jpg' })).toBe('');
  });
});
