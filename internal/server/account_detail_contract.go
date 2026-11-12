package server

// cookieDetailResponse 是单个账号非敏感详情接口的具名响应 DTO。
type cookieDetailResponse struct {
	// ID 是账号稳定标识。
	ID string `json:"id"`
	// Enabled 表示账号是否允许运行。
	Enabled bool `json:"enabled"`
	// AutoConfirm 表示账号是否开启自动发货总开关（库字段 auto_confirm，付款后自动发卡密或发货模板）；不是确认发货开关。
	AutoConfirm bool `json:"auto_confirm"`
	// AutoConsign 表示自动发货后是否自动转已发货。
	AutoConsign bool `json:"auto_consign"`
	// AutoBargain 表示砍价“待刀成”阶段是否自动免拼。
	AutoBargain bool `json:"auto_bargain"`
	// ConsignProofMode 是确认发货携带凭证的模式：delivery 卡密/模板全文，fixed 固定文字，none 不携带。
	ConsignProofMode string `json:"consign_proof_mode"`
	// ConsignProofText 是固定文字模式下提交给平台的用户自填文字。
	ConsignProofText string `json:"consign_proof_text"`
	// ManualTakeoverMinutes 是人工回复后会话暂停自动回复的分钟数；0 表示关闭。
	ManualTakeoverMinutes int `json:"manual_takeover_minutes"`
	// ManualTakeoverScope 是人工介入暂停范围：ai 只暂停 Agent 与 AI，all 暂停全部自动回复。
	ManualTakeoverScope string `json:"manual_takeover_scope"`
	// Remark 是账号备注。
	Remark string `json:"remark"`
	// PauseDuration 是暂停时长，单位为分钟。
	PauseDuration int `json:"pause_duration"`
	// PausedUntil 是暂停结束时间的 Unix 秒。
	PausedUntil int64 `json:"paused_until"`
	// Paused 表示账号当前是否处于暂停状态。
	Paused bool `json:"paused"`
	// ShowBrowser 表示密码登录是否允许显示浏览器。
	ShowBrowser bool `json:"show_browser"`
	// Username 是登录用户名，不包含登录密码。
	Username string `json:"username"`
	// Nickname 是平台账号昵称缓存。
	Nickname string `json:"nickname"`
	// AvatarURL 是平台账号头像地址。
	AvatarURL string `json:"avatar_url"`
	// LoginMethod 是最近一次成功登录方式。
	LoginMethod string `json:"login_method"`
	// LastLoginAt 是最近一次成功登录时间。
	LastLoginAt int64 `json:"last_login_at"`
	// ProfileError 是资料刷新错误说明。
	ProfileError string `json:"profile_error"`
	// HasCookie 表示数据库中存在可用账号记录。
	HasCookie bool `json:"has_cookie"`
	// AutoRateEnabled 表示自动评价计划是否启用。
	AutoRateEnabled bool `json:"auto_rate_enabled"`
	// RateContent 是自动评价文案。
	RateContent string `json:"rate_content"`
	// AutoPolishEnabled 表示自动擦亮计划是否启用。
	AutoPolishEnabled bool `json:"auto_polish_enabled"`
	// PolishTime 是自动擦亮的本地时间。
	PolishTime string `json:"polish_time"`
	// LastRateScanAt 是最近一次自动评价扫描时间。
	LastRateScanAt int64 `json:"last_rate_scan_at"`
	// LastPolishDate 是最近一次自动擦亮日期。
	LastPolishDate string `json:"last_polish_date"`
	// LastPolishAt 是最近一次自动擦亮时间。
	LastPolishAt int64 `json:"last_polish_at"`
}
