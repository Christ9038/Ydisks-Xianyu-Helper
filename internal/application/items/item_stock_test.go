package items

import (
	"context"
	"errors"
	"testing"
)

// stockPortStub 记录库存端口收到的调用，并返回预设结果。
type stockPortStub struct {
	// getCalls、updateCalls 统计两个方法被调用的次数。
	getCalls, updateCalls int
	// gotUser、gotCookie、gotItem 保存最近一次调用的标识参数。
	gotUser   int64
	gotCookie string
	gotItem   string
	// gotUpdate 保存最近一次收到的库存修改。
	gotUpdate StockUpdate
	// err 是端口返回的预设错误。
	err error
}

// GetStock 记录读取调用。
func (s *stockPortStub) GetStock(_ context.Context, userID int64, cookieID, itemID string) (StockSnapshot, error) {
	s.getCalls++
	s.gotUser, s.gotCookie, s.gotItem = userID, cookieID, itemID
	return StockSnapshot{ItemID: itemID, Quantity: 3}, s.err
}

// UpdateStock 记录修改调用。
func (s *stockPortStub) UpdateStock(_ context.Context, userID int64, cookieID, itemID string, update StockUpdate) (StockUpdateOutcome, error) {
	s.updateCalls++
	s.gotUser, s.gotCookie, s.gotItem, s.gotUpdate = userID, cookieID, itemID, update
	return StockUpdateOutcome{ItemID: itemID, CookiePersisted: true}, s.err
}

// TestNewItemStockServiceRejectsNilPort 验证必需依赖为空时构造失败。
func TestNewItemStockServiceRejectsNilPort(t *testing.T) {
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := NewItemStockService(nil); err == nil {
		t.Fatal("端口为空应构造失败")
	}
}

// TestItemStockServiceTrimsIdentifiersAndDelegates 验证标识被规范化后委托给端口。
func TestItemStockServiceTrimsIdentifiersAndDelegates(t *testing.T) {
	// port、service 是端口替身与被测服务。
	port := &stockPortStub{}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用；service 是被测的库存应用服务。
	service, err := NewItemStockService(port)
	if err != nil {
		t.Fatal(err)
	}
	// snapshot 是读取结果。
	snapshot, err := service.GetStock(context.Background(), 7, " acc ", " item ")
	if err != nil || snapshot.Quantity != 3 || port.gotCookie != "acc" || port.gotItem != "item" || port.gotUser != 7 {
		t.Fatalf("snapshot=%+v err=%v port=%+v", snapshot, err, port)
	}
	// outcome 是修改结果。
	outcome, err := service.UpdateStock(context.Background(), 7, " acc ", " item ", StockUpdate{Quantity: 9, ExpectedQuantity: 3})
	if err != nil || !outcome.CookiePersisted || port.gotUpdate.Quantity != 9 || port.gotCookie != "acc" {
		t.Fatalf("outcome=%+v err=%v port=%+v", outcome, err, port)
	}
}

// TestItemStockServiceRejectsInvalidInputBeforePort 验证非法输入不会到达端口。
func TestItemStockServiceRejectsInvalidInputBeforePort(t *testing.T) {
	// port、service 是端口替身与被测服务。
	port := &stockPortStub{}
	// service 是被测的库存应用服务。
	service, _ := NewItemStockService(port)
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := service.GetStock(context.Background(), 0, "a", "b"); !errors.Is(err, ErrStockInvalidInput) {
		t.Fatalf("用户无效 err=%v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := service.GetStock(context.Background(), 1, " ", "b"); !errors.Is(err, ErrStockInvalidInput) {
		t.Fatalf("账号为空 err=%v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := (*ItemStockService)(nil).GetStock(context.Background(), 1, "a", "b"); !errors.Is(err, ErrStockInvalidInput) {
		t.Fatalf("nil 服务 err=%v", err)
	}
	// cases 是各类非法的库存修改。
	cases := map[string]StockUpdate{
		"单规格为0":   {Quantity: 0},
		"单规格超上限":  {Quantity: StockMax + 1},
		"单规格负原库存": {Quantity: 1, ExpectedQuantity: -1},
		"规格行无标识":  {SKUs: []StockSKUChange{{Key: " ", Quantity: 1}}},
		"规格行重复":   {SKUs: []StockSKUChange{{Key: "a", Quantity: 1}, {Key: "a", Quantity: 1}}},
		"规格为负":    {SKUs: []StockSKUChange{{Key: "a", Quantity: -1}}},
		"规格超上限":   {SKUs: []StockSKUChange{{Key: "a", Quantity: StockMax + 1}}},
		"规格负原库存":  {SKUs: []StockSKUChange{{Key: "a", Quantity: 1, ExpectedQuantity: -1}}},
		"规格全为0":   {SKUs: []StockSKUChange{{Key: "a"}, {Key: "b"}}},
	}
	// name、update 是当前用例。
	for name, update := range cases {
		// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
		if _, err := service.UpdateStock(context.Background(), 1, "a", "b", update); !errors.Is(err, ErrStockInvalidInput) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := service.UpdateStock(context.Background(), 1, "a", " ", StockUpdate{Quantity: 1}); !errors.Is(err, ErrStockInvalidInput) {
		t.Fatalf("商品为空 err=%v", err)
	}
	if port.getCalls != 0 || port.updateCalls != 0 {
		t.Fatalf("非法输入不应到达端口: %+v", port)
	}
}

// TestItemStockServiceAcceptsValidMultiSpec 验证合法的多规格修改（允许单行为 0）并透传端口错误。
func TestItemStockServiceAcceptsValidMultiSpec(t *testing.T) {
	// port、service 是端口替身与被测服务。
	port := &stockPortStub{err: ErrStockChanged}
	// service 是被测的库存应用服务。
	service, _ := NewItemStockService(port)
	// update 是两行规格，其中一行为 0。
	update := StockUpdate{SKUs: []StockSKUChange{{Key: "a", Quantity: 0, ExpectedQuantity: 2}, {Key: "b", Quantity: 5, ExpectedQuantity: 4}}}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := service.UpdateStock(context.Background(), 1, "a", "b", update); !errors.Is(err, ErrStockChanged) || port.updateCalls != 1 {
		t.Fatalf("err=%v calls=%d", err, port.updateCalls)
	}
}
