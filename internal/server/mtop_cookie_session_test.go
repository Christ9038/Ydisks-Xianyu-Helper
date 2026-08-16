package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// stubProfileMTop 保存stubProfileMTop，供当前处理流程使用
type stubProfileMTop struct {
	mtop.Client
	profile func(context.Context, string) (*mtop.UserProfileResult, error)
}

// FetchUserProfile 负责Fetch用户Profile相关处理。
func (s *stubProfileMTop) FetchUserProfile(ctx context.Context, cookies string) (*mtop.UserProfileResult, error) {
	return s.profile(ctx, cookies)
}

// TestRefreshAccountProfilePersistsCookieSessionOnResponseError 负责TestRefresh账号ProfilePersists登录凭证会话On响应错误相关处理。
func TestRefreshAccountProfilePersistsCookieSessionOnResponseError(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()

	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "tk1_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "keep", Value: "yes", Domain: "www.goofish.com", Path: "/im", Secure: true},
	}
	// metadata 保存metadata，供当前处理流程使用
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateRenewalCookie(ctx, detail.ID, detail.Value, metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	// client 保存client，供当前处理流程使用
	client := mtop.NewClient()
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// header 保存header，供当前处理流程使用
		header := make(http.Header)
		header.Add("Set-Cookie", "rotated=fresh; Domain=.goofish.com; Path=/; Secure; HttpOnly")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{"ret":`)),
			Request:    req,
		}, nil
	})}
	srv.MTop = client

	detail, err = store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// profile、profileErr 保存资料应用服务返回的非敏感结果和用例错误。
	profile, profileErr := srv.accountProfileApplication().RefreshProfile(ctx, detail.UserID, detail.ID)
	// message 保存资料刷新失败时的可展示错误文本。
	message := profile.ErrorMessage
	if profileErr != nil {
		message = profileErr.Error()
	}
	if message == "" {
		t.Fatal("无效响应应返回解析错误")
	}

	// updated、err 保存updated、err，供当前处理流程使用
	updated, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Value, "rotated=fresh") {
		t.Fatalf("响应错误时仍应保存 canonical Cookie: %q", updated.Value)
	}
	// updatedSnapshot、ok 保存updatedSnapshot、ok，供当前处理流程使用
	updatedSnapshot, ok := cookierefresh.SnapshotFromMetadataOK(updated.MetadataJSON)
	if !ok {
		t.Fatal("响应错误时不应清除权威 Cookie Jar")
	}
	// values 保存values，供当前处理流程使用
	values := make(map[string]string, len(updatedSnapshot))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range updatedSnapshot {
		values[cookie.Name+"@"+cookie.Domain+cookie.Path] = cookie.Value
	}
	if values["rotated@.goofish.com/"] != "fresh" || values["keep@www.goofish.com/im"] != "yes" {
		t.Fatalf("Cookie Jar 写回不完整: %+v", updatedSnapshot)
	}
}

// TestRefreshAccountProfileKeepsAuthoritativeSnapshotWithFlatMock 负责TestRefresh账号ProfileKeepsAuthoritativeSnapshotWithFlatMock相关处理。
func TestRefreshAccountProfileKeepsAuthoritativeSnapshotWithFlatMock(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	seedStaleCookieSnapshot(t, store, "acc1")

	srv.MTop = &stubProfileMTop{profile: func(context.Context, string) (*mtop.UserProfileResult, error) {
		return &mtop.UserProfileResult{
			Nickname:       "mock-profile",
			UpdatedCookies: "unb=123; _m_h5_tk=mockfresh_2",
		}, nil
	}}
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = srv.accountProfileApplication().RefreshProfile(context.Background(), detail.UserID, detail.ID)

	// updated、err 保存updated、err，供当前处理流程使用
	updated, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updated.Value, "_m_h5_tk=mockfresh_2") {
		t.Fatalf("完整 Jar 不得被 mock 扁平 Cookie 覆盖: %q", updated.Value)
	}
	if // complete 保存complete，供当前处理流程使用
	_, complete := cookierefresh.SnapshotFromMetadataOK(updated.MetadataJSON); !complete {
		t.Fatalf("完整 Jar 未发生变化时不得清除: %s", updated.MetadataJSON)
	}
}

// TestRefreshAccountProfileKeepsFlatMockFallbackWithoutSnapshot 负责TestRefresh账号ProfileKeepsFlatMockFallbackWithoutSnapshot相关处理。
func TestRefreshAccountProfileKeepsFlatMockFallbackWithoutSnapshot(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()

	srv.MTop = &stubProfileMTop{profile: func(context.Context, string) (*mtop.UserProfileResult, error) {
		return &mtop.UserProfileResult{
			Nickname:       "mock-profile",
			UpdatedCookies: "unb=123; _m_h5_tk=mockfresh_2",
		}, nil
	}}
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = srv.accountProfileApplication().RefreshProfile(context.Background(), detail.UserID, detail.ID)

	// updated、err 保存updated、err，供当前处理流程使用
	updated, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Value, "_m_h5_tk=mockfresh_2") {
		t.Fatalf("无完整 Jar 时 mock 扁平 Cookie 未沿用兼容写回: %q", updated.Value)
	}
}
