// 卡密 feature 只通过本适配层访问库存和批量接口，页面不直接依赖共享 API 文件。
export {
  appendCardData,
  batchCreateCards,
  createCard,
  deleteCard,
  getCards,
  updateCard,
} from '../../../services/api';

// 卡密 feature 重新导出接口响应类型，保持批量结果契约稳定。
export type { CardAppendResponse, CardBatchResponse } from '../../../types';
