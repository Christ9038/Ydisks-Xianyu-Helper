import type { LibraryImageChooserProps } from '../../../../shared/ui/imageSource';
import { ImagePickerDialog } from './ImagePickerDialog';

/** LibraryImageChooser 单选一张图片库图片并只回传主键，不取回图片字节（发送时由服务端读取）。 */
export const LibraryImageChooser = ({ open, onClose, onSelect }: LibraryImageChooserProps) => (
  <ImagePickerDialog
    open={open}
    maxSelect={1}
    onClose={onClose}
    onConfirm={/* images 是按选择顺序排列的所选图片，单选时取第一张。 */ images => { if (images[0]) onSelect({ id: images[0].id, filename: images[0].filename }); }}
  />
);

export default LibraryImageChooser;
