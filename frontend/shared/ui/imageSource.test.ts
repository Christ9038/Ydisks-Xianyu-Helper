import { describe, expect, it } from 'vitest';
import { emptyImageSource, hasImageSource, libraryImageContentUrl, withImageUrl, withLibraryImage } from './imageSource';

describe('imageSource', /* 当前回调验证图片来源模型的互斥与判空。 */ () => {
  it('选择图片库图片会清空外链，输入外链会清空图片库引用', /* 当前回调验证两个构造函数的互斥语义。 */ () => {
    expect(withLibraryImage(7)).toEqual({ url: '', libraryImageId: 7 });
    expect(withImageUrl('https://a/b.png')).toEqual({ url: 'https://a/b.png', libraryImageId: 0 });
  });

  it('hasImageSource 仅在存在图片库引用或非空白外链时为真', /* 当前回调验证判空规则。 */ () => {
    expect(hasImageSource(emptyImageSource)).toBe(false);
    expect(hasImageSource(withImageUrl('   '))).toBe(false);
    expect(hasImageSource(withImageUrl('https://a/b.png'))).toBe(true);
    expect(hasImageSource(withLibraryImage(3))).toBe(true);
  });

  it('预览地址指向图片库内容接口', /* 当前回调验证预览地址格式。 */ () => {
    expect(libraryImageContentUrl(9)).toBe('/api/v1/library/images/9/content');
  });
});
