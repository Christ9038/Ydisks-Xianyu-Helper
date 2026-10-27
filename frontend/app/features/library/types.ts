import type { ComponentType } from 'react';

/** LibraryImage 是图片库中一张图片的 UI 模型，字节通过内容地址按需加载。 */
export interface LibraryImage {
  /** id 是图片主键。 */
  id: number;
  /** filename 是上传时的原始文件名，仅用于展示。 */
  filename: string;
  /** contentType 是服务端嗅探出的图片类型。 */
  contentType: string;
  /** sizeBytes 是图片字节数。 */
  sizeBytes: number;
  /** createdAt 是入库时间。 */
  createdAt: string;
}

/** LibraryUploadResult 是一次上传中单个文件的处理结果。 */
export interface LibraryUploadResult {
  /** filename 是被处理的文件名。 */
  filename: string;
  /** duplicate 表示图片已存在，复用了原记录。 */
  duplicate: boolean;
  /** error 是失败原因，成功时为空串。 */
  error: string;
  /** image 是成功入库或复用的图片。 */
  image: LibraryImage | null;
}

/** LibraryActionType 是模板自动化动作类型。 */
export type LibraryActionType = 'send_card' | 'send_template';

/** LibraryActionBinding 把发货模板的一个卡密变量绑定到卡密组。 */
export interface LibraryActionBinding {
  /** variableKey 是发货模板中的卡密变量名。 */
  variableKey: string;
  /** cardId 是绑定的卡密组。 */
  cardId: number;
  /** deliveryCount 是每单发放的数量。 */
  deliveryCount: number;
}

/** LibraryAction 是模板自动化中的一个发卡或发货模板动作。 */
export interface LibraryAction {
  /** actionType 决定使用卡密组还是发货模板。 */
  actionType: LibraryActionType;
  /** cardId 是发卡动作使用的卡密组。 */
  cardId: number;
  /** deliveryCount 是发卡数量。 */
  deliveryCount: number;
  /** delaySeconds 是发送前延迟秒数。 */
  delaySeconds: number;
  /** deliveryTemplateId 是发货模板动作使用的模板。 */
  deliveryTemplateId: number;
  /** templateBindings 是发货模板卡密变量绑定。 */
  templateBindings: LibraryActionBinding[];
  /** customVariables 是发货模板自定义变量取值。 */
  customVariables: Record<string, string>;
  /** specName 是规范化后的规格名列，单规格商品为空。 */
  specName: string;
  /** specValue 是规范化后的规格值列，单规格商品为空。 */
  specValue: string;
}

/** LibraryActionGroup 是一类自动化（付款发货或评价赠品）的开关与动作。 */
export interface LibraryActionGroup {
  /** enabled 表示是否启用。 */
  enabled: boolean;
  /** actions 是动作列表。 */
  actions: LibraryAction[];
}

/** LibraryReviewRequest 是求评价配置。 */
export interface LibraryReviewRequest {
  /** enabled 表示是否启用。 */
  enabled: boolean;
  /** afterShippedHours 是发货后多少小时发送。 */
  afterShippedHours: number;
  /** message 是求评价消息。 */
  message: string;
  /** maxAttempts 是最多发送次数。 */
  maxAttempts: number;
  /** delaySeconds 是发送前延迟秒数。 */
  delaySeconds: number;
}

/** LibraryAutomation 是模板携带的发布后自动化。 */
export interface LibraryAutomation {
  /** paidDelivery 是付款后自动发货。 */
  paidDelivery: LibraryActionGroup;
  /** reviewGift 是评价赠品。 */
  reviewGift: LibraryActionGroup;
  /** reviewRequest 是求评价。 */
  reviewRequest: LibraryReviewRequest;
}

/** LibrarySpecValue 是规格值及其图片库图片。 */
export interface LibrarySpecValue {
  /** valueId 是草稿内稳定的规格值标识。 */
  valueId: string;
  /** value 是规格值文本。 */
  value: string;
  /** imageId 是规格值图片的图片库主键，0 表示没有图片。 */
  imageId: number;
}

/** LibrarySpec 是一个规格维度。 */
export interface LibrarySpec {
  /** specId 是草稿内稳定的规格维度标识。 */
  specId: string;
  /** name 是规格名称。 */
  name: string;
  /** supportImage 表示规格值是否可以带图。 */
  supportImage: boolean;
  /** values 是规格值。 */
  values: LibrarySpecValue[];
}

/** LibrarySku 是一个规格组合的价格与库存。 */
export interface LibrarySku {
  /** valueLabels 是按规格维度顺序排列的规格值。 */
  valueLabels: string[];
  /** price 是售价（元）。 */
  price: string;
  /** quantity 是库存文本。 */
  quantity: string;
}

/** LibraryCategory 是模板的平台类目。 */
export interface LibraryCategory {
  /** catId 是平台类目标识。 */
  catId: string;
  /** catName 是类目名称。 */
  catName: string;
  /** channelCatId 是频道类目标识。 */
  channelCatId: string;
  /** tbCatId 是淘宝类目标识。 */
  tbCatId: string;
}

/** ProductTemplate 是已保存的商品模板 UI 模型。 */
export interface ProductTemplate {
  /** id 是模板主键。 */
  id: number;
  /** name 是模板名称。 */
  name: string;
  /** title 是商品标题。 */
  title: string;
  /** description 是商品描述。 */
  description: string;
  /** price 是售价（元）。 */
  price: string;
  /** originalPrice 是原价（元），空串表示未设置。 */
  originalPrice: string;
  /** quantity 是库存。 */
  quantity: number;
  /** postageMode 是邮费模式。 */
  postageMode: 'free' | 'distance' | 'fixed' | 'none';
  /** postage 是固定邮费（元）。 */
  postage: string;
  /** category 是平台类目。 */
  category: LibraryCategory;
  /** imageIds 是主图的图片库主键，顺序即主图顺序。 */
  imageIds: number[];
  /** specs 是规格维度，单规格为空。 */
  specs: LibrarySpec[];
  /** skus 是规格组合，单规格为空。 */
  skus: LibrarySku[];
  /** automation 是发布后自动化。 */
  automation: LibraryAutomation;
  /** updatedAt 是最近更新时间。 */
  updatedAt: string;
}

/** LibraryCardOption 是自动发货可选的卡密组。 */
export interface LibraryCardOption {
  /** id 是卡密组主键。 */
  id: number;
  /** name 是卡密组名称。 */
  name: string;
  /** type 是卡密类型。 */
  type: string;
}

/** LibraryDeliveryTemplateOption 是自动发货可选的发货模板。 */
export interface LibraryDeliveryTemplateOption {
  /** id 是模板主键。 */
  id: number;
  /** name 是模板名称。 */
  name: string;
  /** keys 是模板中的卡密变量名。 */
  keys: string[];
  /** customKeys 是模板中的自定义变量名。 */
  customKeys: string[];
}

/** LibraryAccountOption 是发布时可选的账号。 */
export interface LibraryAccountOption {
  /** id 是账号标识。 */
  id: string;
  /** label 是展示名称。 */
  label: string;
}

/** LibraryPublishPreviewRow 是发布预检中的一行。 */
export interface LibraryPublishPreviewRow {
  /** rowNo 是行号。 */
  rowNo: number;
  /** valid 表示该行是否可发布。 */
  valid: boolean;
  /** errors 是该行的校验错误。 */
  errors: string[];
  /** cookieId 是目标账号。 */
  cookieId: string;
  /** title 是商品标题。 */
  title: string;
  /** price 是售价。 */
  price: string;
  /** quantity 是库存。 */
  quantity: number;
}

/** LibraryPublishPreview 是发布预检结果。 */
export interface LibraryPublishPreview {
  /** previewId 是确认发布时使用的预检标识。 */
  previewId: string;
  /** total 是总行数。 */
  total: number;
  /** valid 是可发布行数。 */
  valid: number;
  /** invalid 是不可发布行数。 */
  invalid: number;
  /** rows 是预检明细。 */
  rows: LibraryPublishPreviewRow[];
}

/** LibraryPublishLocation 是发布使用的发货地。 */
export interface LibraryPublishLocation {
  /** province 是省。 */
  province: string;
  /** city 是市。 */
  city: string;
  /** area 是区县。 */
  area: string;
  /** divisionId 是行政区划标识。 */
  divisionId: string;
}

/** LibraryImagePickerProps 是图片选择器组件的属性，应用壳把它注入到商品发布页。 */
export interface LibraryImagePickerProps {
  /** open 表示是否显示。 */
  open: boolean;
  /** maxSelect 是最多可选张数。 */
  maxSelect: number;
  /** onClose 关闭选择器。 */
  onClose: () => void;
  /** onPick 返回所选图片转换成的文件。 */
  onPick: (files: File[]) => void;
}

/** LibraryImagePickerComponent 是应用壳注入商品页的图片选择器组件类型。 */
export type LibraryImagePickerComponent = ComponentType<LibraryImagePickerProps>;
