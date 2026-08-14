import type { Card } from '../../../types';

// filterCards 按类型和名称筛选卡密组，两个条件同时满足才保留。
export const filterCards = (
  cards: Card[],
  typeFilter: Card['type'] | '',
  nameSearch: string,
): Card[] => {
  // keyword 是去除首尾空格并转为小写的名称搜索词。
  const keyword = nameSearch.trim().toLocaleLowerCase();
  return cards.filter(
    // card 是待判断是否符合筛选条件的卡密组。
    card => (
    (!typeFilter || card.type === typeFilter)
    && (!keyword || card.name.toLocaleLowerCase().includes(keyword))
    ),
  );
};

// previewAppendContent 将追加文本转换为去除空行后的卡密预览列表。
export const previewAppendContent = (content: string): string[] => content
  .split('\n')
  .map(
    // line 是追加文本中的单行原始内容。
    line => line.trim(),
  )
  .filter(Boolean);

// canSubmitAppend 判断当前追加表单是否具备目标卡密组和有效内容。
export const canSubmitAppend = (targetId: string, content: string, busy: boolean): boolean => Boolean(
  targetId && previewAppendContent(content).length > 0 && !busy,
);

// isCurrentCardRequest 判断卡密目标切换后响应是否仍属于当前请求。
export const isCurrentCardRequest = (
  requestGeneration: number,
  currentGeneration: number,
  requestTargetId: string,
  currentTargetId: string,
): boolean => requestGeneration === currentGeneration && requestTargetId === currentTargetId;
