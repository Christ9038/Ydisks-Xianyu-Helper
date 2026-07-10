package db

import (
	"context"
	"errors"
	"testing"
)

// --- extras.go ---

// TestKeywords_AllWithType AllWithType 路径 + Add 默认 type。
func TestKeywords_AllWithType(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// Add 不指定 type → 默认 text。
	id1, err := s.Keywords.Add(ctx, cid, "你好", "在的", "", "", "")
	if err != nil || id1 == 0 {
		t.Fatalf("Add: %d %v", id1, err)
	}
	// 指定 type=image + imageURL + itemID。
	if _, err := s.Keywords.Add(ctx, cid, "图", "[img]", "item1", "image", "http://img"); err != nil {
		t.Fatalf("Add image: %v", err)
	}
	kws, err := s.Keywords.AllWithType(ctx, cid)
	if err != nil {
		t.Fatalf("AllWithType: %v", err)
	}
	if len(kws) != 2 {
		t.Fatalf("len=%d want 2", len(kws))
	}
	// 验证默认 type=text 兜底（第一条）。
	var foundText, foundImage bool
	for _, k := range kws {
		if k.Keyword == "你好" && k.Reply == "在的" && k.Type == "text" {
			foundText = true
		}
		if k.Keyword == "图" && k.Reply == "[img]" && k.Type == "image" && k.ImageURL == "http://img" && k.ItemID == "item1" {
			foundImage = true
		}
	}
	if !foundText || !foundImage {
		t.Fatalf("关键字字段不符: %#v", kws)
	}
}

// TestKeywords_AllRowsEmptyCookie AllRows 对无关键字的账号返回 nil 不报错。
func TestKeywords_AllRowsEmptyCookie(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	seedAccount(t, s)
	rows, err := s.Keywords.AllRows(ctx, "no-such-cookie")
	if err != nil || len(rows) != 0 {
		t.Fatalf("AllRows 空: %#v err=%v", rows, err)
	}
}

// TestItemReplies_SetDelete ItemReplies.Set/Delete/AllForUser + Get。
func TestItemReplies_SetDelete(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// Get 不存在 → ErrNotFound。
	if _, err := s.ItemReps.Get(ctx, cid, "i1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// Set 写入。
	if err := s.ItemReps.Set(ctx, cid, "i1", "回复A"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.ItemReps.Get(ctx, cid, "i1")
	if err != nil || got.ReplyContent != "回复A" {
		t.Fatalf("Get: %#v err=%v", got, err)
	}
	// 二次 Set 同 item → 覆盖（先删后插）。
	if err := s.ItemReps.Set(ctx, cid, "i1", "回复B"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got, _ = s.ItemReps.Get(ctx, cid, "i1")
	if got.ReplyContent != "回复B" {
		t.Fatalf("覆盖后=%q want 回复B", got.ReplyContent)
	}
	// AllForUser。
	all, err := s.ItemReps.AllForUser(ctx, cid)
	if err != nil || len(all) != 1 {
		t.Fatalf("AllForUser: %#v err=%v", all, err)
	}
	// Delete。
	if err := s.ItemReps.Delete(ctx, cid, "i1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.ItemReps.Get(ctx, cid, "i1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete 后应 ErrNotFound, got %v", err)
	}
}

// TestDefaultReplies_GetAndRecord DefaultReplies.Get/HasRecord/AddRecord。
func TestDefaultReplies_GetAndRecord(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// Get 不存在 → ErrNotFound。
	if _, err := s.DefaultReps.Get(ctx, cid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// 直接写入一条。
	_, _ = s.DB.ExecContext(ctx,
		`INSERT INTO default_replies (cookie_id, enabled, reply_content, reply_image_url, reply_once) VALUES (?, 1, '你好', 'http://img', 1)`,
		cid)
	dr, err := s.DefaultReps.Get(ctx, cid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !dr.Enabled || dr.ReplyContent != "你好" || dr.ReplyImageURL != "http://img" || !dr.ReplyOnce {
		t.Fatalf("default reply: %#v", dr)
	}
	// HasRecord 未记录 → false。
	if s.DefaultReps.HasRecord(ctx, cid, "chat1") {
		t.Fatal("未记录应 false")
	}
	// AddRecord → 已记录。
	if err := s.DefaultReps.AddRecord(ctx, cid, "chat1"); err != nil {
		t.Fatalf("AddRecord: %v", err)
	}
	if !s.DefaultReps.HasRecord(ctx, cid, "chat1") {
		t.Fatal("记录后应 true")
	}
	// 重复 AddRecord 应不报错（INSERT IGNORE）。
	if err := s.DefaultReps.AddRecord(ctx, cid, "chat1"); err != nil {
		t.Fatalf("重复 AddRecord: %v", err)
	}
}

// TestNotifications_GetChannelAndAccountChannels GetChannel + AccountChannels。
func TestNotifications_GetChannelAndAccountChannels(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)

	// GetChannel 不存在 → nil, nil。
	got, err := s.Notifications.GetChannel(ctx, 99999)
	if err != nil || got != nil {
		t.Fatalf("GetChannel 不存在应 nil, got %#v err=%v", got, err)
	}
	chID, _ := s.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "wh", Type: "webhook", Config: `{"url":"x"}`, Enabled: true, UserID: uid,
	})
	got, err = s.Notifications.GetChannel(ctx, chID)
	if err != nil || got == nil || got.Name != "wh" || got.Config != `{"url":"x"}` {
		t.Fatalf("GetChannel: %#v err=%v", got, err)
	}

	// AccountChannels：绑定渠道后查询。
	s.Notifications.SetBindings(ctx, cid, []int64{chID})
	channels, err := s.Notifications.AccountChannels(ctx, cid)
	if err != nil {
		t.Fatalf("AccountChannels: %v", err)
	}
	if len(channels) != 1 || channels[0].ID != chID || channels[0].Name != "wh" {
		t.Fatalf("AccountChannels: %#v", channels)
	}
	// 禁用渠道后不应再返回。
	s.Notifications.UpdateChannel(ctx, &NotificationChannelRow{
		ID: chID, Name: "wh", Type: "webhook", Config: `{"url":"x"}`, Enabled: false, UserID: uid,
	})
	channels, _ = s.Notifications.AccountChannels(ctx, cid)
	if len(channels) != 0 {
		t.Fatalf("禁用渠道后 AccountChannels len=%d want 0", len(channels))
	}
}

func TestNotificationsRejectCrossUserBindings(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	if ok, err := s.Users.Create(ctx, "u2", "u2@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create second user: ok=%v err=%v", ok, err)
	}
	u2, _ := s.Users.GetByUsername(ctx, "u2")
	ownCh, _ := s.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "own", Type: "webhook", Config: `{}`, Enabled: true, UserID: uid,
	})
	otherCh, _ := s.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "other", Type: "webhook", Config: `{}`, Enabled: true, UserID: u2.ID,
	})

	if err := s.Notifications.SetBindings(ctx, cid, []int64{otherCh}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-user SetBindings should be forbidden, got %v", err)
	}
	if err := s.Notifications.SetBindings(ctx, cid, []int64{ownCh}); err != nil {
		t.Fatalf("own SetBindings: %v", err)
	}
	// 模拟历史脏数据：当前账号绑定到了其他用户渠道，读取侧也必须过滤掉。
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES (?, ?, 1)`, cid, otherCh)
	bindings, err := s.Notifications.AccountBindings(ctx, cid)
	if err != nil {
		t.Fatalf("AccountBindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0] != ownCh {
		t.Fatalf("bindings should only include own channel: %#v", bindings)
	}
	channels, err := s.Notifications.AccountChannels(ctx, cid)
	if err != nil {
		t.Fatalf("AccountChannels: %v", err)
	}
	if len(channels) != 1 || channels[0].ID != ownCh {
		t.Fatalf("channels should only include own channel: %#v", channels)
	}
}

// TestSettings_GetAndAll Get 不存在返回空串 + All 全量。
func TestSettings_GetAndAll(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Get 不存在 → 空串不报错。
	v, err := s.Settings.Get(ctx, "no-such-key")
	if err != nil || v != "" {
		t.Fatalf("Get 不存在: v=%q err=%v", v, err)
	}
	// 写入两条。
	s.Settings.Set(ctx, "k1", "v1")
	s.Settings.Set(ctx, "k2", "v2")
	// 二次 Set 同 key → 覆盖（UPSERT）。
	s.Settings.Set(ctx, "k1", "v1-updated")
	all, err := s.Settings.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if all["k1"] != "v1-updated" || all["k2"] != "v2" {
		t.Fatalf("All = %#v", all)
	}
	got, _ := s.Settings.Get(ctx, "k1")
	if got != "v1-updated" {
		t.Fatalf("Get k1=%q want v1-updated", got)
	}
}

// TestSettings_Public Public 过滤：白名单 key 返回值，私有 key 不外露。
func TestSettings_Public(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// 写入一个公开 key + 几个遗留/私有 key。
	s.Settings.Set(ctx, "theme_color", "blue")
	s.Settings.Set(ctx, "registration_enabled", "true")
	s.Settings.Set(ctx, "show_default_login_info", "true")
	s.Settings.Set(ctx, "private_secret", "topsecret")
	s.Settings.Set(ctx, "qq_reply_secret_key", "should-not-leak")

	pub, err := s.Settings.Public(ctx)
	if err != nil {
		t.Fatalf("Public: %v", err)
	}
	if pub["theme_color"] != "blue" {
		t.Fatalf("公开 key 缺失: %#v", pub)
	}
	// 私有 key 不应出现。
	if _, ok := pub["private_secret"]; ok {
		t.Fatal("private_secret 不应外露")
	}
	if _, ok := pub["qq_reply_secret_key"]; ok {
		t.Fatal("qq_reply_secret_key 不应外露")
	}
	if _, ok := pub["login_captcha_enabled"]; ok {
		t.Fatal("未实现的登录验证码开关不应公开")
	}
	if _, ok := pub["registration_enabled"]; ok {
		t.Fatal("未实现的注册开关不应公开")
	}
	if _, ok := pub["show_default_login_info"]; ok {
		t.Fatal("未使用的默认登录提示开关不应公开")
	}
}

// --- cookies.go ---

// TestCookies_DeleteAndStatuses Delete/GetValue/GetDetails/SetStatus/GetStatus/UpdateProfile。
func TestCookies_DeleteAndStatuses(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)

	// GetValue。
	v, err := s.Cookies.GetValue(ctx, cid)
	if err != nil || v != "cv=admin" {
		t.Fatalf("GetValue: v=%q err=%v", v, err)
	}
	// GetValue 不存在。
	if _, err := s.Cookies.GetValue(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// GetDetails 不存在。
	if _, err := s.Cookies.GetDetails(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE cookies SET remark=NULL, username=NULL, password=NULL WHERE id=?`, cid); err != nil {
		t.Fatalf("set nullable text fields: %v", err)
	}
	details, err := s.Cookies.GetDetails(ctx, cid)
	if err != nil {
		t.Fatalf("GetDetails should tolerate NULL text fields: %v", err)
	}
	if details.Remark != "" || details.Username != "" || details.Password != "" {
		t.Fatalf("NULL text fields should decode as empty strings: %+v", details)
	}

	// GetAutoConfirm 不存在 → ErrNotFound。
	if _, err := s.Cookies.GetAutoConfirm(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAutoConfirm 不存在应 ErrNotFound, got %v", err)
	}
	// GetAutoConfirm 存在（默认 enabled=true）。
	if enabled, err := s.Cookies.GetAutoConfirm(ctx, cid); err != nil || !enabled {
		t.Fatalf("GetAutoConfirm 存在: enabled=%v err=%v", enabled, err)
	}

	// SetStatus false → GetStatus false。
	if err := s.Cookies.SetStatus(ctx, cid, false); err != nil {
		t.Fatalf("SetStatus false: %v", err)
	}
	if s.Cookies.GetStatus(ctx, cid) {
		t.Fatal("GetStatus 应 false")
	}
	// SetStatus true。
	s.Cookies.SetStatus(ctx, cid, true)
	if !s.Cookies.GetStatus(ctx, cid) {
		t.Fatal("GetStatus 应 true")
	}
	// GetStatus 不存在记录 → 默认 true。
	if !s.Cookies.GetStatus(ctx, "no-such-cookie") {
		t.Fatal("不存在记录 GetStatus 应默认 true")
	}

	// UpdateProfile。
	if err := s.Cookies.UpdateProfile(ctx, cid, "昵称", "http://avatar"); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	d, _ := s.Cookies.GetDetails(ctx, cid)
	if d.Nickname != "昵称" || d.AvatarURL != "http://avatar" {
		t.Fatalf("UpdateProfile 后: %#v", d)
	}

	// AllForUser userID=0 取全部（管理员视图）。
	all, err := s.Cookies.AllForUser(ctx, 0)
	if err != nil || all[cid] != "cv=admin" {
		t.Fatalf("AllForUser(0): %#v err=%v", all, err)
	}

	// Delete cookie → 关联关键字也删（虽然这里没关键字，但走事务路径）。
	if err := s.Cookies.Delete(ctx, cid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Cookies.GetValue(ctx, cid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete 后 GetValue 应 ErrNotFound, got %v", err)
	}
}

// TestCookies_SaveReuseUserID Save 在 userID=0 且 cookie 已存在时复用 user_id。
func TestCookies_SaveReuseUserID(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)

	// cookie 已存在；用 userID=0 调 Save 应复用 existing user_id 并更新 value。
	if err := s.Cookies.Save(ctx, cid, "new-value", 0); err != nil {
		t.Fatalf("Save reuse: %v", err)
	}
	v, _ := s.Cookies.GetValue(ctx, cid)
	if v != "new-value" {
		t.Fatalf("value=%q want new-value", v)
	}
	// 验证 user_id 仍指向 admin。
	d, _ := s.Cookies.GetDetails(ctx, cid)
	if d.UserID != uid {
		t.Fatalf("UserID=%d want %d", d.UserID, uid)
	}

	// Save 一个不存在的 cookie 且 userID=0 → 报错。
	if err := s.Cookies.Save(ctx, "ghost-cookie", "v", 0); err == nil {
		t.Fatal("不存在的 cookie + userID=0 应报错")
	}
}

// TestCookies_GetPauseDurationExplicit 显式 pause_duration=0 应返回 0（有效值）。
func TestCookies_GetPauseDurationExplicit(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	// 显式置 0。
	_, _ = s.DB.ExecContext(ctx, `UPDATE cookies SET pause_duration=0 WHERE id=?`, cid)
	if pd := s.Cookies.GetPauseDuration(ctx, cid); pd != 0 {
		t.Fatalf("GetPauseDuration=%d want 0", pd)
	}
	// 不存在的 cookie → 默认 10。
	if pd := s.Cookies.GetPauseDuration(ctx, "nope"); pd != 10 {
		t.Fatalf("GetPauseDuration 不存在=%d want 10", pd)
	}
}
