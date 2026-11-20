package db

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
)

// boundCookieIDs 读取某渠道当前绑定的账号标识并排序，供断言使用。
func boundCookieIDs(t *testing.T, store *Store, channelID int64) []string {
	t.Helper()
	// rows、queryErr 保存绑定查询结果及错误。
	rows, queryErr := store.Notifications.DB.QueryContext(context.Background(), `SELECT cookie_id FROM message_notifications WHERE channel_id=? AND enabled=1`, channelID)
	if queryErr != nil {
		t.Fatalf("查询渠道绑定: %v", queryErr)
	}
	defer rows.Close()
	// ids 保存绑定账号标识。
	ids := []string{}
	for rows.Next() {
		// id 是当前行的账号标识。
		var id string
		if // scanErr 保存查询扫描错误
		scanErr := rows.Scan(&id); scanErr != nil {
			t.Fatalf("读取绑定行: %v", scanErr)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// TestSetChannelAccountsReplacesOnlyThatChannel 验证整体替换渠道绑定账号：新增、移除、去重，且不影响账号绑定的其他渠道。
func TestSetChannelAccountsReplacesOnlyThatChannel(t *testing.T) {
	// store、cleanup 是临时 SQLite 测试存储和资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存当前数据库操作上下文。
	ctx := context.Background()
	// userID 是种子用户；acc1 是种子账号。
	userID, _ := seedAccount(t, store)
	if // saveErr 保存账号写入错误
	saveErr := store.Cookies.Save(ctx, "acc2", "cv=second", userID); saveErr != nil {
		t.Fatalf("保存第二个账号: %v", saveErr)
	}
	// channelA、channelB 是同一用户的两个渠道。
	channelA, errA := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{Name: "A", Type: "bark", Config: "{}", Enabled: true, UserID: userID})
	// channelB、errB 是第二个渠道的标识；保存渠道B创建错误
	channelB, errB := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{Name: "B", Type: "bark", Config: "{}", Enabled: true, UserID: userID})
	if errA != nil || errB != nil {
		t.Fatalf("创建渠道: %v %v", errA, errB)
	}
	// B 先绑定 acc1，用于确认替换 A 的账号集合不会影响 B。
	if bindErr := store.Notifications.SetSingleBinding(ctx, "acc1", channelB, true); bindErr != nil {
		t.Fatalf("预置 B 绑定: %v", bindErr)
	}

	// 首次绑定：两个账号，含重复项和空串，应去重后写入。
	if setErr := store.Notifications.SetChannelAccounts(ctx, userID, channelA, []string{"acc1", "acc2", "acc1", ""}); setErr != nil {
		t.Fatalf("SetChannelAccounts: %v", setErr)
	}
	if // got 保存当前读取到的绑定账号集合
	got := boundCookieIDs(t, store, channelA); !reflect.DeepEqual(got, []string{"acc1", "acc2"}) {
		t.Fatalf("A 绑定=%v", got)
	}
	// 缩小集合：移除 acc1，保留 acc2。
	if setErr := store.Notifications.SetChannelAccounts(ctx, userID, channelA, []string{"acc2"}); setErr != nil {
		t.Fatalf("缩小绑定: %v", setErr)
	}
	if // got 保存当前读取到的绑定账号集合
	got := boundCookieIDs(t, store, channelA); !reflect.DeepEqual(got, []string{"acc2"}) {
		t.Fatalf("缩小后 A 绑定=%v", got)
	}
	// 与账号管理一致：从账号侧读取 acc2 的绑定应包含 A。
	if ids, accountErr := store.Notifications.AccountBindings(ctx, "acc2"); accountErr != nil || len(ids) != 1 || ids[0] != channelA {
		t.Fatalf("账号侧绑定未同步: ids=%v err=%v", ids, accountErr)
	}
	// 空集合表示全部解绑。
	if setErr := store.Notifications.SetChannelAccounts(ctx, userID, channelA, []string{}); setErr != nil {
		t.Fatalf("清空绑定: %v", setErr)
	}
	if // got 保存当前读取到的绑定账号集合
	got := boundCookieIDs(t, store, channelA); len(got) != 0 {
		t.Fatalf("清空后 A 绑定=%v", got)
	}
	// B 的绑定始终不受影响。
	if got := boundCookieIDs(t, store, channelB); !reflect.DeepEqual(got, []string{"acc1"}) {
		t.Fatalf("B 绑定被误改=%v", got)
	}
}

// TestSetChannelAccountsRejectsForeignResourcesWithoutPartialWrite 验证渠道或账号不属于当前用户时拒绝，且不留下部分绑定。
func TestSetChannelAccountsRejectsForeignResourcesWithoutPartialWrite(t *testing.T) {
	// store、cleanup 是临时 SQLite 测试存储和资源释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存当前数据库操作上下文。
	ctx := context.Background()
	// userID 是种子管理员；otherID 是另一个普通用户。
	userID, _ := seedAccount(t, store)
	if // createErr、created 保存创建结果错误；表示用户是否创建成功
	created, createErr := store.Users.Create(ctx, "other", "other@e.com", "pw"); createErr != nil || !created {
		t.Fatalf("创建另一个用户: ok=%v err=%v", created, createErr)
	}
	// other 是另一个普通用户
	other, _ := store.Users.GetByUsername(ctx, "other")
	if // saveErr 保存账号写入错误
	saveErr := store.Cookies.Save(ctx, "foreign", "cv=other", other.ID); saveErr != nil {
		t.Fatalf("保存他人账号: %v", saveErr)
	}
	// channelID 是当前用户自己的渠道；foreignChannel 属于另一个用户。
	channelID, ownErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{Name: "own", Type: "bark", Config: "{}", Enabled: true, UserID: userID})
	// foreignChannel、foreignErr 是他人渠道标识；保存他人渠道创建错误
	foreignChannel, foreignErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{Name: "foreign", Type: "bark", Config: "{}", Enabled: true, UserID: other.ID})
	if ownErr != nil || foreignErr != nil {
		t.Fatalf("创建渠道: %v %v", ownErr, foreignErr)
	}
	// 他人账号混入集合：整体拒绝，acc1 也不能被写入。
	if setErr := store.Notifications.SetChannelAccounts(ctx, userID, channelID, []string{"acc1", "foreign"}); !errors.Is(setErr, ErrForbidden) {
		t.Fatalf("混入他人账号应返回 ErrForbidden: %v", setErr)
	}
	if // got 保存当前读取到的绑定账号集合
	got := boundCookieIDs(t, store, channelID); len(got) != 0 {
		t.Fatalf("拒绝后不应留下部分绑定: %v", got)
	}
	// 他人渠道：拒绝。
	if setErr := store.Notifications.SetChannelAccounts(ctx, userID, foreignChannel, []string{"acc1"}); !errors.Is(setErr, ErrForbidden) {
		t.Fatalf("他人渠道应返回 ErrForbidden: %v", setErr)
	}
	// 不存在的账号：拒绝。
	if setErr := store.Notifications.SetChannelAccounts(ctx, userID, channelID, []string{"missing"}); !errors.Is(setErr, ErrForbidden) {
		t.Fatalf("不存在账号应返回 ErrForbidden: %v", setErr)
	}
}
