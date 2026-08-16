package server

import (
	"reflect"
	"testing"

	"xianyu-go/internal/adapter"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// TestOrderForAutomation 保证未迁移的自动化边界不会丢失订单字段。
func TestOrderForAutomation(t *testing.T) {
	// applicationOrder 是传入自动化中心适配器的应用订单。
	applicationOrder := &orderapp.Order{
		OrderID: "order-2", ItemID: "item-2", BuyerID: "buyer-2", ReceiverAddress: "地址",
		CookieID: "cookie-2", ChatID: "chat-2", OrderStatus: "pending_ship", Version: 4,
	}
	// expectedDatabaseOrder 是自动化中心当前接口仍接收的数据库订单。
	expectedDatabaseOrder := &db.Order{
		OrderID: "order-2", ItemID: "item-2", BuyerID: "buyer-2", ReceiverAddr: "地址",
		CookieID: "cookie-2", ChatID: "chat-2", OrderStatus: "pending_ship", Version: 4,
	}
	// converted 是传给自动化中心的数据库订单。
	converted := adapter.OrderForAutomation(applicationOrder)
	if !reflect.DeepEqual(converted, expectedDatabaseOrder) {
		t.Fatalf("自动化边界订单转换错误: got=%+v want=%+v", converted, expectedDatabaseOrder)
	}
	if adapter.OrderForAutomation(nil) != nil {
		t.Fatal("空应用订单应转换为空数据库订单")
	}
}
