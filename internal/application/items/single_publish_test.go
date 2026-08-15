package items

import (
	"context"
	"errors"
	"testing"
)

// fakePublisher 是可控的平台端口替身，用于验证应用服务不会泄露平台 DTO。
type fakePublisher struct {
	// outcome 保存桩平台返回的业务结果。
	outcome PublishOutcome
	// err 保存桩平台返回的调用错误。
	err error
}

// Publish 返回预设的平台结果或错误。
func (p fakePublisher) Publish(_ context.Context, _ PublishInput) (PublishOutcome, error) {
	return p.outcome, p.err
}

// fakeItemRepository 是记录最后一次商品写入的本地仓储替身。
type fakeItemRepository struct {
	// record 保存最后一次收到的商品记录。
	record ItemRecord
	// err 保存桩仓储返回的写入错误。
	err error
}

// Upsert 保存记录并返回预设错误。
func (r *fakeItemRepository) Upsert(_ context.Context, record ItemRecord) error {
	r.record = record
	return r.err
}

// TestServicePublishSinglePersistsRemoteResult 验证平台成功后应用层写入本地商品。
func TestServicePublishSinglePersistsRemoteResult(t *testing.T) {
	// repository 是记录写入内容的本地仓储桩。
	repository := &fakeItemRepository{}
	// service 是使用平台和仓储替身构造的应用服务。
	service, err := NewService(fakePublisher{outcome: PublishOutcome{Result: &PublishResult{
		ItemID: "item-1", ItemURL: "https://example.invalid/item-1", Title: "标题",
		PriceText: "12.00", CategoryID: "cat-1", CategoryName: "资料", ImageURL: "image",
		Quantity: 2, RawData: map[string]any{"trace": "opaque"},
	}}}, repository)
	if err != nil {
		t.Fatalf("NewService error: %v", err)
	}
	// outcome 是应用服务返回的远端结果及本地状态。
	outcome, err := service.PublishSingle(context.Background(), PublishInput{CookieID: "acc-1", Description: "描述", Quantity: 2})
	if err != nil || outcome.Result == nil || outcome.Result.ItemID != "item-1" {
		t.Fatalf("unexpected outcome: %+v, err=%v", outcome, err)
	}
	if repository.record.ItemID != "item-1" || !repository.record.MultiQuantityDelivery {
		t.Fatalf("unexpected local record: %+v", repository.record)
	}
}

// TestServicePublishSingleKeepsRemoteError 验证平台失败时不写入本地商品。
func TestServicePublishSingleKeepsRemoteError(t *testing.T) {
	// repository 是记录是否被调用的本地仓储桩。
	repository := &fakeItemRepository{}
	// remoteErr 是平台调用失败的代表性错误。
	remoteErr := errors.New("platform unavailable")
	// service 是配置平台错误桩的应用服务。
	service, err := NewService(fakePublisher{err: remoteErr}, repository)
	if err != nil {
		t.Fatalf("NewService error: %v", err)
	}
	// outcome 是平台失败后应用服务保留的空结果。
	outcome, callErr := service.PublishSingle(context.Background(), PublishInput{})
	if !errors.Is(callErr, remoteErr) || outcome.Result != nil || repository.record.ItemID != "" {
		t.Fatalf("unexpected failure outcome: %+v, err=%v, record=%+v", outcome, callErr, repository.record)
	}
}

// TestNewServiceRejectsMissingPort 验证必需应用端口不能在运行时缺失。
func TestNewServiceRejectsMissingPort(t *testing.T) {
	// repository 是满足仓储端口的最小替身。
	repository := &fakeItemRepository{}
	// err 保存缺少发布端口时的构造错误。
	if _, err := NewService(nil, repository); err == nil {
		t.Fatal("缺少发布端口时应返回错误")
	}
	// err 保存缺少仓储端口时的构造错误。
	if _, err := NewService(fakePublisher{}, nil); err == nil {
		t.Fatal("缺少仓储端口时应返回错误")
	}
}
