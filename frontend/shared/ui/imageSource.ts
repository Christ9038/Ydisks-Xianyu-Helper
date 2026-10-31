import type { ComponentType } from 'react';

/** ImageSourceValue 是图片回复的来源：外链 URL 与图片库图片互斥，两者都为空表示未配置。 */
export interface ImageSourceValue {
  /** url 是外链图片地址；选择图片库图片后为空。 */
  url: string;
  /** libraryImageId 是图片库图片主键；0 表示未使用图片库。 */
  libraryImageId: number;
}

/** LibraryImageChoice 是图片库选择器回传的最小图片信息。 */
export interface LibraryImageChoice {
  /** id 是图片库图片主键。 */
  id: number;
  /** filename 是上传时的原始文件名，仅用于展示。 */
  filename: string;
}

/** LibraryImageChooserProps 是应用壳注入各功能页的单选图片库选择器属性。 */
export interface LibraryImageChooserProps {
  /** open 表示是否显示选择器。 */
  open: boolean;
  /** onClose 关闭选择器而不选择图片。 */
  onClose: () => void;
  /** onSelect 返回用户确认选择的那一张图片。 */
  onSelect: (image: LibraryImageChoice) => void;
}

/** LibraryImageChooserComponent 是应用壳按需注入的单选图片库选择器组件类型。 */
export type LibraryImageChooserComponent = ComponentType<LibraryImageChooserProps>;

/** emptyImageSource 是未配置任何图片来源的初始值。 */
export const emptyImageSource: ImageSourceValue = { url: '', libraryImageId: 0 };

/** libraryImageContentUrl 返回图片库图片的预览地址，仅用于缩略图展示。 */
export const libraryImageContentUrl = (id: number): string => `/api/v1/library/images/${id}/content`;

/** withLibraryImage 切换为图片库图片并清空外链，保证两种来源互斥。 */
export const withLibraryImage = (id: number): ImageSourceValue => ({ url: '', libraryImageId: id });

/** withImageUrl 切换为外链图片并清空图片库引用，保证两种来源互斥。 */
export const withImageUrl = (url: string): ImageSourceValue => ({ url, libraryImageId: 0 });

/** hasImageSource 判断是否已配置有效图片来源（图片库图片或非空白 URL）。 */
export const hasImageSource = (value: ImageSourceValue): boolean => value.libraryImageId > 0 || value.url.trim() !== '';
