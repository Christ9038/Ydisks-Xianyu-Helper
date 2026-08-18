// common 公开分页、错误和跨领域基础传输契约；具体 feature 应优先导入自己的领域模块。
/** 跨 feature 的最小 HTTP 传输 DTO，不得承载领域专有类型。 */
export type { ApiErrorResponse, Item, NotificationChannel, PaginatedResponse, SystemSettings } from './transport';
