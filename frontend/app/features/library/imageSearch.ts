import type { LibraryImage } from './types';

/** filterImages 按文件名做不区分大小写的包含匹配；空关键字返回全部图片。 */
export const filterImages = (images: LibraryImage[], query: string): LibraryImage[] => {
  // keyword 是去除首尾空白并小写化的搜索词。
  const keyword = query.trim().toLowerCase();
  if (!keyword) return images;
  return images.filter(/* image 按文件名匹配搜索词。 */ image => image.filename.toLowerCase().includes(keyword));
};
