import type { Dispatch, SetStateAction } from 'react';
import type { Card, CardAppendResponse, CardBatchResponse } from '../../../types';

// AddCardForm 描述新增卡密组弹窗中的可编辑字段。
export interface AddCardForm {
  // name 是卡密组名称。
  name: string;
  // type 是卡密交付类型。
  type: Card['type'];
  // content 是当前类型对应的主要内容。
  content: string;
  // description 是卡密组说明。
  description: string;
  // enabled 表示新建后是否启用。
  enabled: boolean;
  // delay_seconds 是自动发货延迟秒数。
  delay_seconds: number;
  // api_method 是 API 卡密请求方法。
  api_method: 'GET' | 'POST';
  // api_timeout 是 API 请求超时时间。
  api_timeout: number;
  // api_headers 是 API 请求头 JSON 文本。
  api_headers: string;
  // api_params 是 API 请求参数 JSON 文本。
  api_params: string;
}

// EditCardForm 描述编辑卡密组时的表单字段和 API 扩展字段。
export type EditCardForm = Partial<Card> & {
  // api_url 是编辑中的 API 地址。
  api_url?: string;
  // api_method 是编辑中的 API 请求方法。
  api_method?: 'GET' | 'POST';
  // api_timeout 是编辑中的 API 超时时间。
  api_timeout?: number;
  // api_headers 是编辑中的 API 请求头 JSON 文本。
  api_headers?: string;
  // api_params 是编辑中的 API 请求参数 JSON 文本。
  api_params?: string;
};

// CardBatchTab 表示批量导入弹窗当前展示的操作页签。
export type CardBatchTab = 'create' | 'append';

// CardBatchResponseResult 保存包含逐行统计的批量创建响应和可选错误说明。
export interface CardBatchResponseResult extends CardBatchResponse {
  // error 是批量请求失败时的用户可见说明。
  error?: string;
}

// CardBatchError 保存没有统计数据时的批量创建错误。
export interface CardBatchError {
  // error 是批量请求失败时的用户可见说明。
  error: string;
}

// CardBatchResult 保存批量创建接口返回的成功、失败统计或错误说明。
export type CardBatchResult = CardBatchResponseResult | CardBatchError | null;

// CardAppendResult 保存批量追加接口返回的新增数量。
export type CardAppendResult = Pick<CardAppendResponse, 'added'> | null;

// CardBatchState 描述批量创建和追加弹窗的全部短暂 UI 状态。
export interface CardBatchState {
  // showBatchModal 表示批量操作弹窗是否打开。
  showBatchModal: boolean;
  // setShowBatchModal 更新弹窗展示状态。
  setShowBatchModal: Dispatch<SetStateAction<boolean>>;
  // batchTab 表示当前是批量创建还是追加库存。
  batchTab: CardBatchTab;
  // setBatchTab 切换批量操作页签。
  setBatchTab: Dispatch<SetStateAction<CardBatchTab>>;
  // batchFile 是待上传的批量文件。
  batchFile: File | null;
  // setBatchFile 更新待上传文件。
  setBatchFile: Dispatch<SetStateAction<File | null>>;
  // batchResult 保存最近一次批量创建结果。
  batchResult: CardBatchResult;
  // batchBusy 表示批量创建或追加请求正在执行。
  batchBusy: boolean;
  // appendTargetId 是当前追加库存目标卡密组 ID。
  appendTargetId: string;
  // setAppendTargetId 切换追加库存目标卡密组。
  setAppendTargetId: Dispatch<SetStateAction<string>>;
  // appendContent 是待追加的原始卡密文本。
  appendContent: string;
  // setAppendContent 更新待追加卡密文本。
  setAppendContent: Dispatch<SetStateAction<string>>;
  // appendResult 保存最近一次追加成功的数量。
  appendResult: CardAppendResult;
  // appendError 保存追加失败说明，供用户重试。
  appendError: string;
  // appendPreview 是按行过滤空白后的追加预览。
  appendPreview: string[];
  // openBatchModal 重置批量状态并打开弹窗。
  openBatchModal: () => void;
  // closeBatchModal 关闭弹窗并取消未完成的批量请求。
  closeBatchModal: () => void;
  // handleBatchCreate 执行批量创建请求。
  handleBatchCreate: () => Promise<void>;
  // handleBatchAppend 执行批量追加请求。
  handleBatchAppend: () => Promise<void>;
  // handleRetryBatchCreate 重试最近一次批量创建请求。
  handleRetryBatchCreate: () => Promise<void>;
  // handleRetryBatchAppend 重试最近一次追加请求。
  handleRetryBatchAppend: () => Promise<void>;
}

// CardBatchModalProps 描述批量导入/追加组件的状态和动作边界。
export interface CardBatchModalProps extends CardBatchState {
  // dataCards 是可追加库存的 data 类型卡密组。
  dataCards: Card[];
  // downloadCardTemplate 下载批量创建模板。
  downloadCardTemplate: () => void;
}
