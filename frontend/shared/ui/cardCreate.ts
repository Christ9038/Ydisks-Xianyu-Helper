import type { ComponentType } from 'react';
import type { LibraryImageChooserComponent } from './imageSource';

/** CreatedCard 是新建卡密成功后回传给调用方的最小信息，不含任何库存内容或密钥。 */
export interface CreatedCard {
  /** id 是服务端返回的新卡密组主键。 */
  id: number;
  /** name 是用户填写的卡密组名称。 */
  name: string;
}

/** CardCreateDialogProps 是应用壳注入各功能页的“新建卡密”弹窗属性。 */
export interface CardCreateDialogProps {
  /** onClose 关闭弹窗并丢弃草稿；创建成功后由弹窗在 onCreated 之后不再主动调用。 */
  onClose: () => void;
  /** onCreated 在创建成功后回传新卡密；调用方负责刷新选项并关闭弹窗。 */
  onCreated: (card: CreatedCard) => void;
  /** libraryImageChooser 是可选的图片库选择器；缺省时图片卡密只能填写外链。 */
  libraryImageChooser?: LibraryImageChooserComponent;
}

/** CardCreateDialogComponent 是应用壳按需注入的新建卡密弹窗组件类型。 */
export type CardCreateDialogComponent = ComponentType<CardCreateDialogProps>;
