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

type stubProfileMTop struct {
	mtop.Client
	profile func(context.Context, string) (*mtop.UserProfileResult, error)
}

func (s *stubProfileMTop) FetchUserProfile(ctx context.Context, cookies string) (*mtop.UserProfileResult, error) {
	return s.profile(ctx, cookies)
}

func TestRefreshAccountProfilePersistsCookieSessionOnResponseError(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()

	ctx := context.Background()
	detail, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "tk1_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "keep", Value: "yes", Domain: "www.goofish.com", Path: "/im", Secure: true},
	}
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	if err := store.Cookies.UpdateRenewalCookie(ctx, detail.ID, detail.Value, metadata, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	client := mtop.NewClient()
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	_, _, message := srv.refreshAccountProfile(ctx, detail)
	if message == "" {
		t.Fatal("无效响应应返回解析错误")
	}

	updated, err := store.Cookies.GetDetails(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Value, "rotated=fresh") {
		t.Fatalf("响应错误时仍应保存 canonical Cookie: %q", updated.Value)
	}
	updatedSnapshot, ok := cookierefresh.SnapshotFromMetadataOK(updated.MetadataJSON)
	if !ok {
		t.Fatal("响应错误时不应清除权威 Cookie Jar")
	}
	values := make(map[string]string, len(updatedSnapshot))
	for _, cookie := range updatedSnapshot {
		values[cookie.Name+"@"+cookie.Domain+cookie.Path] = cookie.Value
	}
	if values["rotated@.goofish.com/"] != "fresh" || values["keep@www.goofish.com/im"] != "yes" {
		t.Fatalf("Cookie Jar 写回不完整: %+v", updatedSnapshot)
	}
}

func TestRefreshAccountProfileKeepsFlatMockFallback(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	seedStaleCookieSnapshot(t, store, "acc1")

	srv.MTop = &stubProfileMTop{profile: func(context.Context, string) (*mtop.UserProfileResult, error) {
		return &mtop.UserProfileResult{
			Nickname:       "mock-profile",
			UpdatedCookies: "unb=123; _m_h5_tk=mockfresh_2",
		}, nil
	}}
	detail, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	srv.refreshAccountProfile(context.Background(), detail)

	requireCookieSnapshotCleared(t, store, "acc1")
	updated, err := store.Cookies.GetDetails(context.Background(), "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Value, "_m_h5_tk=mockfresh_2") {
		t.Fatalf("mock 扁平 Cookie 未沿用兼容写回: %q", updated.Value)
	}
}
