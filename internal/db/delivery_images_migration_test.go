package db

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMultiDB_DeliveryImageMigration 验证三方言 00055 可逆升级、旧文本与 URL 不变及新字段默认值。
func TestMultiDB_DeliveryImageMigration(t *testing.T) {
	// target 是独立 SQLite 或显式提供的测试数据库，不连接生产环境。
	for _, target := range allTestTargets(t) {
		t.Run(target.name, func(t *testing.T) { // t 是当前数据库方言的独立断言入口。
			defer target.cleanup()
			// subdir、dialect 是 Goose 迁移目录和方言名称。
			subdir, dialect := migrationTestSubdir(t, target.dialect)
			if err := goose.SetDialect(dialect); err != nil { // err 是迁移方言设置错误。
				t.Fatal(err)
			}
			goose.SetBaseFS(migrationsFS)
			if err := goose.DownTo(target.store.DB, "migrations/"+subdir, 53); err != nil { // err 是构造升级前结构的错误。
				t.Fatal(err)
			}
			if columnExistsForDialect(t, target.store.DB, target.dialect, "cards", "image_path") || columnExistsForDialect(t, target.store.DB, target.dialect, "delivery_template_messages", "type") {
				t.Fatal("回退后新字段仍存在")
			}
			// ctx 是本次离线迁移测试的数据库上下文。
			ctx := context.Background()
			// userID 是模板和卡券共享的用户所有者，不绑定具体账号。
			userID, _ := seedAccount(t, target.store)
			// cardID、cardErr 在旧 schema 中直接插入历史远程图片卡。
			cardID, cardErr := insertReturningID(ctx, target.store.DB, target.dialect, `INSERT INTO cards (user_id,name,type,image_url) VALUES (?,'旧图片','image','https://example.test/old.png')`, userID)
			if cardErr != nil {
				t.Fatal(cardErr)
			}
			// templateID、templateErr 插入不含新消息字段的历史模板。
			templateID, templateErr := insertReturningID(ctx, target.store.DB, target.dialect, `INSERT INTO delivery_templates (user_id,name,enabled) VALUES (?,'旧模板',1)`, userID)
			if templateErr != nil {
				t.Fatal(templateErr)
			}
			if _, err := target.store.DB.ExecContext(ctx, `INSERT INTO delivery_template_messages (template_id,sort_order,content) VALUES (?,1,'旧正文 {{order_id}}')`, templateID); err != nil { // err 是旧格式消息写入错误。
				t.Fatal(err)
			}
			if err := goose.UpTo(target.store.DB, "migrations/"+subdir, 55); err != nil { // err 是当前迁移执行错误。
				t.Fatal(err)
			}
			// card、readErr 验证卡券升级保留旧 URL 并将本地路径默认设为空。
			card, readErr := target.store.Cards.Get(ctx, cardID)
			if readErr != nil || card.ImagePath != "" || card.ImageURL != "https://example.test/old.png" {
				t.Fatalf("历史图片迁移失败: %+v err=%v", card, readErr)
			}
			// template、messageErr 验证历史消息被默认标记为 text，正文和顺序不变。
			template, messageErr := target.store.DeliveryTemplates.GetForUser(ctx, userID, templateID)
			if messageErr != nil || len(template.Messages) != 1 || template.Messages[0].Type != "text" || template.Messages[0].Content != "旧正文 {{order_id}}" || template.Messages[0].ImagePath != "" {
				t.Fatalf("历史模板迁移失败: %+v err=%v", template, messageErr)
			}
			card.ImagePath, card.ImageURL = "本地/卡.png", ""
			if err := target.store.Cards.Update(ctx, card); err != nil { // err 是新字段跨方言写入错误。
				t.Fatal(err)
			}
			// updated、updateErr 验证当前方言能够往返中文相对路径。
			updated, updateErr := target.store.Cards.Get(ctx, cardID)
			if updateErr != nil || updated.ImagePath != "本地/卡.png" || updated.ImageURL != "" {
				t.Fatalf("新路径往返失败: %+v err=%v", updated, updateErr)
			}
		})
	}
}
