package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

const qrLoginGenerateTimeout = 2 * time.Minute

// mountQRLoginReal 扫码登录端点（纯 HTTP，不需要浏览器）。
func (s *Server) mountQRLoginReal(r chi.Router) {
	r.Post("/qr-login/generate", s.generateQRLogin)
	r.Get("/qr-login/check/{session_id}", s.checkQRLoginStatus)
	r.Get("/qr-login/status/{session_id}", s.checkQRLoginStatusAndPersist)
	r.Post("/qr-login/complete-verification/{session_id}", s.completeQRVerification)
}

// generateQRLogin 生成扫码登录二维码。
func (s *Server) generateQRLogin(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	s.cleanupQRLoginSessions()
	// 风控后的闲鱼匿名 token 接口偶尔会明显变慢。二维码生成需要连续完成
	// token、登录参数和二维码请求，不能沿用前端通用接口的短超时。
	generateCtx, cancel := context.WithTimeout(r.Context(), qrLoginGenerateTimeout)
	defer cancel()
	sessionID, qrCodeURL, err := s.QRLogin.GenerateQRCode(generateCtx)
	if err != nil {
		message := "生成二维码失败: " + err.Error()
		switch {
		case errors.Is(err, context.Canceled):
			s.Logger.Info("二维码生成请求已取消")
			message = "二维码生成请求已取消，请重新获取"
		case errors.Is(err, context.DeadlineExceeded):
			s.Logger.Error("生成二维码超时", "err", err)
			message = "闲鱼二维码接口响应超时，请稍后重新获取"
		default:
			s.Logger.Error("生成二维码失败", "err", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": message,
		})
		return
	}
	s.qrMu.Lock()
	s.qrOwners[sessionID] = qrLoginOwner{UserID: sess.UserID, CreatedAt: time.Now().UTC()}
	s.qrMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"session_id":  sessionID,
		"qr_code_url": qrCodeURL,
	})
}

// checkQRLoginStatus 检查扫码登录状态。
func (s *Server) checkQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	result := publicQRStatus(s.QRLogin.GetSessionStatus(sessionID))
	writeJSON(w, http.StatusOK, result)
}

// checkQRLoginStatusAndPersist 兼容上游 /status 语义：扫码成功后由后端幂等保存账号。
func (s *Server) checkQRLoginStatusAndPersist(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	result := cloneQRStatus(s.QRLogin.GetSessionStatus(sessionID))
	if qrStatus(result) != "success" {
		writeJSON(w, http.StatusOK, publicQRStatus(result))
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	persisted, err := s.persistQRLoginSuccess(r.Context(), sess.UserID, sessionID, result)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("保存扫码登录结果失败", "session_id", sessionID, "err", err)
		}
		result["success"] = false
		result["status"] = "error"
		result["message"] = "保存扫码登录结果失败: " + err.Error()
		writeJSON(w, http.StatusOK, publicQRStatus(result))
		return
	}
	result["success"] = true
	result["account_id"] = persisted.AccountID
	result["is_new_account"] = persisted.IsNew
	writeJSON(w, http.StatusOK, publicQRStatus(result))
}

// completeQRVerification 用户完成风控验证后调用，提取真实 cookie 并入库。
func (s *Server) completeQRVerification(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 session_id")
		return
	}
	if !s.requireQRSessionOwner(w, r, sessionID) {
		return
	}
	cookies, unb, err := s.QRLogin.CompleteVerification(r.Context(), sessionID)
	if err != nil {
		s.Logger.Error("验证完成处理失败", "err", err)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	var req struct {
		TargetAccountID string `json:"target_account_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求格式错误")
			return
		}
	}
	req.TargetAccountID = strings.TrimSpace(req.TargetAccountID)
	if req.TargetAccountID != "" && req.TargetAccountID != unb {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":            false,
			"scanned_account_id": unb,
			"message":            "扫码账号与待重新授权账号不一致，已拒绝覆盖；请使用正确账号重新扫码",
		})
		return
	}
	resp := map[string]any{
		"success": true,
		"unb":     unb,
	}
	sess := auth.SessionFromContext(r.Context())
	if sess != nil {
		result := map[string]any{
			"status":  "success",
			"cookies": cookies,
			"unb":     unb,
		}
		if current := s.QRLogin.GetSessionStatus(sessionID); current != nil {
			if snapshot, ok := current["cookie_snapshot"]; ok {
				result["cookie_snapshot"] = snapshot
			}
		}
		persisted, persistErr := s.persistQRLoginSuccessFor(r.Context(), sess.UserID, sessionID, result, req.TargetAccountID)
		if persistErr != nil {
			if s.Logger != nil {
				s.Logger.Warn("保存扫码验证结果失败", "session_id", sessionID, "err", persistErr)
			}
			resp["success"] = false
			resp["message"] = "保存扫码登录结果失败: " + persistErr.Error()
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp["account_id"] = persisted.AccountID
		resp["is_new_account"] = persisted.IsNew
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) persistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any) (qrLoginPersistence, error) {
	return s.persistQRLoginSuccessFor(ctx, userID, sessionID, result, "")
}

func (s *Server) persistQRLoginSuccessFor(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (qrLoginPersistence, error) {
	lockValue, _ := s.qrPersistLocks.LoadOrStore(sessionID, &sync.Mutex{})
	persistMu := lockValue.(*sync.Mutex)
	persistMu.Lock()
	defer persistMu.Unlock()

	s.qrMu.Lock()
	if s.qrPersisted == nil {
		s.qrPersisted = make(map[string]qrLoginPersistence)
	}
	if persisted, ok := s.qrPersisted[sessionID]; ok {
		s.qrMu.Unlock()
		if persisted.UserID != userID {
			return qrLoginPersistence{}, errors.New("扫码会话不属于当前用户")
		}
		return persisted, nil
	}
	s.qrMu.Unlock()
	cookies := qrString(result, "cookies")
	cookieSnapshot, snapshotComplete := qrCookieSnapshot(result)
	scannedAccountID := strings.TrimSpace(firstNonEmpty(qrString(result, "unb"), protocol.TransCookies(cookies)["unb"]))
	if cookies == "" || scannedAccountID == "" {
		return qrLoginPersistence{}, errors.New("扫码结果缺少 cookies 或 unb")
	}
	accountID := strings.TrimSpace(targetAccountID)
	if accountID == "" {
		accountID = scannedAccountID
	} else if accountID != scannedAccountID {
		return qrLoginPersistence{}, errors.New("扫码账号与待重新授权账号不一致，已拒绝覆盖")
	}

	isNew := false
	credentialUnlock := s.Store.LockAccountCredentials(accountID)
	saveErr := func() error {
		defer credentialUnlock()
		detail, err := s.loadCookiePlatformDetail(ctx, accountID)
		switch {
		case errors.Is(err, db.ErrNotFound):
			if targetAccountID != "" {
				return errors.New("待重新授权账号不存在")
			}
			isNew = true
			if err := s.Store.Cookies.CreateOwned(ctx, accountID, cookies, userID); err != nil {
				return err
			}
			if snapshotComplete {
				metadata := cookierefresh.MetadataWithSnapshot("", cookieSnapshot)
				if err := s.Store.Cookies.UpdateRenewalCookie(ctx, accountID, cookies, metadata, time.Now().Unix()); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		case detail == nil:
			return db.ErrNotFound
		case detail.UserID != userID:
			if targetAccountID != "" {
				return errors.New("待重新授权账号不属于当前用户")
			}
			return db.ErrForbidden
		default:
			if snapshotComplete {
				metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, cookieSnapshot)
				if err := s.Store.Cookies.UpdateRenewalCookie(ctx, detail.ID, cookies, metadata, time.Now().Unix()); err != nil {
					return err
				}
			} else if err := s.updateFlatCookieOwnedLocked(ctx, detail, cookies); err != nil {
				return err
			}
		}
		s.markSuccessfulLogin(ctx, accountID, userID, loginMethodQRScan, "扫码登录成功")
		if s.Store.Tokens != nil {
			if err := s.Store.Tokens.Clear(ctx, accountID); err != nil {
				s.Logger.Warn("扫码登录保存后清理旧连接凭证失败", "cookie_id", accountID, "err", err)
			}
		}
		return nil
	}()
	if saveErr != nil {
		if errors.Is(saveErr, db.ErrForbidden) {
			return qrLoginPersistence{}, errors.New("该账号ID已存在且不属于当前用户")
		}
		if errors.Is(saveErr, db.ErrAlreadyExists) {
			return qrLoginPersistence{}, errors.New("该账号ID已被并发创建，请重新获取账号状态")
		}
		return qrLoginPersistence{}, saveErr
	}
	if d, err := s.loadCookieSummaryDetail(ctx, userID, accountID); err == nil {
		s.refreshAccountProfile(ctx, d)
	}
	s.wakeCredentialBlockedAutomation(ctx, accountID)
	if s.Manager != nil && s.Store.Cookies.GetStatus(ctx, accountID) {
		if err := s.Manager.Restart(ctx, accountID); err != nil && s.Logger != nil {
			s.Logger.Warn("扫码登录后重启账号失败", "cookie_id", accountID, "err", err)
		}
	}
	persisted := qrLoginPersistence{AccountID: accountID, IsNew: isNew, UserID: userID, CreatedAt: time.Now().UTC()}
	s.qrMu.Lock()
	s.qrPersisted[sessionID] = persisted
	s.qrMu.Unlock()
	s.qrPersistLocks.Delete(sessionID)
	return persisted, nil
}

func (s *Server) requireQRSessionOwner(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return false
	}
	s.qrMu.Lock()
	owner, ok := s.qrOwners[sessionID]
	expired := ok && owner.CreatedAt.Before(time.Now().UTC().Add(-30*time.Minute))
	if expired {
		delete(s.qrOwners, sessionID)
		delete(s.qrPersisted, sessionID)
		s.qrPersistLocks.Delete(sessionID)
	}
	s.qrMu.Unlock()
	if expired {
		if cleaner, cleanable := s.QRLogin.(interface{ DeleteSession(string) }); cleanable {
			cleaner.DeleteSession(sessionID)
		}
	}
	if !ok || expired || owner.UserID != sess.UserID {
		writeErr(w, http.StatusNotFound, "扫码会话不存在或已过期")
		return false
	}
	return true
}

func (s *Server) cleanupQRLoginSessions() {
	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	expired := make([]string, 0)
	s.qrMu.Lock()
	for id, owner := range s.qrOwners {
		if owner.CreatedAt.Before(cutoff) {
			delete(s.qrOwners, id)
			delete(s.qrPersisted, id)
			s.qrPersistLocks.Delete(id)
			expired = append(expired, id)
		}
	}
	s.qrMu.Unlock()
	if cleaner, ok := s.QRLogin.(interface{ DeleteSession(string) }); ok {
		for _, id := range expired {
			cleaner.DeleteSession(id)
		}
	}
}

func cloneQRStatus(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// publicQRStatus 返回可暴露给浏览器的扫码状态。闲鱼 Cookie 只在服务端持久化，
// 永远不进入前端、浏览器日志或代理响应。
func publicQRStatus(src map[string]any) map[string]any {
	dst := cloneQRStatus(src)
	delete(dst, "cookies")
	delete(dst, "cookie_snapshot")
	return dst
}

func qrStatus(result map[string]any) string {
	status, _ := result["status"].(string)
	return status
}

func qrString(result map[string]any, key string) string {
	value, _ := result[key].(string)
	return strings.TrimSpace(value)
}

func qrCookieSnapshot(result map[string]any) ([]cookierefresh.BrowserCookie, bool) {
	raw, ok := result["cookie_snapshot"]
	if !ok {
		return nil, false
	}
	snapshot, ok := raw.([]cookierefresh.BrowserCookie)
	if !ok || snapshot == nil {
		return nil, false
	}
	normalized := cookierefresh.NormalizeSnapshot(snapshot)
	if normalized == nil {
		normalized = []cookierefresh.BrowserCookie{}
	}
	return normalized, true
}
