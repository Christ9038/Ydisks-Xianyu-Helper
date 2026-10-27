package library

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// newTestImageService 构造带内存依赖的图片服务。
func newTestImageService(t *testing.T) (*ImageService, *fakeImageRepository, *fakeBlobStore) {
	t.Helper()
	// repository、blobs 是内存依赖。
	repository, blobs := newFakeImageRepository(), newFakeBlobStore()
	// service 保存 NewImageService 的返回结果，err 保存其失败原因。
	service, err := NewImageService(repository, blobs, fakeInspector{})
	if err != nil {
		t.Fatal(err)
	}
	return service, repository, blobs
}

// TestNewImageServiceRejectsNilPorts 验证必需端口缺失时构造失败。
func TestNewImageServiceRejectsNilPorts(t *testing.T) {
	// err 保存 NewImageService 调用的失败原因。
	if _, err := NewImageService(nil, newFakeBlobStore(), fakeInspector{}); err == nil {
		t.Fatal("缺少仓储必须失败")
	}
}

// TestImageServiceUploadFlow 覆盖上传成功、去重、校验失败、数量限制与故障中断。
func TestImageServiceUploadFlow(t *testing.T) {
	// blobs、repository、service 保存 newTestImageService 的返回结果。
	service, repository, blobs := newTestImageService(t)
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	// err 保存 UploadImages 调用的失败原因。
	if _, err := service.UploadImages(ctx, 1, nil); err == nil {
		t.Fatal("空上传必须失败")
	}
	// tooMany 超过单次数量上限。
	tooMany := make([]ImageUpload, MaxUploadFiles+1)
	// err 保存 UploadImages 调用的失败原因。
	if _, err := service.UploadImages(ctx, 1, tooMany); err == nil {
		t.Fatal("超出数量必须失败")
	}
	// results 覆盖成功、重复内容、空文件、非图片和超大文件。
	results, err := service.UploadImages(ctx, 1, []ImageUpload{
		{Filename: "a.png", Data: []byte("IMG-a")},
		{Filename: "again.png", Data: []byte("IMG-a")},
		{Filename: "empty.png"},
		{Filename: "text.txt", Data: []byte("hello")},
		{Filename: "big.png", Data: append([]byte("IMG"), make([]byte, MaxImageBytes)...)},
	})
	if err != nil || len(results) != 5 {
		t.Fatalf("results=%d err=%v", len(results), err)
	}
	if results[0].Error != "" || results[0].Duplicate || results[0].Image.ID == 0 || !strings.HasPrefix(results[0].Image.StorageKey, "1/") || !strings.HasSuffix(results[0].Image.StorageKey, ".png") {
		t.Fatalf("首次上传异常: %+v", results[0])
	}
	if !results[1].Duplicate || results[1].Image.ID != results[0].Image.ID {
		t.Fatalf("重复内容应命中去重: %+v", results[1])
	}
	if results[2].Error == "" || results[3].Error == "" || !strings.Contains(results[4].Error, "10MB") {
		t.Fatalf("失败文件应记录错误: %+v", results[2:])
	}
	if len(blobs.files) != 1 {
		t.Fatalf("去重和失败文件不应产生额外字节: %d", len(blobs.files))
	}
	// 并发写入同内容：Insert 返回已有记录，视为重复。
	existing := results[0].Image
	repository.raceExisting = &existing
	// race 保存 UploadImages 的返回结果，err 保存其失败原因。
	race, err := service.UploadImages(ctx, 1, []ImageUpload{{Filename: "b.png", Data: []byte("IMG-b")}})
	if err != nil || !race[0].Duplicate || race[0].Image.ID != existing.ID {
		t.Fatalf("并发去重异常: %+v %v", race, err)
	}
	repository.raceExisting = nil
	// 元数据写入失败时回收刚写入的字节。
	repository.insertErr = errors.New("db down")
	// before 保存 len 的返回结果。
	before := len(blobs.files)
	// insertErr 保存 UploadImages 调用的失败原因。
	if _, insertErr := service.UploadImages(ctx, 1, []ImageUpload{{Filename: "c.png", Data: []byte("IMG-c")}}); insertErr == nil || len(blobs.files) != before {
		t.Fatalf("元数据失败应回收字节: %v files=%d want %d", insertErr, len(blobs.files), before)
	}
	repository.insertErr = nil
	// 存储与查询故障中断请求。
	blobs.saveErr = errors.New("disk full")
	// saveErr 保存 UploadImages 调用的失败原因。
	if _, saveErr := service.UploadImages(ctx, 1, []ImageUpload{{Filename: "d.png", Data: []byte("IMG-d")}}); saveErr == nil {
		t.Fatal("存储失败必须返回错误")
	}
	blobs.saveErr = nil
	repository.err = errors.New("db down")
	// findErr 保存 UploadImages 调用的失败原因。
	if _, findErr := service.UploadImages(ctx, 1, []ImageUpload{{Filename: "e.png", Data: []byte("IMG-e")}}); findErr == nil {
		t.Fatal("查询失败必须返回错误")
	}
}

// TestImageServiceListContentDelete 覆盖列表、内容读取、归属隔离与引用保护。
func TestImageServiceListContentDelete(t *testing.T) {
	// blobs、repository、service 保存 newTestImageService 的返回结果。
	service, repository, blobs := newTestImageService(t)
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	// results 保存 UploadImages 的返回结果。
	results, _ := service.UploadImages(ctx, 1, []ImageUpload{{Filename: "a.png", Data: []byte("IMG-a")}})
	// id 是刚上传图片的主键。
	id := results[0].Image.ID
	// list 保存 ListImages 的返回结果，err 保存其失败原因。
	if list, err := service.ListImages(ctx, 1); err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	// data、image 保存 ImageContent 的返回结果，err 保存其失败原因。
	if image, data, err := service.ImageContent(ctx, 1, id); err != nil || string(data) != "IMG-a" || image.ContentType != "image/png" {
		t.Fatalf("content=%v %q %v", image, data, err)
	}
	// err 保存 ImageContent 调用的失败原因。
	if _, _, err := service.ImageContent(ctx, 2, id); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("他人读取应不存在: %v", err)
	}
	blobs.readErr = errors.New("io")
	// err 保存 ImageContent 调用的失败原因。
	if _, _, err := service.ImageContent(ctx, 1, id); err == nil {
		t.Fatal("字节读取失败必须返回错误")
	}
	blobs.readErr = nil
	// 被模板引用时拒绝删除，并列出模板名。
	repository.refs = []TemplateImageRef{{TemplateName: "模板A", ImageIDs: []int64{id}}, {TemplateName: "模板B", ImageIDs: []int64{99}}}
	// inUse 保存图片被引用错误的类型化视图。
	var inUse *ImageInUseError
	// err 保存 DeleteImage 调用的失败原因。
	if err := service.DeleteImage(ctx, 1, id); !errors.As(err, &inUse) || len(inUse.Templates) != 1 || inUse.Templates[0] != "模板A" || !strings.Contains(inUse.Error(), "模板A") {
		t.Fatalf("引用保护异常: %v", err)
	}
	repository.refs = nil
	// err 保存 DeleteImage 调用的失败原因。
	if err := service.DeleteImage(ctx, 2, id); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("他人删除应不存在: %v", err)
	}
	repository.err = errors.New("db down")
	// err 保存 DeleteImage 调用的失败原因。
	if err := service.DeleteImage(ctx, 1, id); err == nil {
		t.Fatal("引用查询失败必须返回错误")
	}
	repository.err = nil
	// err 保存 DeleteImage 调用的失败原因。
	if err := service.DeleteImage(ctx, 1, id); err != nil || len(blobs.files) != 0 {
		t.Fatalf("删除应同时清理字节: %v files=%d", err, len(blobs.files))
	}
}
