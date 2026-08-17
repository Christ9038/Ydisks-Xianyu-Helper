package automation

import "testing"

// TestNewWithDependenciesFixesCenterDependencies 验证自动化中心在构造阶段固定所有生产协作依赖。
func TestNewWithDependenciesFixesCenterDependencies(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// sender 保存中心使用的消息发送器提供者。
	sender := testSenderProvider{sender: &testSender{}}
	// fetcher 保存同时提供订单详情与凭证恢复能力的测试依赖。
	fetcher := &fakeCredentialRecoverer{store: store}
	// notifier 保存发货通知测试依赖。
	notifier := &recordingNotifier{}
	// client 保存确认发货使用的协议客户端。
	client := &fakeMTop{}
	// taskClient 保存账号任务使用的独立协议客户端。
	taskClient := &fakeAccountTaskClient{}
	// center 保存通过显式依赖构造器创建的自动化中心。
	center := NewWithDependencies(store, sender, nil, CenterDependencies{
		MTop:               client,
		AccountTaskClient:  taskClient,
		OrderDetailFetcher: fetcher,
		Notifier:           notifier,
	})
	if center.dependencies.mtop != client || center.dependencies.accountTaskClient != taskClient {
		t.Fatal("协议依赖未在构造阶段固定")
	}
	if center.dependencies.fetcher != fetcher || center.dependencies.recoverer != fetcher || center.dependencies.notifier != notifier {
		t.Fatal("订单详情、凭证恢复或通知依赖未在构造阶段固定")
	}
	if center.dependencies.cookieSrc != nil {
		t.Fatal("未注入 CookieSource 时不应保存读取函数")
	}
	if center.actions.store != store || center.actions.senders != sender {
		t.Fatal("动作执行器未继承中心构造依赖")
	}
}

// TestNewWithDependenciesCopiesConfiguration 验证构造器复制依赖字段，调用方后续修改配置结构不会改变中心状态。
func TestNewWithDependenciesCopiesConfiguration(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// baseClient 是构造阶段固定的协议客户端。
	baseClient := &fakeMTop{}
	// replacementClient 是调用方在构造完成后写回配置结构的无关客户端。
	replacementClient := &fakeMTop{}
	// dependencies 保存交给构造器的外部依赖配置。
	dependencies := CenterDependencies{MTop: baseClient}
	// center 保存使用构造期配置创建的自动化中心。
	center := NewWithDependencies(store, nil, nil, dependencies)
	dependencies.MTop = replacementClient
	if center.dependencies.mtop != baseClient {
		t.Fatal("构造完成后的配置修改不应改写中心依赖")
	}
}
