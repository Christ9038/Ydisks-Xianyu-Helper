import type { Dispatch, SetStateAction } from 'react';
import type { Item, AccountDetail, ShippingRule } from '../../../shared/api-contract';
import type { PublishLocation } from './api';

// ItemListProps 描述商品页面从父级接收的规则配置回调。
export interface ItemListProps {
  // onConfigureDelivery 打开指定商品的自动化发货规则编辑器。
  onConfigureDelivery: (item: Item) => void;
}

// BatchPhase 表示批量铺货流程当前所在的步骤。
export type BatchPhase = 'upload' | 'preview' | 'running' | 'done';

// PublishCategory 表示批量铺货行使用的类目映射。
export interface PublishCategory {
  // cat_id 是闲鱼类目标识。
  cat_id: string;
  // cat_name 是类目展示名称。
  cat_name: string;
  // channel_cat_id 是闲鱼频道类目标识。
  channel_cat_id?: string;
  // tb_cat_id 是可选的淘宝类目标识。
  tb_cat_id?: string;
}

// PublishBatchPreviewRow 表示批量预检返回的一行商品。
export interface PublishBatchPreviewRow {
  // row_no 是表格中的原始行号。
  row_no: number;
  // valid 表示该行是否可以进入发布阶段。
  valid: boolean;
  // errors 是该行的校验错误列表。
  errors?: string[];
  // cookie_id 是该行最终使用的账号标识。
  cookie_id: string;
  // title 是该行商品标题。
  title: string;
  // price 是该行商品价格。
  price: string;
  // quantity 是该行商品库存数量。
  quantity: number;
  // images 是该行解析出的图片地址。
  images: string[];
  // category 是该行最终采用的类目。
  category: PublishCategory;
}

// PublishBatchDetailRow 表示批量任务中的单行执行结果。
export interface PublishBatchDetailRow {
  // id 是持久化任务行标识。
  id: number;
  // row_no 是原始表格行号。
  row_no: number;
  // cookie_id 是该行使用的账号标识。
  cookie_id: string;
  // title 是该行商品标题。
  title: string;
  // price 是该行商品价格。
  price: string;
  // quantity 是该行商品库存数量。
  quantity: number;
  // status 是该行执行状态。
  status: string;
  // item_id 是发布成功后的商品标识。
  item_id: string;
  // item_url 是发布成功后的商品链接。
  item_url: string;
  // error_message 是该行失败原因。
  error_message: string;
  // failure_kind 是后端归类的失败类型。
  failure_kind: string;
  // images 是该行实际使用的图片地址。
  images?: string[];
  // category 是该行使用的类目。
  category: PublishCategory;
}

// PublishBatchDetail 表示批量任务的汇总和逐行结果。
export interface PublishBatchDetail {
  // id 是批量任务标识。
  id: string;
  // status 是批量任务当前状态。
  status: string;
  // filename 是上传的原始文件名。
  filename: string;
  // total 是任务总行数。
  total: number;
  // success 是成功行数。
  success: number;
  // failed 是失败行数。
  failed: number;
  // pending 是等待执行行数。
  pending: number;
  // running 是正在执行行数。
  running: number;
  // retryable 是可重试失败行数。
  retryable: number;
  // rows 是逐行执行结果。
  rows: PublishBatchDetailRow[];
}

// PublishBatchPreview 表示批量预检的汇总和逐行结果。
export interface PublishBatchPreview {
  // preview_id 是预检任务标识。
  preview_id: string;
  // total 是预检总行数。
  total: number;
  // valid 是可发布行数。
  valid: number;
  // invalid 是需要修正的行数。
  invalid: number;
  // rows 是逐行预检结果。
  rows: PublishBatchPreviewRow[];
}

// BatchFallbackCategory 表示批量预检使用的默认类目。
export interface BatchFallbackCategory {
  // catId 是默认类目 ID。
  catId: string;
  // catName 是默认类目名称。
  catName: string;
  // channelCatId 是默认频道类目 ID。
  channelCatId: string;
  // tbCatId 是默认淘宝类目 ID。
  tbCatId: string;
}

// ItemPublishBatchState 描述批量铺货 Hook 暴露的服务端和表单状态。
export interface ItemPublishBatchState {
  // showBatchModal 表示批量铺货弹窗是否打开。
  showBatchModal: boolean;
  // setShowBatchModal 控制批量铺货弹窗的显示状态。
  setShowBatchModal: Dispatch<SetStateAction<boolean>>;
  // batchLoading 表示批量任务请求是否正在执行。
  batchLoading: boolean;
  // batchPhase 是批量流程当前步骤。
  batchPhase: BatchPhase;
  // setBatchPhase 更新批量流程当前步骤。
  setBatchPhase: Dispatch<SetStateAction<BatchPhase>>;
  // batchFile 是待预检的商品表格文件。
  batchFile: File | null;
  // setBatchFile 更新待预检的商品表格。
  setBatchFile: Dispatch<SetStateAction<File | null>>;
  // batchImagesZip 是可选的商品图片压缩包。
  batchImagesZip: File | null;
  // setBatchImagesZip 更新可选的图片压缩包。
  setBatchImagesZip: Dispatch<SetStateAction<File | null>>;
  // batchCategoryKeyword 是默认类目搜索词。
  batchCategoryKeyword: string;
  // setBatchCategoryKeyword 更新默认类目搜索词。
  setBatchCategoryKeyword: Dispatch<SetStateAction<string>>;
  // batchCategoryLoading 表示默认类目推荐请求是否正在执行。
  batchCategoryLoading: boolean;
  // batchFallbackCategory 是默认类目配置。
  batchFallbackCategory: BatchFallbackCategory;
  // setBatchFallbackCategory 更新默认类目配置。
  setBatchFallbackCategory: Dispatch<SetStateAction<BatchFallbackCategory>>;
  // batchPreview 是最近一次预检结果。
  batchPreview: PublishBatchPreview | null;
  // setBatchPreview 更新最近一次预检结果。
  setBatchPreview: Dispatch<SetStateAction<ItemPublishBatchState['batchPreview']>>;
  // batchDetail 是当前批量任务详情。
  batchDetail: PublishBatchDetail | null;
  // setBatchDetail 更新当前批量任务详情。
  setBatchDetail: Dispatch<SetStateAction<PublishBatchDetail | null>>;
  // recentBatch 是页面入口处展示的最近任务。
  recentBatch: PublishBatchDetail | null;
  // setRecentBatch 更新页面入口处展示的最近任务。
  setRecentBatch: Dispatch<SetStateAction<PublishBatchDetail | null>>;
  // batchLocations 是批量任务可选的发货地列表。
  batchLocations: PublishLocation[];
  // batchLocation 是批量任务当前选择的发货地。
  batchLocation: PublishLocation | null;
  // setBatchLocations 更新批量发货地候选列表。
  setBatchLocations: Dispatch<SetStateAction<PublishLocation[]>>;
  // setBatchLocation 更新批量当前发货地。
  setBatchLocation: Dispatch<SetStateAction<PublishLocation | null>>;
  // openBatchModal 打开批量铺货流程并恢复可继续任务。
  openBatchModal: () => Promise<void>;
  // handleRecommendBatchCategory 请求默认类目推荐。
  handleRecommendBatchCategory: () => Promise<void>;
  // openRecentBatchResult 打开最近批量任务结果。
  openRecentBatchResult: () => Promise<void>;
  // handlePreviewBatch 提交文件并执行批量预检。
  handlePreviewBatch: () => Promise<void>;
  // handleStartBatch 启动预检通过的批量任务。
  handleStartBatch: () => Promise<void>;
  // handleCancelBatch 请求安全取消当前任务。
  handleCancelBatch: () => Promise<void>;
  // abandonBatchPreview 清理预检任务并回到上传步骤。
  abandonBatchPreview: () => Promise<void>;
  // closeBatchModal 关闭批量弹窗并清理临时预检任务。
  closeBatchModal: () => Promise<void>;
  // handleRetryBatchFailed 重试当前任务的失败行。
  handleRetryBatchFailed: () => Promise<void>;
}

// ItemPublishBatchOptions 描述批量 Hook 依赖的页面上下文。
export interface ItemPublishBatchOptions {
  // selectedAccount 是批量任务默认使用的账号。
  selectedAccount: string;
  // loadItems 刷新商品列表。
  loadItems: () => Promise<void>;
  // loadShippingRules 刷新商品关联规则。
  loadShippingRules: () => Promise<void>;
}

// ItemReferenceState 描述商品页面 Hook 需要的共享列表状态。
export interface ItemReferenceState {
  // items 是商品列表。
  items: Item[];
  // shippingRules 是商品关联的自动化规则。
  shippingRules: ShippingRule[];
  // accounts 是账号列表。
  accounts: AccountDetail[];
}
