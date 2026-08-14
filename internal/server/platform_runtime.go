package server

import (
	"context"

	"xianyu-go/internal/db"
)

// loadCookiePlatformDetail 读取平台请求所需的最小 Cookie 状态，并转换为 Server 内部已有的会话适配模型。
func (s *Server) loadCookiePlatformDetail(ctx context.Context, cookieID string) (*db.CookieDetail, error) {
	// platformData 是 repository 返回的不含登录密码的平台运行视图。
	platformData, err := s.Store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	// detail 是仅供 Cookie Session 适配器使用的浅层模型，不包含用户名、密码或账号设置。
	detail := &db.CookieDetail{
		ID:           platformData.ID,
		UserID:       platformData.UserID,
		Value:        platformData.Value,
		MetadataJSON: platformData.MetadataJSON,
	}
	return detail, nil
}

// loadCookieSummaryDetail 读取账号非敏感摘要并转换为 Server 内部的兼容模型。
func (s *Server) loadCookieSummaryDetail(ctx context.Context, userID int64, cookieID string) (*db.CookieDetail, error) {
	// summary 是按当前用户与账号 ID 联合过滤得到的非敏感摘要。
	summary, err := s.Store.Cookies.GetSummaryOwned(ctx, userID, cookieID)
	if err != nil {
		return nil, err
	}
	// detail 是只包含摘要字段的兼容模型，不携带任何 Cookie、metadata 或登录密码。
	detail := &db.CookieDetail{
		ID:            summary.ID,
		UserID:        summary.UserID,
		AutoConfirm:   summary.AutoConfirm,
		Remark:        summary.Remark,
		PauseDuration: summary.PauseDuration,
		PausedUntil:   summary.PausedUntil,
		Username:      summary.Username,
		ShowBrowser:   summary.ShowBrowser,
		Nickname:      summary.Nickname,
		AvatarURL:     summary.AvatarURL,
		LastRefreshAt: summary.LastRefreshAt,
		LoginMethod:   summary.LoginMethod,
		LastLoginAt:   summary.LastLoginAt,
		CreatedAt:     summary.CreatedAt,
	}
	return detail, nil
}
