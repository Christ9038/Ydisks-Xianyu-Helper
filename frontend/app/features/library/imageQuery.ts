import type { LibraryImage, ProductTemplate } from './types';

/** ImageFormatFilter 是图片格式筛选项，all 表示不过滤。 */
export type ImageFormatFilter = 'all' | 'jpg' | 'png' | 'webp' | 'gif';

/** ImageUsageFilter 是图片引用筛选项：被商品模板引用、未被引用，all 表示不过滤。 */
export type ImageUsageFilter = 'all' | 'used' | 'unused';

/** ImageSort 是图片排序方式。 */
export type ImageSort = 'newest' | 'oldest' | 'name-asc' | 'name-desc' | 'size-desc' | 'size-asc';

/** ImageFilters 是图片列表的搜索、筛选与排序条件，由界面持有并交给 queryImages 推导可见列表。 */
export interface ImageFilters {
  /** query 是文件名搜索词。 */
  query: string;
  /** format 是格式筛选。 */
  format: ImageFormatFilter;
  /** usage 是引用筛选，需要 queryImages 同时传入引用计数才会生效。 */
  usage: ImageUsageFilter;
  /** sort 是排序方式。 */
  sort: ImageSort;
}

/** defaultImageFilters 是默认条件：不搜索、不筛选、最新上传在前。 */
export const defaultImageFilters: ImageFilters = { query: '', format: 'all', usage: 'all', sort: 'newest' };

/** isImageFilterActive 判断是否有任何搜索或筛选生效（排序不算筛选），用于决定是否展示“重置”。 */
export const isImageFilterActive = (filters: ImageFilters): boolean => filters.query.trim() !== '' || filters.format !== 'all' || filters.usage !== 'all';

/** filterImages 按文件名做不区分大小写的包含匹配；空关键字返回全部图片。 */
export const filterImages = (images: LibraryImage[], query: string): LibraryImage[] => {
  // keyword 是去除首尾空白并小写化的搜索词。
  const keyword = query.trim().toLowerCase();
  if (!keyword) return images;
  return images.filter(/* image 按文件名匹配搜索词。 */ image => image.filename.toLowerCase().includes(keyword));
};

/** imageFormat 把服务端嗅探的 contentType 归一为筛选格式；不在支持列表内的类型返回 null。 */
export const imageFormat = (contentType: string): Exclude<ImageFormatFilter, 'all'> | null => {
  // type 是去掉参数并小写化后的媒体类型。
  const type = contentType.split(';')[0].trim().toLowerCase();
  if (type === 'image/jpeg' || type === 'image/jpg') return 'jpg';
  if (type === 'image/png') return 'png';
  if (type === 'image/webp') return 'webp';
  if (type === 'image/gif') return 'gif';
  return null;
};

/** imageUsage 统计每张图片被多少个模板引用（主图与规格值图合并，同一模板只计一次），键为图片主键。 */
export const imageUsage = (templates: ProductTemplate[]): Map<number, number> => {
  // usage 是图片主键到引用模板数的映射。
  const usage = new Map<number, number>();
  for (const /* template 是正在统计的模板。 */ template of templates) {
    // ids 是该模板引用的去重图片主键；0 表示规格值没有图片。
    const ids = new Set<number>(template.imageIds);
    for (const /* spec 是模板的一个规格维度。 */ spec of template.specs) for (const /* value 是该维度下的规格值，imageId 为 0 表示没有图片。 */ value of spec.values) ids.add(value.imageId);
    ids.delete(0);
    for (const /* id 是被该模板引用的图片主键。 */ id of ids) usage.set(id, (usage.get(id) ?? 0) + 1);
  }
  return usage;
};

/** formatBytes 把字节数格式化为 B/KB/MB 文本，供卡片展示文件大小。 */
export const formatBytes = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
};

/** imageTime 把入库时间转为毫秒数；无法解析时按 0 处理，保证排序稳定。 */
const imageTime = (image: LibraryImage): number => {
  // time 是解析出的毫秒时间戳，非法时间为 NaN。
  const time = Date.parse(image.createdAt);
  return Number.isNaN(time) ? 0 : time;
};

/** compareImages 返回指定排序方式的比较器；主键作为最后的平局依据，让同值排序可预测。 */
const compareImages = (sort: ImageSort): ((a: LibraryImage, b: LibraryImage) => number) => {
  switch (sort) {
    case 'oldest': return /* 较早入库在前。 */ (a, b) => imageTime(a) - imageTime(b) || a.id - b.id;
    case 'name-asc': return /* 文件名升序，使用中文友好的本地化比较。 */ (a, b) => a.filename.localeCompare(b.filename, 'zh-CN') || a.id - b.id;
    case 'name-desc': return /* 文件名降序。 */ (a, b) => b.filename.localeCompare(a.filename, 'zh-CN') || a.id - b.id;
    case 'size-desc': return /* 文件大的在前。 */ (a, b) => b.sizeBytes - a.sizeBytes || a.id - b.id;
    case 'size-asc': return /* 文件小的在前。 */ (a, b) => a.sizeBytes - b.sizeBytes || a.id - b.id;
    default: return /* 较新入库在前。 */ (a, b) => imageTime(b) - imageTime(a) || b.id - a.id;
  }
};

/**
 * queryImages 依次应用文件名搜索、格式筛选、引用筛选和排序，返回新数组，不修改入参。
 * usage 缺省时（例如图片选择弹窗没有加载模板）引用筛选被忽略。
 */
export const queryImages = (images: LibraryImage[], filters: ImageFilters, usage?: Map<number, number>): LibraryImage[] => {
  // matched 是通过搜索、格式和引用筛选的图片。
  const matched = filterImages(images, filters.query).filter(/* image 同时满足格式与引用条件。 */ image => {
    if (filters.format !== 'all' && imageFormat(image.contentType) !== filters.format) return false;
    if (usage && filters.usage !== 'all') {
      // used 表示该图片至少被一个模板引用。
      const used = (usage.get(image.id) ?? 0) > 0;
      if (filters.usage === 'used' ? !used : used) return false;
    }
    return true;
  });
  return [...matched].sort(compareImages(filters.sort));
};
