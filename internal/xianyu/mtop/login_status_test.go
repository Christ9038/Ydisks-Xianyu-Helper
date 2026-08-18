package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRiskVerificationClassification 封装TestRiskVerificationClassification业务协调。
func TestRiskVerificationClassification(t *testing.T) {
	// ret 用于本次流程后续判断的ret
	ret := []string{"FAIL_SYS_USER_VALIDATE::用户校验失败"}
	if !isRiskVerificationRet(ret) {
		t.Fatal("FAIL_SYS_USER_VALIDATE 应识别为风控")
	}
	if isTokenExpiredRet(ret) {
		t.Fatal("FAIL_SYS_USER_VALIDATE 不应再被当作普通 token 过期")
	}
	// err 用于本次流程后续判断的err
	err := &RiskVerificationError{Ret: ret, VerificationURL: "https://passport.goofish.com/punish?x5secdata=1"}
	if !IsRiskVerificationErr(err) {
		t.Fatal("RiskVerificationError 应被识别")
	}
}

// TestRefreshTokenReturnsRiskVerificationError 封装TestRefresh令牌ReturnsRiskVerification错误业务协调。
func TestRefreshTokenReturnsRiskVerificationError(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtk_1"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_USER_VALIDATE::用户校验失败"],"data":{"url":"https://passport.goofish.com/punish?x5secdata=1"}}`)
	}))
	defer srv.Close()

	// c 用于本次流程后续判断的c
	c := &ClientImpl{HTTPClient: srv.Client(), TokenURL: srv.URL}
	// res、err 用于本次流程后续判断的res、err
	res, err := c.RefreshTokenWithDeviceIDContext(context.Background(), "unb=123; _m_h5_tk=old_1", "device-1")
	if err == nil || !IsRiskVerificationErr(err) {
		t.Fatalf("应返回风控错误: res=%#v err=%v", res, err)
	}
	if res == nil || !strings.Contains(res.UpdatedCookies, "_m_h5_tk=newtk_1") {
		t.Fatalf("风控时仍应保留 Set-Cookie: %#v", res)
	}
	// riskErr 用于本次流程后续判断的riskErr
	var riskErr *RiskVerificationError
	if !strings.Contains(err.Error(), "x5secdata") {
		t.Fatalf("风控 URL 未进入错误信息: %v", riskErr)
	}
}

// TestCheckLoginStatusTokenRefreshed 封装TestCheck登录状态令牌Refreshed业务协调。
func TestCheckLoginStatusTokenRefreshed(t *testing.T) {
	// srv 用于本次流程后续判断的srv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "newtk_1"})
		fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::令牌过期"],"data":{}}`)
	}))
	defer srv.Close()

	// c 用于本次流程后续判断的c
	c := &ClientImpl{HTTPClient: srv.Client(), LoginUserURL: srv.URL}
	// res、err 用于本次流程后续判断的res、err
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

// TestClassifyLoginStatusRisk 封装TestClassify登录状态Risk业务协调。
func TestClassifyLoginStatusRisk(t *testing.T) {
	// status、msg 用于本次流程后续判断的status、msg
	status, msg := classifyLoginStatus([]string{"RGV587_ERROR::哎哟喂，被挤爆啦"}, false)
	if status != LoginStatusRiskRequired || msg == "" {
		t.Fatalf("status=%s msg=%q", status, msg)
	}
}
