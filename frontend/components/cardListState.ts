// 旧目录保留兼容入口，实际卡密筛选和批量状态逻辑已归入 cards feature。
export {
  canSubmitAppend,
  filterCards,
  isCurrentCardRequest,
  previewAppendContent,
} from '../app/features/cards/batchState';
