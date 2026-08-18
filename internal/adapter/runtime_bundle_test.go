package adapter

import "testing"

// TestNewRuntimeBundle 验证生产运行时依赖在启动前一次性闭环装配，避免通过 setter 留下半成品。
func TestNewRuntimeBundle(t *testing.T) {
	// store、closeStore 是测试专用数据库及其资源释放函数。
	store, closeStore := newAdapterTestStore(t)
	defer closeStore()
	// bundle、err 是运行时闭环装配结果及其错误。
	bundle, err := NewRuntimeBundle(store, nil, nil)
	if err != nil {
		t.Fatalf("NewRuntimeBundle error: %v", err)
	}
	if bundle == nil || bundle.Adapter == nil || bundle.Manager == nil || bundle.Notifier == nil || bundle.Automation == nil || bundle.Chat == nil {
		t.Fatalf("运行时依赖未完整装配: %+v", bundle)
	}
	if bundle.Adapter.chat != bundle.Chat || bundle.Adapter.automation != bundle.Automation || bundle.Adapter.notifier != bundle.Notifier {
		t.Fatal("Adapter 未持有构造期固定的运行时协作依赖")
	}
}

// TestNewRuntimeBundleRejectsNilStore 验证缺少必需数据库依赖时在构造期失败，而非启动后延迟失效。
func TestNewRuntimeBundleRejectsNilStore(t *testing.T) {
	// bundle、err 是缺少数据库时的装配结果及失败原因。
	bundle, err := NewRuntimeBundle(nil, nil, nil)
	if err == nil || bundle != nil {
		t.Fatalf("缺少数据库应构造失败: bundle=%+v err=%v", bundle, err)
	}
}
