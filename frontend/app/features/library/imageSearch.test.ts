import { describe, expect, test } from 'vitest';
import { filterImages } from './imageSearch';

// images 是搜索测试使用的图片列表。
const images = [
  { id: 1, filename: 'Cover.PNG', contentType: 'image/png', sizeBytes: 1, createdAt: '' },
  { id: 2, filename: '详情图.jpg', contentType: 'image/jpeg', sizeBytes: 1, createdAt: '' },
];

describe('filterImages', /* 图片文件名搜索。 */ () => {
  test('空关键字返回全部，匹配不区分大小写并支持中文', /* 搜索分支。 */ () => {
    expect(filterImages(images, '  ')).toHaveLength(2);
    expect(filterImages(images, 'cover').map(/* image 取主键。 */ image => image.id)).toEqual([1]);
    expect(filterImages(images, '详情').map(/* image 取主键。 */ image => image.id)).toEqual([2]);
    expect(filterImages(images, 'zzz')).toEqual([]);
  });
});
