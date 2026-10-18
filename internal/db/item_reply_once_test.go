package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestItemReplyOnceRecordsIsolation 用 t 验证三元作用域互不消耗、状态更新隔离及清空账号记录不误清商品。
func TestItemReplyOnceRecordsIsolation(t *testing.T) {
	// store、cleanup 拥有临时SQLite库与同步关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ownerID、cookieID 是当前测试用户和第一个账号的非敏感身份。
	ownerID, cookieID := seedAccount(t, store)
	// ctx 不包含任何平台会话或凭证。
	ctx := context.Background()
	// scope 是账号兜底或不同商品，同一chat仍必须分别领取。
	for _, scope := range []string{"", "item-a", "item-b"} {
		// records 是不可变作用域访问器；claimed、err 检查首次发送权。
		records := store.DefaultReps.RecordsForItem(scope)
		// claimed、err 保存原子领取结果；首次不应读取其他作用域的状态。
		_, claimed, err := records.ClaimRecord(ctx, cookieID, "chat", true, true)
		if err != nil || !claimed {
			t.Fatalf("scope=%q claim=%v err=%v", scope, claimed, err)
		}
	}
	if err := store.Cookies.Save(ctx, "second-once-account", "", ownerID); err != nil { // err 是创建同用户第二个测试账号的错误。
		t.Fatal(err)
	}
	if _, claimed, err := store.DefaultReps.RecordsForItem("item-a").ClaimRecord(ctx, "second-once-account", "chat", true, true); err != nil || !claimed { // claimed、err 验证同名商品跨账号不能互相抑制。
		t.Fatalf("跨账号领取=%v err=%v", claimed, err)
	}
	// itemA、itemB 是不同商品访问器，不持有可变全局作用域。
	itemA, itemB := store.DefaultReps.RecordsForItem("item-a"), store.DefaultReps.RecordsForItem("item-b")
	if _, claimed, err := itemA.ClaimRecord(ctx, cookieID, "chat", true, true); err != nil || claimed { // claimed、err 必须证明正在发送的同作用域不可重领。
		t.Fatalf("并发重复领取=%v err=%v", claimed, err)
	}
	if err := itemA.MarkPartSent(ctx, cookieID, "chat", "image"); err != nil { // err 是图片确认写入错误。
		t.Fatal(err)
	}
	if err := itemA.MarkRecordFailed(ctx, cookieID, "chat", "文字确定失败"); err != nil { // err 是确定失败持久化错误。
		t.Fatal(err)
	}
	// record、claimed、err 验证重试继承图片确认而非重新发送图片。
	record, claimed, err := itemA.ClaimRecord(ctx, cookieID, "chat", true, true)
	if err != nil || !claimed || !record.ImageSent || record.TextSent {
		t.Fatalf("分段恢复=%+v claim=%v err=%v", record, claimed, err)
	}
	// other、err 必须证明上面的更新没有修改同会话另一商品。
	other, err := itemB.Record(ctx, cookieID, "chat")
	if err != nil || other.ImageSent || other.TextSent || other.Status != "sending" {
		t.Fatalf("商品隔离=%+v err=%v", other, err)
	}
	if err := itemA.MarkRecordUncertain(ctx, cookieID, "chat", "投递结果不确定"); err != nil { // err 是未知状态隔离错误。
		t.Fatal(err)
	}
	if _, claimed, err := itemA.ClaimRecord(ctx, cookieID, "chat", true, true); err != nil || claimed { // claimed、err 验证未知结果禁止重领。
		t.Fatalf("未知结果被重领=%v err=%v", claimed, err)
	}
	if err := itemB.MarkRecordSent(ctx, cookieID, "chat"); err != nil { // err 是另一商品完成状态写入错误。
		t.Fatal(err)
	}
	if err := store.DefaultReps.ClearRecords(ctx, cookieID); err != nil { // err 是只清除账号兜底历史的错误。
		t.Fatal(err)
	}
	if !itemB.HasRecord(ctx, cookieID, "chat") {
		t.Fatal("账号清空误删商品记录")
	}
	if err := itemA.ClearRecords(ctx, cookieID); err != nil { // err 是指定商品范围记录清理错误。
		t.Fatal(err)
	}
	if !itemB.HasRecord(ctx, cookieID, "chat") {
		t.Fatal("商品A清空误删商品B")
	}
}

// TestItemReplyOnceConcurrentClaim 验证同一商品同会话跨访问器并发也只产生一个发送者。
func TestItemReplyOnceConcurrentClaim(t *testing.T) {
	// store、cleanup 属于测试，不由并发访问器关闭。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// cookieID 是被并发争用的非敏感账号身份。
	_, cookieID := seedAccount(t, store)
	// winners 由所有goroutine原子递增；wait由测试拥有，退出前等待所有数据库调用完成。
	var winners atomic.Int32
	// wait 收口全部并发领取，没有后台遗留任务。
	var wait sync.WaitGroup
	// index 是启动的并发领取序号，不参与作用域选择。
	for index := 0; index < 12; index++ {
		wait.Add(1)
		go func() { // 每个领取者独立构造作用域访问器，只共享并发安全连接池。
			defer wait.Done()
			// claimed、err 保存当前调用的发送权及数据库错误。
			_, claimed, err := store.DefaultReps.RecordsForItem("item").ClaimRecord(context.Background(), cookieID, "chat", true, false)
			if err != nil {
				t.Errorf("领取失败: %v", err)
				return
			}
			if claimed {
				winners.Add(1)
			}
		}()
	}
	wait.Wait()
	if winners.Load() != 1 {
		t.Fatalf("并发发送者=%d，期望1", winners.Load())
	}
}

// TestMultiDB_ItemReplyOnceMigration 验证00057升级/回退保留旧账号状态，商品默认关闭且三方言字段和唯一性一致。
func TestMultiDB_ItemReplyOnceMigration(t *testing.T) {
	// target 是SQLite或明确配置的外部测试方言，不连接生产服务。
	for _, target := range allTestTargets(t) {
		t.Run(target.name, func(t *testing.T) { // t 收集当前方言独立schema的迁移结果。
			defer target.cleanup()
			// subdir、dialect 是当前方言的嵌入迁移目录与Goose标识。
			subdir, dialect := migrationTestSubdir(t, target.dialect)
			if err := goose.SetDialect(dialect); err != nil { // err 是测试迁移器配置错误。
				t.Fatal(err)
			}
			goose.SetBaseFS(migrationsFS)
			if err := goose.DownTo(target.store.DB, "migrations/"+subdir, 54); err != nil { // err 是旧schema夹具构建错误。
				t.Fatal(err)
			}
			// ctx、cookieID 用于所有迁移前后的身份一致性检查。
			ctx := context.Background()
			// cookieID 是迁移前就存在的账号。
			_, cookieID := seedAccount(t, target.store)
			if _, err := target.store.DB.ExecContext(ctx, `INSERT INTO item_replay(cookie_id,item_id,reply_content) VALUES (?,'item','历史正文')`, cookieID); err != nil { // err 是旧商品数据写入错误。
				t.Fatal(err)
			}
			if _, err := target.store.DB.ExecContext(ctx, `INSERT INTO default_reply_records(cookie_id,chat_id,status,text_sent,image_sent,last_error,lease_expires_at) VALUES (?,'chat','failed',0,1,'历史失败',123)`, cookieID); err != nil { // err 是历史分段记录写入错误。
				t.Fatal(err)
			}
			if _, err := target.store.DB.ExecContext(ctx, `INSERT INTO default_reply_records(cookie_id,chat_id,status) VALUES (?,'already-sent','sent'),(?,'in-flight','sending')`, cookieID, cookieID); err != nil { // err 是升级前完成与发送中状态的写入错误。
				t.Fatal(err)
			}
			if err := goose.UpTo(target.store.DB, "migrations/"+subdir, 57); err != nil { // err 是新增迁移执行错误。
				t.Fatal(err)
			}
			// chatID 是升级前已完成或发送中的会话，迁移不能让任何一个重新取得发送权。
			for _, chatID := range []string{"already-sent", "in-flight"} {
				if _, claimed, err := target.store.DefaultReps.ClaimRecord(ctx, cookieID, chatID, true, false); err != nil || claimed { // claimed、err 验证旧账号发送事实在升级后仍阻断重复投递。
					t.Fatalf("迁移后重复领取chat=%s claimed=%v err=%v", chatID, claimed, err)
				}
			}
			// item、err 验证历史商品默认不限制回复次数。
			item, err := target.store.ItemReps.Get(ctx, cookieID, "item")
			if err != nil || item.ReplyOnce || item.ReplyContent != "历史正文" {
				t.Fatalf("历史配置=%+v err=%v", item, err)
			}
			// account、err 验证迁移未丢失部分发送事实。
			account, err := target.store.DefaultReps.Record(ctx, cookieID, "chat")
			if err != nil || account.Status != "failed" || account.TextSent || !account.ImageSent {
				t.Fatalf("历史记录=%+v err=%v", account, err)
			}
			if err := target.store.ItemReps.UpdateWithCurrent(ctx, cookieID, "item", func(current ItemReply) (ItemReply, error) { // current 是迁移后的旧正文；只开启商品去重。
				current.ReplyOnce = true
				return current, nil
			}); err != nil { // err 是商品开关持久化错误。
				t.Fatal(err)
			}
			if err := target.store.ItemReps.Set(ctx, cookieID, "item", "旧客户端更新"); err != nil { // err 是旧接口更新，不能清空新开关。
				t.Fatal(err)
			}
			// rows、err 验证列表读取与旧写入口同时保留新开关。
			rows, err := target.store.ItemReps.AllForUser(ctx, cookieID)
			if err != nil || len(rows) != 1 || !rows[0].ReplyOnce || rows[0].ReplyContent != "旧客户端更新" {
				t.Fatalf("往返配置=%+v err=%v", rows, err)
			}
			if _, claimed, err := target.store.DefaultReps.RecordsForItem("item").ClaimRecord(ctx, cookieID, "chat", true, false); err != nil || !claimed { // claimed、err 必须证明历史账号记录不阻止商品。
				t.Fatalf("商品领取=%v err=%v", claimed, err)
			}
			if err := goose.DownTo(target.store.DB, "migrations/"+subdir, 54); err != nil { // err 是新增作用域回退错误。
				t.Fatal(err)
			}
			// status、imageSent 验证回退只删除新商品记录，不将其混入旧账号记录。
			var status string
			// imageSent 保留迁移前已确认的图片分段。
			var imageSent int
			if err := target.store.DB.QueryRowContext(ctx, `SELECT status,image_sent FROM default_reply_records WHERE cookie_id=? AND chat_id='chat'`, cookieID).Scan(&status, &imageSent); err != nil || status != "failed" || imageSent != 1 { // err 是回退后历史事实读取错误。
				t.Fatalf("回退状态=%s image=%d err=%v", status, imageSent, err)
			}
			if err := goose.Up(target.store.DB, "migrations/"+subdir); err != nil { // err 是回退后再次升级错误。
				t.Fatal(err)
			}
		})
	}
}

// TestItemReplyOnceDatabaseFailures 验证领取和未知结果隔离遇到数据库错误时不能授予发送权或恢复自动重放。
func TestItemReplyOnceDatabaseFailures(t *testing.T) {
	// store、cleanup 拥有本次故障注入库；SQLite触发器只存在于该临时库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// cookieID 是单次回复记录的非敏感账号身份。
	_, cookieID := seedAccount(t, store)
	// ctx 用于正常操作；canceled 只用于验证插入前的确定性取消错误。
	ctx := context.Background()
	// canceled、cancel 构造已取消的调用范围，数据库不得开始领取。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	// records 固定到本商品，不改变账号兜底访问器。
	records := store.DefaultReps.RecordsForItem("item")
	if _, claimed, err := records.ClaimRecord(canceled, cookieID, "canceled", true, false); !errors.Is(err, context.Canceled) || claimed { // claimed、err 必须证明取消不会绕过持久化直接发送。
		t.Fatalf("取消时领取=%v err=%v", claimed, err)
	}
	if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER ignore_missing_once BEFORE INSERT ON default_reply_records WHEN NEW.chat_id='missing' BEGIN SELECT RAISE(IGNORE); END`); err != nil { // err 是插入忽略夹具配置失败。
		t.Fatal(err)
	}
	if _, claimed, err := records.ClaimRecord(ctx, cookieID, "missing", true, false); !errors.Is(err, sql.ErrNoRows) || claimed { // claimed、err 验证插入未生效且查不到快照时必须停发。
		t.Fatalf("缺失快照领取=%v err=%v", claimed, err)
	}
	// chatID 分别代表确定失败重试和不可自动接管的发送中记录。
	for _, chatID := range []string{"failed", "sending"} {
		if _, claimed, err := records.ClaimRecord(ctx, cookieID, chatID, true, false); err != nil || !claimed { // claimed、err 是故障注入前的初始发送权。
			t.Fatalf("初始化领取=%v err=%v", claimed, err)
		}
	}
	if err := records.MarkRecordFailed(ctx, cookieID, "failed", "确定未发送"); err != nil { // err 是可重试状态的初始化错误。
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_once_updates BEFORE UPDATE ON default_reply_records BEGIN SELECT RAISE(FAIL,'fixture update failure'); END`); err != nil { // err 是更新故障夹具配置失败。
		t.Fatal(err)
	}
	if _, claimed, err := records.ClaimRecord(ctx, cookieID, "failed", true, false); err == nil || claimed { // claimed、err 验证重试状态写入失败不能继续投递。
		t.Fatalf("更新失败仍领取=%v err=%v", claimed, err)
	}
	if err := records.MarkRecordUncertain(ctx, cookieID, "sending", "结果未知"); err == nil { // err 必须汇总主隔离与降级隔离均无法落库的错误。
		t.Fatal("两次隔离写入失败却返回成功")
	}
	if _, claimed, err := records.ClaimRecord(ctx, cookieID, "sending", true, false); err != nil || claimed { // claimed、err 验证保留的sending状态正常阻止重领，不能产生重复发送者。
		t.Fatalf("隔离失败后领取=%v err=%v", claimed, err)
	}
}
