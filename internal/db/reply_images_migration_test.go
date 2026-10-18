package db

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMultiDB_ReplyImageMigration 验证 00056 可逆迁移、旧正文和 URL 保留，并在每个可用方言往返新图片配置。
func TestMultiDB_ReplyImageMigration(t *testing.T) {
	// target 是本地 SQLite 或显式配置的外部测试数据库，不接触生产连接。
	for _, target := range allTestTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// subdir、gooseDialect 保存当前方言迁移目录和 Goose 方言名。
			subdir, gooseDialect := migrationTestSubdir(t, target.dialect)
			if err := goose.SetDialect(gooseDialect); err != nil { // err 是迁移运行器方言设置错误。
				t.Fatal(err)
			}
			goose.SetBaseFS(migrationsFS)
			if err := goose.DownTo(target.store.DB, "migrations/"+subdir, 52); err != nil { // err 是构造旧版本基线失败。
				t.Fatal(err)
			}
			if columnExistsForDialect(t, target.store.DB, target.dialect, "default_replies", "reply_image_path") || columnExistsForDialect(t, target.store.DB, target.dialect, "item_replay", "reply_image_path") {
				t.Fatal("回退后新字段仍存在")
			}
			// ctx 和 cookieID 是本次回归的上下文及隔离账号标识。
			ctx := context.Background()
			// cookieID 是本次方言迁移回归所属的隔离账号。
			_, cookieID := seedAccount(t, target.store)
			if _, err := target.store.DB.ExecContext(ctx, `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_image_url,reply_once) VALUES (?,1,'历史正文','https://example.test/old.png',1)`, cookieID); err != nil { // err 是旧格式账号配置写入错误。
				t.Fatal(err)
			}
			if _, err := target.store.DB.ExecContext(ctx, `INSERT INTO item_replay (item_id,cookie_id,reply_content) VALUES ('old-item',?,'历史商品')`, cookieID); err != nil { // err 是旧格式商品配置写入错误。
				t.Fatal(err)
			}
			if err := goose.UpTo(target.store.DB, "migrations/"+subdir, 56); err != nil { // err 是本次新增迁移的执行错误。
				t.Fatal(err)
			}
			// reply、readErr 验证升级不改旧 URL、正文或一次性开关。
			reply, readErr := target.store.DefaultReps.Get(ctx, cookieID)
			if readErr != nil || reply.ReplyImageURL != "https://example.test/old.png" || reply.ReplyImagePath != "" || reply.ReplyContent != "历史正文" || !reply.ReplyOnce {
				t.Fatalf("历史账号配置=%+v err=%v", reply, readErr)
			}
			if !columnExistsForDialect(t, target.store.DB, target.dialect, "item_replay", "reply_image_path") {
				t.Fatal("00056 必须单独建立商品图片列")
			}
			// 当前仓储还需要后续商品去重字段，先完成后续迁移；不改变上面对 00056 本身的断言。
			if err := goose.Up(target.store.DB, "migrations/"+subdir); err != nil { // err 是历史结构升级到当前仓储契约的错误。
				t.Fatal(err)
			}
			if err := target.store.ItemReps.UpdateWithCurrent(ctx, cookieID, "old-item", func(current ItemReply) (ItemReply, error) { // current 是升级后的旧商品正文快照。
				current.ReplyImagePath = "商品/图.png"
				return current, nil
			}); err != nil { // err 是新本地图配置写入错误。
				t.Fatal(err)
			}
			// item、itemErr 验证三方言均能保留正文并往返本地路径。
			item, itemErr := target.store.ItemReps.Get(ctx, cookieID, "old-item")
			if itemErr != nil || item.ReplyContent != "历史商品" || item.ReplyImagePath != "商品/图.png" {
				t.Fatalf("商品配置=%+v err=%v", item, itemErr)
			}
		})
	}
}
