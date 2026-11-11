/** defaultPublishIntervalSeconds 是发布间隔为 0 或无效时服务端使用的默认间隔，与批量铺货一致。 */
const defaultPublishIntervalSeconds = 5;

/**
 * estimatePublishSeconds 估算一批商品排队发布的最短耗时（秒）：批次逐行串行发布，相邻两次发布至少间隔 intervalText 秒。
 * rows 是“模板 × 账号”行数；intervalText 是用户输入的间隔文本，0、空或非法值按服务端默认 5 秒计。
 * 实际耗时还包含图片上传与平台响应时间，因此这是下限。
 */
export const estimatePublishSeconds = (rows: number, intervalText: string): number => {
  if (rows <= 1) return 0;
  // interval 是生效的发布间隔秒数。
  const interval = Number.parseInt(intervalText, 10);
  return (rows - 1) * (Number.isFinite(interval) && interval > 0 ? interval : defaultPublishIntervalSeconds);
};

/** formatDuration 把秒数格式化为“约 N 秒 / 分钟 / 小时 M 分钟”的中文文本，用于提示排队时长。 */
export const formatDuration = (seconds: number): string => {
  if (seconds < 60) return `约 ${seconds} 秒`;
  if (seconds < 3600) return `约 ${Math.ceil(seconds / 60)} 分钟`;
  // hours、minutes 是整小时数与剩余分钟数。
  const hours = Math.floor(seconds / 3600);
  // minutes 是不足一小时的剩余分钟数，向上取整。
  const minutes = Math.ceil((seconds % 3600) / 60);
  return minutes > 0 ? `约 ${hours} 小时 ${minutes} 分钟` : `约 ${hours} 小时`;
};
