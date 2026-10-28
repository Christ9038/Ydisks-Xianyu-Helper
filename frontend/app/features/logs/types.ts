/** RuntimeLogLevel 是运行日志的稳定等级名称，与服务端契约枚举一致。 */
export type RuntimeLogLevel = 'debug' | 'info' | 'warn' | 'error';

/** RuntimeLogSource 是运行日志的来源分类，与服务端契约枚举一致。 */
export type RuntimeLogSource = 'http' | 'mtop' | 'automation' | 'engine' | 'notify' | 'ws' | 'renewal' | 'other';

/** RuntimeLogDetail 是单条运行日志的附加属性，值已由服务端完成脱敏。 */
export interface RuntimeLogDetail {
  /** key 是属性键；分组属性使用点号连接的完整前缀。 */
  key: string;
  /** value 是已脱敏的属性展示文本。 */
  value: string;
}

/** RuntimeLogEntry 是页面展示的单条运行日志。 */
export interface RuntimeLogEntry {
  /** seq 是服务端记录序号，同时作为增量读取游标。 */
  seq: number;
  /** time 是记录产生时间的原始 RFC3339 文本，展示时再本地化。 */
  time: string;
  /** level 是记录等级。 */
  level: RuntimeLogLevel;
  /** source 是记录来源分类。 */
  source: RuntimeLogSource;
  /** message 是记录消息。 */
  message: string;
  /** method 是 HTTP 请求方法；非 HTTP 来源为空字符串。 */
  method: string;
  /** path 是 HTTP 请求路径或平台接口路径；缺失时为空字符串。 */
  path: string;
  /** status 是 HTTP 响应状态码；缺失时为零。 */
  status: number;
  /** durationMs 是请求或调用耗时毫秒数；缺失时为零。 */
  durationMs: number;
  /** requestId 是服务端请求关联标识；缺失时为空字符串。 */
  requestId: string;
  /** api 是平台接口名称；非平台调用为空字符串。 */
  api: string;
  /** category 是平台失败分类；非平台失败为空字符串。 */
  category: string;
  /** ret 是平台返回标记摘要；缺失时为空字符串。 */
  ret: string;
  /** error 是已脱敏的错误文本；无错误时为空字符串。 */
  error: string;
  /** accountId 是记录关联的账号标识；缺失时为空字符串。 */
  accountId: string;
  /** itemId 是记录关联的商品标识；缺失时为空字符串。 */
  itemId: string;
  /** orderId 是记录关联的订单标识；缺失时为空字符串。 */
  orderId: string;
  /** chatId 是记录关联的会话标识；缺失时为空字符串。 */
  chatId: string;
  /** caller 是写出该日志的源码位置（相对 internal 的文件路径:行号）；缺失时为空字符串。 */
  caller: string;
  /** func 是写出该日志的包名.方法名；缺失时为空字符串。 */
  func: string;
  /** route 是 HTTP 请求匹配的路由模板；缺失时为空字符串。 */
  route: string;
  /** runId 是记录关联的自动化运行标识；缺失时为空字符串。 */
  runId: string;
  /** details 是未升为具名字段的其余附加属性。 */
  details: RuntimeLogDetail[];
}

/** RuntimeLogQuery 是运行日志的来源、等级和关键字过滤条件。 */
export interface RuntimeLogQuery {
  /** level 是最低等级过滤；空字符串表示不过滤。 */
  level: RuntimeLogLevel | '';
  /** source 是来源过滤；空字符串表示不过滤。 */
  source: RuntimeLogSource | '';
  /** keyword 是消息与关键属性文本的包含过滤；空字符串表示不过滤。 */
  keyword: string;
}

/** RuntimeLogPage 是一次增量查询返回的日志页与缓冲区边界。 */
export interface RuntimeLogPage {
  /** entries 是本次返回的记录，按序号升序排列。 */
  entries: RuntimeLogEntry[];
  /** nextCursor 是下一次增量读取应使用的排他游标。 */
  nextCursor: number;
  /** oldestSeq 是服务端当前最早记录的序号；没有记录时为零。 */
  oldestSeq: number;
  /** dropped 是服务进程启动以来被覆盖的累计记录数。 */
  dropped: number;
  /** capacity 是服务端可保留的最大记录数；零表示服务端未启用日志缓冲。 */
  capacity: number;
}

/** RuntimeLogContextItem 是日志行上展示的一个键值标签。 */
export interface RuntimeLogContextItem {
  /** label 是标签名称，例如"接口"或"状态"。 */
  label: string;
  /** value 是标签值，已由服务端完成脱敏。 */
  value: string;
}
