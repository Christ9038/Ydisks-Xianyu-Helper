package adapter

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// TestOrderReconciliationRepositoryRecordsPending 验证补偿应用模型能写入数据库并返回记录标识。
func TestOrderReconciliationRepositoryRecordsPending(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及资源清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 保存绑定测试数据库的订单补偿适配器。
	repository := NewOrderReconciliationRepository(store)
	// recordID、err 保存补偿记录写入结果及错误。
	recordID, err := repository.RecordReconciliation(context.Background(), "order-1", "cookie-1", "manual_status_ship", "本地写入失败")
	if err != nil || recordID == "" {
		t.Fatalf("创建补偿记录失败: id=%q err=%v", recordID, err)
	}
	// records、listErr 保存数据库扫描到的待补偿记录及错误。
	records, listErr := store.Reconciliations.ListPending(context.Background(), 10)
	if listErr != nil || len(records) != 1 || records[0].ID != recordID || records[0].OrderID != "order-1" || records[0].Kind != "manual_status_ship" {
		t.Fatalf("补偿记录映射异常: records=%+v err=%v", records, listErr)
	}
}

// TestOrderReconciliationRepositoryRejectsMissingDependency 验证缺少数据库依赖时适配器快速失败。
func TestOrderReconciliationRepositoryRejectsMissingDependency(t *testing.T) {
	// repository 保存未装配数据库时返回的空适配器。
	repository := NewOrderReconciliationRepository(nil)
	// recordID、err 保存缺少依赖时的写入结果。
	recordID, err := repository.RecordReconciliation(context.Background(), "order-1", "cookie-1", "manual_status_ship", "失败")
	if recordID != "" || err == nil {
		t.Fatalf("缺少数据库时应返回装配错误: id=%q err=%v", recordID, err)
	}
}

// TestOrderReconciliationRepositoryPropagatesPersistenceError 验证数据库关闭后的错误不会被伪装为成功。
func TestOrderReconciliationRepositoryPropagatesPersistenceError(t *testing.T) {
	// store、cleanup 保存随后关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 保存绑定已关闭数据库的订单补偿适配器。
	repository := NewOrderReconciliationRepository(store)
	// closeErr 表示关闭测试数据库连接时产生的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// recordID、err 保存底层数据库故障的写入结果。
	recordID, err := repository.RecordReconciliation(context.Background(), "order-1", "cookie-1", "manual_status_ship", "失败")
	if recordID != "" || err == nil || errors.Is(err, orderapp.ErrNotFound) {
		t.Fatalf("数据库故障应透传持久化错误: id=%q err=%v", recordID, err)
	}
}
