package items

import (
	"context"
	"errors"
	"testing"
)

// platformLifecyclePortStub 记录应用服务传入的商品标识，并返回预设的平台下架结果。
type platformLifecyclePortStub struct {
	// outcome 是测试端口预设的商品下架结果。
	outcome DownShelfOutcome
	// err 是测试端口预设的下架错误。
	err error
	// userID、cookieID、itemID 保存应用服务转发的输入。
	userID   int64
	cookieID string
	itemID   string
}

// DownShelf 保存测试调用参数并返回预设的下架结果。
func (port *platformLifecyclePortStub) DownShelf(_ context.Context, userID int64, cookieID, itemID string) (DownShelfOutcome, error) {
	port.userID, port.cookieID, port.itemID = userID, cookieID, itemID
	return port.outcome, port.err
}

// TestPlatformLifecycleDownShelfValidatesAndDelegates 验证下架应用服务校验参数并保留平台结果。
func TestPlatformLifecycleDownShelfValidatesAndDelegates(t *testing.T) {
	// port 是记录调用参数的商品平台生命周期测试端口。
	port := &platformLifecyclePortStub{outcome: DownShelfOutcome{ItemID: "item-1", Status: "off_sale", PlatformSuccess: true, CookiePersisted: true}}
	// service 是使用测试端口构造的应用服务。
	service, err := NewPlatformLifecycleService(port)
	if err != nil {
		t.Fatal(err)
	}
	// outcome 是商品下架调用结果。
	outcome, err := service.DownShelf(context.Background(), 7, " cookie-1 ", " item-1 ")
	if err != nil || !outcome.PlatformSuccess || outcome.Status != "off_sale" {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if port.userID != 7 || port.cookieID != "cookie-1" || port.itemID != "item-1" {
		t.Fatalf("port received user=%d cookie=%q item=%q", port.userID, port.cookieID, port.itemID)
	}
}

// TestPlatformLifecycleDownShelfRejectsInvalidInput 验证无效身份或空标识不会触发平台端口。
func TestPlatformLifecycleDownShelfRejectsInvalidInput(t *testing.T) {
	// port 是不应被非法输入触发的测试端口。
	port := &platformLifecyclePortStub{}
	// service 是使用测试端口构造的应用服务。
	service, err := NewPlatformLifecycleService(port)
	if err != nil {
		t.Fatal(err)
	}
	// cases 是必须在应用层拒绝的输入集合。
	cases := []struct {
		userID   int64
		cookieID string
		itemID   string
	}{
		{userID: 0, cookieID: "cookie-1", itemID: "item-1"},
		{userID: 7, cookieID: "", itemID: "item-1"},
		{userID: 7, cookieID: "cookie-1", itemID: ""},
	}
	// testCase 是当前需要在应用层拒绝的用户、账号和商品标识组合。
	for _, testCase := range cases {
		// err 是当前非法输入被下架应用服务拒绝时的业务错误。
		if _, err := service.DownShelf(context.Background(), testCase.userID, testCase.cookieID, testCase.itemID); !errors.Is(err, ErrDownShelfInvalidInput) {
			t.Fatalf("case %+v should be invalid, err=%v", testCase, err)
		}
	}
	if port.itemID != "" {
		t.Fatalf("invalid input reached platform port: %+v", port)
	}
}
