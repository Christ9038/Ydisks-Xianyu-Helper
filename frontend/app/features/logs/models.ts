import type { RuntimeLogContextItem, RuntimeLogEntry, RuntimeLogLevel, RuntimeLogQuery, RuntimeLogSource } from './types';

/** RuntimeLogLevelOption 是等级过滤下拉的一个选项。 */
interface RuntimeLogLevelOption {
  /** value 是选项对应的等级取值。 */
  value: RuntimeLogLevel;
  /** label 是选项展示文案。 */
  label: string;
}

/** RuntimeLogSourceOption 是来源过滤下拉的一个选项。 */
interface RuntimeLogSourceOption {
  /** value 是选项对应的来源取值。 */
  value: RuntimeLogSource;
  /** label 是选项展示文案。 */
  label: string;
}

/** RUNTIME_LOG_LEVEL_OPTIONS 是等级过滤下拉使用的稳定选项集合，顺序从最详细到最严格。 */
export const RUNTIME_LOG_LEVEL_OPTIONS: ReadonlyArray<RuntimeLogLevelOption> = [
  { value: 'debug', label: 'Debug 及以上' },
  { value: 'info', label: 'Info 及以上' },
  { value: 'warn', label: 'Warn 及以上' },
  { value: 'error', label: '仅 Error' },
];

/** RUNTIME_LOG_SOURCE_OPTIONS 是来源过滤下拉使用的稳定选项集合，与后端来源分类一一对应。 */
export const RUNTIME_LOG_SOURCE_OPTIONS: ReadonlyArray<RuntimeLogSourceOption> = [
  { value: 'http', label: '内部接口' },
  { value: 'mtop', label: '闲鱼接口' },
  { value: 'automation', label: '自动化' },
  { value: 'engine', label: '消息引擎' },
  { value: 'ws', label: 'WebSocket' },
  { value: 'renewal', label: '凭证续期' },
  { value: 'notify', label: '通知投递' },
  { value: 'other', label: '其他' },
];

/** RUNTIME_LOG_LEVEL_LABELS 是等级徽标文本，同时作为等级取值的白名单。 */
const RUNTIME_LOG_LEVEL_LABELS: Readonly<Record<RuntimeLogLevel, string>> = {
  debug: 'DEBUG',
  info: 'INFO',
  warn: 'WARN',
  error: 'ERROR',
};

/** RUNTIME_LOG_SOURCE_LABELS 是来源徽标文本，同时作为来源取值的白名单。 */
const RUNTIME_LOG_SOURCE_LABELS: Readonly<Record<RuntimeLogSource, string>> = {
  http: '内部接口',
  mtop: '闲鱼接口',
  automation: '自动化',
  engine: '消息引擎',
  ws: 'WebSocket',
  renewal: '凭证续期',
  notify: '通知投递',
  other: '其他',
};

/** normalizeRuntimeLogLevel 把服务端等级文本归一化为页面等级；未知或空值回退为 info。 */
export const normalizeRuntimeLogLevel = (raw: string): RuntimeLogLevel => {
  // normalized 是去除首尾空白并统一小写后的等级文本。
  const normalized = raw.trim().toLowerCase();
  return Object.prototype.hasOwnProperty.call(RUNTIME_LOG_LEVEL_LABELS, normalized) ? (normalized as RuntimeLogLevel) : 'info';
};

/** normalizeRuntimeLogSource 把服务端来源文本归一化为页面来源；未知或空值归入 other。 */
export const normalizeRuntimeLogSource = (raw: string): RuntimeLogSource => {
  // normalized 是去除首尾空白并统一小写后的来源文本。
  const normalized = raw.trim().toLowerCase();
  return Object.prototype.hasOwnProperty.call(RUNTIME_LOG_SOURCE_LABELS, normalized) ? (normalized as RuntimeLogSource) : 'other';
};

/** runtimeLogLevelLabel 返回等级徽标文本。 */
export const runtimeLogLevelLabel = (level: RuntimeLogLevel): string => RUNTIME_LOG_LEVEL_LABELS[level];

/** runtimeLogSourceLabel 返回来源徽标文本。 */
export const runtimeLogSourceLabel = (source: RuntimeLogSource): string => RUNTIME_LOG_SOURCE_LABELS[source];

/** runtimeLogLevelRowClass 返回日志行左侧色条与底色，使错误行一眼可辨。 */
export const runtimeLogLevelRowClass = (level: RuntimeLogLevel): string => {
  if (level === 'error') return 'border-l-red-400 bg-red-50/40';
  if (level === 'warn') return 'border-l-amber-400 bg-amber-50/30';
  if (level === 'info') return 'border-l-sky-300';
  return 'border-l-slate-200';
};

/** runtimeLogLevelBadgeClass 返回等级徽标配色。 */
export const runtimeLogLevelBadgeClass = (level: RuntimeLogLevel): string => {
  if (level === 'error') return 'bg-red-100 text-red-700';
  if (level === 'warn') return 'bg-amber-100 text-amber-700';
  if (level === 'info') return 'bg-sky-100 text-sky-700';
  return 'bg-slate-100 text-slate-500';
};

/** formatRuntimeLogTime 把服务端 RFC3339 时间转换为本地可读文本；无效输入返回空字符串。 */
export const formatRuntimeLogTime = (raw: string): string => {
  // parsed 是解析后的本地时间对象。
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return '';
  // pad 把单个时间片段补齐为两位数字。
  const pad = (value: number): string => String(value).padStart(2, '0');
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}:${pad(parsed.getSeconds())}`;
};

/** runtimeLogHeadline 生成日志行主标题，优先展示平台接口名或内部请求目标。 */
export const runtimeLogHeadline = (entry: RuntimeLogEntry): string => {
  if (entry.api) return entry.api;
  if (entry.path) return entry.method ? `${entry.method} ${entry.path}` : entry.path;
  return entry.message;
};

/** runtimeLogContextItems 生成日志行键值标签，只输出确实有值的字段。 */
export const runtimeLogContextItems = (entry: RuntimeLogEntry): RuntimeLogContextItem[] => {
  // items 保存按固定顺序生成的标签集合。
  const items: RuntimeLogContextItem[] = [];
  if (entry.method && entry.path) items.push({ label: '请求', value: `${entry.method} ${entry.path}` });
  if (entry.route) items.push({ label: '路由', value: entry.route });
  if (entry.status > 0) items.push({ label: '状态', value: String(entry.status) });
  if (entry.durationMs > 0) items.push({ label: '耗时', value: `${entry.durationMs}ms` });
  if (entry.category) items.push({ label: '失败分类', value: entry.category });
  if (entry.ret) items.push({ label: '平台返回', value: entry.ret });
  if (entry.accountId) items.push({ label: '账号', value: entry.accountId });
  if (entry.itemId) items.push({ label: '商品', value: entry.itemId });
  if (entry.orderId) items.push({ label: '订单', value: entry.orderId });
  if (entry.chatId) items.push({ label: '会话', value: entry.chatId });
  if (entry.runId) items.push({ label: '运行', value: entry.runId });
  if (entry.requestId) items.push({ label: '请求标识', value: entry.requestId });
  return items;
};

/** runtimeLogQuerySignature 生成过滤条件签名，供日志流在条件变化时重置游标。 */
export const runtimeLogQuerySignature = (query: RuntimeLogQuery): string => `${query.level}|${query.source}|${query.keyword.trim()}`;

/** mergeRuntimeLogEntries 按序号合并增量日志并只保留最近 capacity 条，避免重复与无限增长。 */
export const mergeRuntimeLogEntries = (
  previous: RuntimeLogEntry[],
  incoming: RuntimeLogEntry[],
  capacity: number,
): RuntimeLogEntry[] => {
  if (incoming.length === 0) return previous;
  // knownSeqs 保存已展示记录的序号集合，用于丢弃重复增量。
  const knownSeqs = new Set(previous.map(/* entry 是已展示的一条日志。 */ entry => entry.seq));
  // appended 保存本轮新增且尚未展示过的记录。
  const appended = incoming.filter(/* entry 是本轮返回的一条日志。 */ entry => !knownSeqs.has(entry.seq));
  if (appended.length === 0) return previous;
  // merged 是按序号顺序拼接后的完整列表。
  const merged = [...previous, ...appended];
  // limit 是页面保留的最大条数；服务端未提供容量时不做裁剪。
  const limit = capacity > 0 ? capacity : merged.length;
  return merged.length > limit ? merged.slice(merged.length - limit) : merged;
};

/** runtimeLogRetentionHint 生成缓冲区保留提示；无需提示时返回空字符串。 */
export const runtimeLogRetentionHint = (capacity: number, dropped: number): string => {
  if (capacity <= 0) return '服务端未启用运行日志缓冲，请确认服务端已按当前版本启动。';
  if (dropped > 0) return `服务端只保留最近 ${capacity} 条日志，已有 ${dropped} 条更早记录被覆盖。`;
  return '';
};
