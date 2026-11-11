package automation

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestResolveConsignProofModes 验证三种模式各自提交的 tradeText/picList，且不修改调用方持有的已投递凭证。
func TestResolveConsignProofModes(t *testing.T) {
	// delivered 是买家实际收到的卡密文本与图片。
	delivered := shipmentDeliveryProof{tradeText: "卡密 ABC\n使用说明", picList: []string{"https://img.example/a.png"}, preparedUnits: 1}

	// full 是 delivery 模式的提交内容，必须与已投递内容一致。
	full := resolveConsignProof(db.ConsignProofSetting{Mode: db.ConsignProofModeDelivery}, delivered)
	if full.tradeText != delivered.tradeText || len(full.picList) != 1 || full.picList[0] != delivered.picList[0] {
		t.Fatalf("delivery 模式应原样提交: %#v", full)
	}
	// full.picList 与 delivered.picList 不得共享底层数组，否则修改提交内容会污染持久化凭证。
	full.picList[0] = "changed"
	if delivered.picList[0] != "https://img.example/a.png" {
		t.Fatal("delivery 模式不得与已投递凭证共享图片切片")
	}

	// fixed 是固定文字模式的提交内容：只有去掉首尾空白的固定文字，没有图片。
	fixed := resolveConsignProof(db.ConsignProofSetting{Mode: db.ConsignProofModeFixed, FixedText: "  已通过聊天发送  "}, delivered)
	if fixed.tradeText != "已通过聊天发送" || len(fixed.picList) != 0 {
		t.Fatalf("fixed 模式只应提交固定文字: %#v", fixed)
	}

	// none 是不携带模式的提交内容：文本与图片均为空。
	none := resolveConsignProof(db.ConsignProofSetting{Mode: db.ConsignProofModeNone, FixedText: "不应提交"}, delivered)
	if none.tradeText != "" || len(none.picList) != 0 {
		t.Fatalf("none 模式不应携带凭证: %#v", none)
	}

	// legacy 是未知模式的提交内容，按历史行为归一为 delivery。
	legacy := resolveConsignProof(db.ConsignProofSetting{Mode: "garbage"}, delivered)
	if legacy.tradeText != delivered.tradeText {
		t.Fatalf("未知模式应按 delivery 处理: %#v", legacy)
	}
}

// TestConfirmShipmentUsesAccountConsignProofSetting 验证确认发货按账号设置决定提交给平台的凭证，并在设置读取失败时不调用平台。
func TestConfirmShipmentUsesAccountConsignProofSetting(t *testing.T) {
	// ctx 是调用共用的上下文。
	ctx := context.Background()
	// task 是已开启自动确认发货的账号 cid 的确认发货任务。
	task := Task{AccountID: "cid", OrderID: "consign-proof-order", ItemID: "i", BuyerID: "b", ChatID: "c"}
	// delivered 是买家已收到的模板全文与图片。
	delivered := shipmentDeliveryProof{tradeText: "网盘链接 https://pan.example/x 提取码 1234", picList: []string{"https://img.example/p.png"}}
	// cases 覆盖三种凭证模式下平台应收到的提交内容；每个子测试使用独立数据库。
	cases := []struct {
		// name 是子测试名称。
		name string
		// mode、text 是写入账号的凭证模式与固定文字；mode 为空表示保持迁移默认值。
		mode, text string
		// wantText、wantPics 是平台替身应收到的 tradeText 与图片数量。
		wantText string
		wantPics int
	}{
		{name: "默认携带卡密或模板全文", wantText: delivered.tradeText, wantPics: 1},
		{name: "固定文字", mode: db.ConsignProofModeFixed, text: "已通过聊天发送", wantText: "已通过聊天发送"},
		{name: "不携带凭证", mode: db.ConsignProofModeNone, wantText: ""},
	}
	// tc 是当前子测试用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// client 记录确认发货收到的凭证。
			client := &fakeMTop{consignOk: true, consignRet: []string{"SUCCESS::调用成功"}}
			// center、store 是隔离的自动化中心与数据库。
			center, store, _ := newConsignGapCenter(t, client, false, false)
			if tc.mode != "" {
				// admin、adminErr 保存账号所属用户。
				admin, adminErr := store.Users.GetByUsername(ctx, "admin")
				if adminErr != nil {
					t.Fatal(adminErr)
				}
				// updateErr 保存写入账号凭证设置的错误。
				if _, updateErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, ConsignProofMode: &tc.mode, ConsignProofText: &tc.text}); updateErr != nil {
					t.Fatal(updateErr)
				}
			}
			// confirmErr 保存确认发货结果。
			if confirmErr := center.actions.confirmShipmentWithProof(ctx, task, delivered); confirmErr != nil {
				t.Fatal(confirmErr)
			}
			if client.consignCalls != 1 || client.consignTradeTextIn != tc.wantText || len(client.consignPicListIn) != tc.wantPics {
				t.Fatalf("确认发货凭证错误: calls=%d text=%q pics=%v", client.consignCalls, client.consignTradeTextIn, client.consignPicListIn)
			}
		})
	}

	t.Run("设置读取失败时不调用平台", func(t *testing.T) {
		// client 记录是否发生确认发货调用。
		client := &fakeMTop{consignOk: true}
		// center、store 是隔离的自动化中心与数据库。
		center, store, _ := newConsignGapCenter(t, client, false, false)
		execGapSQL(t, store, `ALTER TABLE cookies DROP COLUMN consign_proof_text`)
		// confirmErr 必须是设置读取错误，且平台未被调用。
		confirmErr := center.actions.confirmShipmentWithProof(ctx, task, delivered)
		if confirmErr == nil || !strings.Contains(confirmErr.Error(), "读取确认发货凭证设置") || client.consignCalls != 0 {
			t.Fatalf("读取失败应在调用平台前返回: err=%v calls=%d", confirmErr, client.consignCalls)
		}
	})
}
