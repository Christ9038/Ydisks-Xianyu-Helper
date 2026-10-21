/** 前端等待预算耗尽但后台订单刷新任务仍在运行时抛出；已处理的订单页已经入库，调用方应刷新列表并提示用户稍后再看。 */
export class OrderRefreshStillRunningError extends Error {
  /** jobId 是仍在服务端运行的订单刷新任务标识。 */
  readonly jobId: string;

  /** 创建带任务标识的“仍在后台运行”错误。 */
  constructor(jobId: string) {
    super('订单同步仍在后台进行，已同步的订单已显示，请稍后刷新查看');
    this.name = 'OrderRefreshStillRunningError';
    this.jobId = jobId;
  }
}
