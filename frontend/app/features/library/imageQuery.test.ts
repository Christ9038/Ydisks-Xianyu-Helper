import { describe, expect, test } from 'vitest';
import { defaultImageFilters, filterImages, formatBytes, imageFormat, imageUsage, isImageFilterActive, queryImages } from './imageQuery';
import type { LibraryImage, ProductTemplate } from './types';

/** makeImage 构造测试图片，只覆盖关心的字段。 */
const makeImage = (id: number, patch: Partial<LibraryImage>): LibraryImage => ({ id, filename: `f${id}.png`, contentType: 'image/png', sizeBytes: 1, createdAt: '2026-01-01T00:00:00Z', ...patch });

// images 是搜索、筛选和排序共用的图片列表。
const images = [
  makeImage(1, { filename: 'Cover.PNG', sizeBytes: 300, createdAt: '2026-01-03T00:00:00Z' }),
  makeImage(2, { filename: '详情图.jpg', contentType: 'image/jpeg', sizeBytes: 100, createdAt: '2026-01-01T00:00:00Z' }),
  makeImage(3, { filename: 'anim.gif', contentType: 'image/gif', sizeBytes: 200, createdAt: 'bad-time' }),
];

/** ids 取出图片主键序列，方便断言顺序。 */
const ids = (list: LibraryImage[]): number[] => list.map(/* image 取主键。 */ image => image.id);

describe('filterImages', /* 图片文件名搜索。 */ () => {
  test('空关键字返回全部，匹配不区分大小写并支持中文', /* 搜索分支。 */ () => {
    expect(filterImages(images, '  ')).toHaveLength(3);
    expect(ids(filterImages(images, 'cover'))).toEqual([1]);
    expect(ids(filterImages(images, '详情'))).toEqual([2]);
    expect(filterImages(images, 'zzz')).toEqual([]);
  });
});

describe('imageFormat', /* contentType 归一。 */ () => {
  test('识别常见格式、忽略参数，未知类型返回 null', /* 格式分支。 */ () => {
    expect(imageFormat('image/jpeg')).toBe('jpg');
    expect(imageFormat('IMAGE/JPG')).toBe('jpg');
    expect(imageFormat('image/png; charset=x')).toBe('png');
    expect(imageFormat('image/webp')).toBe('webp');
    expect(imageFormat('image/gif')).toBe('gif');
    expect(imageFormat('image/bmp')).toBeNull();
  });
});

describe('imageUsage', /* 模板引用计数。 */ () => {
  test('主图与规格图合并，同一模板只计一次，0 表示无图', /* 计数分支。 */ () => {
    // templates 是只带引用字段的最小模板。
    const templates = [
      { imageIds: [1, 2], specs: [{ values: [{ imageId: 2 }, { imageId: 0 }] }] },
      { imageIds: [2], specs: [] },
    ] as unknown as ProductTemplate[];
    // usage 是计数结果。
    const usage = imageUsage(templates);
    expect(usage.get(1)).toBe(1);
    expect(usage.get(2)).toBe(2);
    expect(usage.has(0)).toBe(false);
  });
});

describe('formatBytes', /* 文件大小展示。 */ () => {
  test('按 B、KB、MB 分档', /* 单位边界。 */ () => {
    expect(formatBytes(1023)).toBe('1023 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB');
  });
});

describe('queryImages', /* 搜索、筛选与排序组合。 */ () => {
  test('默认最新在前，非法时间排在最后且不修改入参', /* 默认排序。 */ () => {
    // before 是调用前的顺序快照。
    const before = ids(images);
    expect(ids(queryImages(images, defaultImageFilters))).toEqual([1, 2, 3]);
    expect(ids(images)).toEqual(before);
  });

  test('覆盖全部排序方式', /* 排序分支。 */ () => {
    expect(ids(queryImages(images, { ...defaultImageFilters, sort: 'oldest' }))).toEqual([3, 2, 1]);
    // named 是只含 ASCII 文件名的列表，避免中英文混排顺序依赖运行环境的 ICU 排序规则。
    const named = [makeImage(1, { filename: 'b.png' }), makeImage(2, { filename: 'c.png' }), makeImage(3, { filename: 'a.png' })];
    expect(ids(queryImages(named, { ...defaultImageFilters, sort: 'name-asc' }))).toEqual([3, 1, 2]);
    expect(ids(queryImages(named, { ...defaultImageFilters, sort: 'name-desc' }))).toEqual([2, 1, 3]);
    expect(ids(queryImages(images, { ...defaultImageFilters, sort: 'size-desc' }))).toEqual([1, 3, 2]);
    expect(ids(queryImages(images, { ...defaultImageFilters, sort: 'size-asc' }))).toEqual([2, 3, 1]);
  });

  test('格式筛选与引用筛选，缺少引用计数时忽略引用筛选', /* 筛选分支。 */ () => {
    expect(ids(queryImages(images, { ...defaultImageFilters, format: 'gif' }))).toEqual([3]);
    // usage 表示只有图片 1 被引用。
    const usage = new Map([[1, 1]]);
    expect(ids(queryImages(images, { ...defaultImageFilters, usage: 'used' }, usage))).toEqual([1]);
    expect(ids(queryImages(images, { ...defaultImageFilters, usage: 'unused' }, usage))).toEqual([2, 3]);
    expect(queryImages(images, { ...defaultImageFilters, usage: 'used' })).toHaveLength(3);
    expect(ids(queryImages(images, { ...defaultImageFilters, query: 'cover', format: 'jpg' }))).toEqual([]);
  });

  test('isImageFilterActive 不把排序算作筛选', /* 重置按钮条件。 */ () => {
    expect(isImageFilterActive({ ...defaultImageFilters, sort: 'size-asc' })).toBe(false);
    expect(isImageFilterActive({ ...defaultImageFilters, query: ' a ' })).toBe(true);
    expect(isImageFilterActive({ ...defaultImageFilters, format: 'png' })).toBe(true);
    expect(isImageFilterActive({ ...defaultImageFilters, usage: 'used' })).toBe(true);
  });
});
