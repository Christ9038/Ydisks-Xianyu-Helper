package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRiskVerificationClassification 负责TestRiskVerificationClassification相关处理。
func TestRiskVerificationClassification(t *testing.T) {
	// ret 保存ret，供当前处理流程使用
	ret := []string{"FAIL_SYS_USER_VALIDATE::用户校验失败"}
	if !isRiskVerificationRet(ret) {
		t.Fatal("FAIL_SYS_USER_VALIDATE 应识别为风控")
	}
	if isTokenExpiredRet(ret) {
		t.Fatal("FAIL_SYS_USER_VALIDATE 不应再被当作普通 token 过期")
	}
	// err 保存err，供当前处理流程使用
	err := &RiskVerificationError{Ret: ret, VerificationURL: "https://passport.goofish.com/punish?x5secdata=1"}
	if !IsRiskVerificationErr(err) {
		t.Fatal("RiskVerificationError 应被识别")
	}
}

// TestRefreshTokenReturnsRiskVerificationError 负责TestRefresh令牌ReturnsRiskVerification错误相关处理。
func TestRefreshTokenReturnsRiskVerificationError(t *testing.T) {
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtk_1"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_USER_VALIDATE::用户校验失败"],"data":{"url":"https://passport.goofish.com/punish?x5secdata=1"}}`)
	}))
	defer srv.Close()

	// c 保存c，供当前处理流程使用
	c := &ClientImpl{HTTPClient: srv.Client(), TokenURL: srv.URL}
	// res、err 保存res、err，供当前处理流程使用
	res, err := c.RefreshTokenWithDeviceIDContext(context.Background(), "unb=123; _m_h5_tk=old_1", "device-1")
	if err == nil || !IsRiskVerificationErr(err) {
		t.Fatalf("应返回风控错误: res=%#v err=%v", res, err)
	}
	if res == nil || !strings.Contains(res.UpdatedCookies, "_m_h5_tk=newtk_1") {
		t.Fatalf("风控时仍应保留 Set-Cookie: %#v", res)
	}
	// riskErr 保存riskErr，供当前处理流程使用
	var riskErr *RiskVerificationError
	if !strings.Contains(err.Error(), "x5secdata") {
		t.Fatalf("风控 URL 未进入错误信息: %v", riskErr)
	}
}

// TestCheckLoginStatusTokenRefreshed 负责TestCheck登录状态令牌Refreshed相关处理。
func TestCheckLoginStatusTokenRefreshed(t *testing.T) {
	// srv 保存srv，供当前处理流程使用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtk_1"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
	}))
	defer srv.Close()

	// c 保存c，供当前处理流程使用
	c := &ClientImpl{HTTPClient: srv.Client(), LoginUserURL: srv.URL}
	// res、err 保存res、err，供当前处理流程使用
	res, err := c.CheckLoginStatusContext(context.Background(), "unb=123; _m_h5_tk=old_1")
	if err != nil {
		t.Fatalf("CheckLoginStatusContext: %v", err)
	}
	if res.Status != LoginStatusTokenRefreshed {
		t.Fatalf("status=%s want %s", res.Status, LoginStatusTokenRefreshed)
	}
	if !strings.Contains(res.UpdatedCookies, "_m_h5_tk=newtk_1") {
		t.Fatalf("UpdatedCookies=%q", res.UpdatedCookies)
	}
}

// TestClassifyLoginStatusRisk 负责TestClassify登录状态Risk相关处理。
func TestClassifyLoginStatusRisk(t *testing.T) {
	// status、msg 保存status、msg，供当前处理流程使用
	status, msg := classifyLoginStatus([]string{"RGV587_ERROR::哎哟喂，被挤爆啦"}, false)
	if status != LoginStatusRiskRequired || msg == "" {
		t.Fatalf("status=%s msg=%q", status, msg)
	}
}
