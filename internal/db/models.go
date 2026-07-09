package db

// User 对应 users 表。
type User struct {
	ID           int64
	Username     string
	Email        string
	PasswordHash string
	IsActive     bool
	IsAdmin      bool
	CreatedAt    string
	UpdatedAt    string
}

// Session 对应 sessions 表（HttpOnly Cookie 会话）。
type Session struct {
	SessionID string
	UserID    int64
	Username  string
	IsAdmin   bool
	ExpiresAt int64
	CreatedAt int64
}

// CookieDetail 对应 cookies 表的完整行（get_cookie_details）。
type CookieDetail struct {
	ID            string
	Value         string
	UserID        int64
	AutoConfirm   bool
	Remark        string
	PauseDuration int
	Username      string
	Password      string
	ShowBrowser   bool
	Nickname      string
	AvatarURL     string
	MetadataJSON  string
	LastRefreshAt int64
	LoginMethod   string
	LastLoginAt   int64
	CreatedAt     string
}

// AccountLoginLog 记录账号登录/续登尝试。
type AccountLoginLog struct {
	ID                 int64
	CookieID           string
	UserID             int64
	OwnerID            int64
	AccountPK          int64
	AccountIdentifier  string
	Username           string
	Method             string
	Status             string
	Message            string
	TriggerReason      string
	FailureReason      string
	ErrorMessage       string
	UpdatedCookieNames string
	DurationMS         int64
	CreatedAt          int64
}
