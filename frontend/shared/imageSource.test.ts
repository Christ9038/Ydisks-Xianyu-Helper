import { describe, expect, test } from 'vitest';
import { imageSource, imageSourceFields, validateImageSource } from './imageSource';

describe('图片来源模型', /* 当前回调验证与自动回复一致的来源和输入边界。 */ () => {
  test('历史来源推断与显式空来源选择互不覆盖', /* 当前回调验证保存时清空隐藏旧值。 */ () => {
    expect(imageSource({})).toBe('url');
    expect(imageSource({ image_path: 'a.png' })).toBe('local');
    expect(imageSource({ image_source: 'local', image_url: 'https://example.com/old.png' })).toBe('local');
    expect(imageSourceFields({ image_source: 'url', image_url: ' https://example.com/a.png ', image_path: 'old.png' })).toEqual({ image_url: 'https://example.com/a.png', image_path: '' });
    expect(imageSourceFields({ image_source: 'local', image_url: 'https://example.com/old.png', image_path: ' goods/a.png ' })).toEqual({ image_url: '', image_path: 'goods/a.png' });
    expect(imageSourceFields({})).toEqual({ image_url: '', image_path: '' });
  });

  test.each(['', 'file:///tmp/a.png', 'data:image/png;base64,a', 'javascript:alert(1)', '//example.com/a.png', 'https://example.com/a b.png'])('拒绝非 HTTP(S) 或空白 URL %s', /* 当前回调拒绝自动回复模型不接受的 URL 输入。 */ image_url => {
    expect(validateImageSource({ image_url })).toContain('HTTP(S)');
  });

  test.each(['', '../a.png', '/a.png', 'a/../b.png', './a.png', 'a//b.png', 'C:/a.png', 'a\\b.png', 'a/./b.png', 'a\u0000.png', 'a\n.png'])('拒绝越界或非法相对路径 %s', /* 当前回调验证专用账号目录的引用边界。 */ image_path => {
    expect(validateImageSource({ image_source: 'local', image_path })).toContain('相对路径');
  });

  test.each(['a.png', 'goods/教程.jpg', 'goods/code.gif'])('允许合法相对路径 %s', /* 当前回调将文件存在性留给服务端校验。 */ image_path => {
    expect(validateImageSource({ image_path })).toBe('');
  });

  test('图片来源拒绝模板变量，保持与保存接口一致', /* 当前回调拒绝被服务端当作动态来源的路径和URL。 */ () => {
    expect(validateImageSource({ image_path: 'goods/{{cards.literal}}.gif' })).toContain('不支持模板变量');
    expect(validateImageSource({ image_url: 'https://example.com/{{custom.image}}.png' })).toContain('不支持模板变量');
  });

  test('接受 HTTP(S) 地址并忽略未选中的旧路径', /* 当前回调保持与自动回复 URL 校验一致。 */ () => {
    expect(validateImageSource({ image_source: 'url', image_url: ' HTTPS://example.com/a.png ', image_path: '../old.png' })).toBe('');
    expect(validateImageSource({ image_url: 'http://example.com/a.png' })).toBe('');
  });
});
