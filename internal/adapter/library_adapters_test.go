package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	libraryapp "xianyu-go/internal/application/library"
)

// TestLibraryRepositoriesRoundTrip 覆盖图片与模板仓储的写入、读取、JSON 编解码、引用统计和不存在映射。
func TestLibraryRepositoriesRoundTrip(t *testing.T) {
	// cleanup、store 保存 newDeliveryTemplateAdapterStore 的返回结果。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	defer cleanup()
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	// userID 是外键所需的用户主键。
	result, err := store.DB.ExecContext(ctx, `INSERT INTO users (username, email, password_hash) VALUES ('u','u@example.test','x')`)
	if err != nil {
		t.Fatal(err)
	}
	// userID 保存 LastInsertId 的返回结果。
	userID, _ := result.LastInsertId()
	// images 保存 NewLibraryImageRepository 的返回结果。
	images := NewLibraryImageRepository(store)
	// templates 保存 NewLibraryTemplateRepository 的返回结果。
	templates := NewLibraryTemplateRepository(store)

	// created、first 保存 Insert 的返回结果，err 保存其失败原因。
	first, created, err := images.Insert(ctx, libraryapp.Image{UserID: userID, Filename: "a.png", StorageKey: "k1", ContentType: "image/png", SizeBytes: 3, SHA256: "s1"})
	if err != nil || !created || first.ID == 0 {
		t.Fatalf("insert=%+v created=%v err=%v", first, created, err)
	}
	// second 保存 Insert 的返回结果。
	second, _, _ := images.Insert(ctx, libraryapp.Image{UserID: userID, Filename: "b.png", StorageKey: "k2", SHA256: "s2"})
	// list 保存 List 的返回结果，listErr 保存其失败原因。
	if list, listErr := images.List(ctx, userID); listErr != nil || len(list) != 2 {
		t.Fatalf("list=%v %v", list, listErr)
	}
	// many 保存 GetMany 的返回结果，manyErr 保存其失败原因。
	if many, manyErr := images.GetMany(ctx, userID, []int64{first.ID, 999}); manyErr != nil || len(many) != 1 {
		t.Fatalf("many=%v %v", many, manyErr)
	}
	// got 保存 Get 的返回结果，getErr 保存其失败原因。
	if got, getErr := images.Get(ctx, userID, first.ID); getErr != nil || got.SHA256 != "s1" {
		t.Fatalf("get=%v %v", got, getErr)
	}
	// getErr 保存 Get 调用的失败原因。
	if _, getErr := images.Get(ctx, userID, 999); !errors.Is(getErr, libraryapp.ErrImageNotFound) {
		t.Fatalf("missing get err=%v", getErr)
	}
	// findErr 保存 FindBySHA256 调用的失败原因。
	if _, findErr := images.FindBySHA256(ctx, userID, "nope"); !errors.Is(findErr, libraryapp.ErrImageNotFound) {
		t.Fatalf("missing sha err=%v", findErr)
	}
	// found 保存 FindBySHA256 的返回结果，findErr 保存其失败原因。
	if found, findErr := images.FindBySHA256(ctx, userID, "s2"); findErr != nil || found.ID != second.ID {
		t.Fatalf("find=%v %v", found, findErr)
	}

	// template 同时含规格、规格图、自动化，验证 JSON 往返。
	template := libraryapp.Template{UserID: userID, Name: "模板", Title: "标题", PriceCents: 900, Quantity: 5, PostageMode: "free",
		Category: itemapp.BatchPreviewCategory{CatID: "1", CatName: "n", ChannelCatID: "c"}, ImageIDs: []int64{first.ID},
		Specs:      &libraryapp.TemplateSpecs{Specs: []itemapp.PublishSpec{{PropertyName: "颜色", Values: []itemapp.PublishSpecValue{{Value: "红", ImageIndex: 0}}}}, SpecImageIDs: []int64{second.ID}},
		Automation: itemapp.BatchPreviewAutomation{PaidDelivery: itemapp.BatchPreviewCardAutomation{Enabled: true, Actions: []itemapp.BatchPreviewCardAction{{CardID: 7, DeliveryCount: 1}}}}}
	// id 保存 Create 的返回结果，err 保存其失败原因。
	id, err := templates.Create(ctx, template)
	if err != nil {
		t.Fatal(err)
	}
	// got 保存 Get 的返回结果，err 保存其失败原因。
	got, err := templates.Get(ctx, userID, id)
	if err != nil || got.Specs == nil || got.Specs.SpecImageIDs[0] != second.ID || got.Category.CatID != "1" || !got.Automation.PaidDelivery.Enabled || got.CreatedAt == "" {
		t.Fatalf("template=%+v err=%v", got, err)
	}
	// 单规格模板、nil 图片切片编码为 []。
	plain := libraryapp.Template{UserID: userID, Name: "单", Title: "t", PriceCents: 1, Quantity: 1, PostageMode: "free"}
	// plainID 保存 Create 的返回结果。
	plainID, _ := templates.Create(ctx, plain)
	// plainGot 保存 Get 的返回结果。
	plainGot, _ := templates.Get(ctx, userID, plainID)
	if plainGot.Specs != nil || len(plainGot.ImageIDs) != 0 {
		t.Fatalf("单规格模板异常: %+v", plainGot)
	}
	got.Title = "新标题"
	// updateErr 保存 Update 调用的失败原因。
	if updateErr := templates.Update(ctx, got); updateErr != nil {
		t.Fatal(updateErr)
	}
	// list 保存 List 的返回结果，listErr 保存其失败原因。
	if list, listErr := templates.List(ctx, userID); listErr != nil || len(list) != 2 {
		t.Fatalf("list=%v %v", list, listErr)
	}
	// 引用统计同时包含主图和规格图。
	refs, err := images.TemplateImageRefs(ctx, userID)
	if err != nil || len(refs) != 2 {
		t.Fatalf("refs=%v %v", refs, err)
	}
	// getErr 保存 Get 调用的失败原因。
	if _, getErr := templates.Get(ctx, userID, 999); !errors.Is(getErr, libraryapp.ErrTemplateNotFound) {
		t.Fatalf("missing template err=%v", getErr)
	}
	// updateErr 保存 Update 调用的失败原因。
	if updateErr := templates.Update(ctx, libraryapp.Template{ID: 999, UserID: userID}); !errors.Is(updateErr, libraryapp.ErrTemplateNotFound) {
		t.Fatalf("update missing err=%v", updateErr)
	}
	// deleteErr 保存 Delete 调用的失败原因。
	if deleteErr := templates.Delete(ctx, userID, id); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// deleteErr 保存 Delete 调用的失败原因。
	if deleteErr := templates.Delete(ctx, userID, id); !errors.Is(deleteErr, libraryapp.ErrTemplateNotFound) {
		t.Fatalf("delete missing err=%v", deleteErr)
	}
	// deleteErr 保存 Delete 调用的失败原因。
	if deleteErr := images.Delete(ctx, userID, first.ID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// deleteErr 保存 Delete 调用的失败原因。
	if deleteErr := images.Delete(ctx, userID, first.ID); !errors.Is(deleteErr, libraryapp.ErrImageNotFound) {
		t.Fatalf("delete missing image err=%v", deleteErr)
	}

	// 损坏的 JSON 列必须作为错误暴露，不能被静默当作无引用。
	for column, value := range map[string]string{"category_json": "{", "image_ids_json": "{", "automation_json": "{", "specs_json": "{"} {
		// execErr 保存 ExecContext 调用的失败原因。
		if _, execErr := store.DB.ExecContext(ctx, `UPDATE product_templates SET `+column+`=? WHERE id=?`, value, plainID); execErr != nil {
			t.Fatal(execErr)
		}
		// getErr 保存 Get 调用的失败原因。
		if _, getErr := templates.Get(ctx, userID, plainID); getErr == nil {
			t.Fatalf("%s 损坏必须报错", column)
		}
		// listErr 保存 List 调用的失败原因。
		if _, listErr := templates.List(ctx, userID); listErr == nil {
			t.Fatalf("%s 损坏时列表必须报错", column)
		}
		// refsErr 保存 TemplateImageRefs 调用的失败原因。
		if _, refsErr := images.TemplateImageRefs(ctx, userID); refsErr == nil {
			t.Fatalf("%s 损坏时引用统计必须报错", column)
		}
		// reset 是破坏性迁移后各 JSON 列应恢复的默认值。
		reset := map[string]string{"category_json": "{}", "image_ids_json": "[]", "automation_json": "{}", "specs_json": "{}"}[column]
		_, _ = store.DB.ExecContext(ctx, `UPDATE product_templates SET `+column+`=? WHERE id=?`, reset, plainID)
	}
}

// TestLibraryRepositoriesRequireStore 验证未装配依赖时返回初始化错误而不是 panic。
func TestLibraryRepositoriesRequireStore(t *testing.T) {
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	// images、templates 是被验证 JSON 损坏行为的图片与模板仓库。
	images, templates := &LibraryImageRepository{}, &LibraryTemplateRepository{}
	// err 保存 List 调用的失败原因。
	if _, err := images.List(ctx, 1); err == nil {
		t.Fatal("List")
	}
	// err 保存 Get 调用的失败原因。
	if _, err := images.Get(ctx, 1, 1); err == nil {
		t.Fatal("Get")
	}
	// err 保存 GetMany 调用的失败原因。
	if _, err := images.GetMany(ctx, 1, nil); err == nil {
		t.Fatal("GetMany")
	}
	// err 保存 FindBySHA256 调用的失败原因。
	if _, err := images.FindBySHA256(ctx, 1, ""); err == nil {
		t.Fatal("FindBySHA256")
	}
	// err 保存 Insert 调用的失败原因。
	if _, _, err := images.Insert(ctx, libraryapp.Image{}); err == nil {
		t.Fatal("Insert")
	}
	// err 保存 Delete 调用的失败原因。
	if err := images.Delete(ctx, 1, 1); err == nil {
		t.Fatal("Delete")
	}
	// err 保存 TemplateImageRefs 调用的失败原因。
	if _, err := images.TemplateImageRefs(ctx, 1); err == nil {
		t.Fatal("TemplateImageRefs")
	}
	// err 保存 List 调用的失败原因。
	if _, err := templates.List(ctx, 1); err == nil {
		t.Fatal("templates List")
	}
	// err 保存 Get 调用的失败原因。
	if _, err := templates.Get(ctx, 1, 1); err == nil {
		t.Fatal("templates Get")
	}
	// err 保存 Create 调用的失败原因。
	if _, err := templates.Create(ctx, libraryapp.Template{}); err == nil {
		t.Fatal("templates Create")
	}
	// err 保存 Update 调用的失败原因。
	if err := templates.Update(ctx, libraryapp.Template{}); err == nil {
		t.Fatal("templates Update")
	}
	// err 保存 Delete 调用的失败原因。
	if err := templates.Delete(ctx, 1, 1); err == nil {
		t.Fatal("templates Delete")
	}
	if (*ItemDependencies)(nil).NewLibraryImageRepository() != nil || (*ItemDependencies)(nil).NewLibraryTemplateRepository() != nil {
		t.Fatal("nil 依赖应返回 nil")
	}
	// 非 nil 依赖构造出可用适配器。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	defer cleanup()
	// dependencies 保存 NewItemDependencies 的返回结果，err 保存其失败原因。
	dependencies, err := NewItemDependencies(store)
	if err != nil || dependencies.NewLibraryImageRepository() == nil || dependencies.NewLibraryTemplateRepository() == nil {
		t.Fatalf("依赖构造异常: %v", err)
	}
}

// libraryKey 是符合存储键格式的测试键。
const libraryKey = "1/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"

// TestLibraryFileStoreSaveReadRemove 覆盖字节存储的读写删除、键校验和目录延迟创建。
func TestLibraryFileStoreSaveReadRemove(t *testing.T) {
	// root 保存 Join 的返回结果。
	root := filepath.Join(t.TempDir(), "uploads")
	// store 保存 NewLibraryFileStore 的返回结果，err 保存其失败原因。
	store, err := NewLibraryFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// statErr 保存 Stat 调用的失败原因。
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatal("构造不应产生 I/O")
	}
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	// saveErr 保存 Save 调用的失败原因。
	if saveErr := store.Save(ctx, libraryKey, []byte("IMG")); saveErr != nil {
		t.Fatal(saveErr)
	}
	// data 保存 Read 的返回结果，readErr 保存其失败原因。
	if data, readErr := store.Read(ctx, libraryKey); readErr != nil || string(data) != "IMG" {
		t.Fatalf("read=%q %v", data, readErr)
	}
	// statErr 保存 Stat 调用的失败原因。
	if _, statErr := os.Stat(filepath.Join(root, "library", libraryKey+".tmp")); !os.IsNotExist(statErr) {
		t.Fatal("临时文件应已被重命名")
	}
	// removeErr 保存 Remove 调用的失败原因。
	if removeErr := store.Remove(ctx, libraryKey); removeErr != nil {
		t.Fatal(removeErr)
	}
	// removeErr 保存 Remove 调用的失败原因。
	if removeErr := store.Remove(ctx, libraryKey); removeErr != nil {
		t.Fatalf("重复删除应视为成功: %v", removeErr)
	}
	// readErr 保存 Read 调用的失败原因。
	if _, readErr := store.Read(ctx, libraryKey); readErr == nil {
		t.Fatal("已删除文件读取必须失败")
	}
	// 非法键一律拒绝，防止路径穿越。
	for _, bad := range []string{"", "../x.png", "1/../../x.png", "/abs.png", "1/short.png", "a/" + strings.Repeat("a", 64) + ".png"} {
		if store.Save(ctx, bad, nil) == nil {
			t.Fatalf("Save 应拒绝 %q", bad)
		}
		// readErr 保存 Read 调用的失败原因。
		if _, readErr := store.Read(ctx, bad); readErr == nil {
			t.Fatalf("Read 应拒绝 %q", bad)
		}
		if store.Remove(ctx, bad) == nil {
			t.Fatalf("Remove 应拒绝 %q", bad)
		}
	}
	// newErr 保存 NewLibraryFileStore 调用的失败原因。
	if _, newErr := NewLibraryFileStore(" "); newErr == nil {
		t.Fatal("空根目录必须失败")
	}
	// 根目录是文件时创建资源库目录失败。
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	// blocked 保存 NewLibraryFileStore 的返回结果。
	blocked, _ := NewLibraryFileStore(blocker)
	if blocked.Save(ctx, libraryKey, []byte("x")) == nil {
		t.Fatal("根目录不可用时 Save 必须失败")
	}
	// readErr 保存 Read 调用的失败原因。
	if _, readErr := blocked.Read(ctx, libraryKey); readErr == nil {
		t.Fatal("根目录不可用时 Read 必须失败")
	}
	if blocked.Remove(ctx, libraryKey) == nil {
		t.Fatal("根目录不可用时 Remove 必须失败")
	}
}

// TestLibraryFileStoreBatchSnapshots 覆盖批次目录创建、快照复制、回收和越界保护。
func TestLibraryFileStoreBatchSnapshots(t *testing.T) {
	// root 保存 Join 的返回结果。
	root := filepath.Join(t.TempDir(), "uploads")
	// store 保存 NewLibraryFileStore 的返回结果。
	store, _ := NewLibraryFileStore(root)
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	_ = store.Save(ctx, libraryKey, []byte("IMG"))
	// dir 保存 Prepare 的返回结果，err 保存其失败原因。
	dir, err := store.Prepare(ctx, "batch_abc")
	if err != nil || dir != filepath.Join(root, "publish_batches", "batch_abc") {
		t.Fatalf("dir=%q err=%v", dir, err)
	}
	// copyErr 保存 CopyImage 调用的失败原因。
	if copyErr := store.CopyImage(ctx, dir, libraryKey, "lib_1.png"); copyErr != nil {
		t.Fatal(copyErr)
	}
	// data 保存 ReadFile 的返回结果。
	if data, _ := os.ReadFile(filepath.Join(dir, "lib_1.png")); string(data) != "IMG" {
		t.Fatalf("快照内容异常: %q", data)
	}
	// 非法批次标识、文件名、目录和缺失源文件都被拒绝。
	if _, prepareErr := store.Prepare(ctx, "../evil"); prepareErr == nil {
		t.Fatal("非法批次标识必须失败")
	}
	// name 是遍历 []string 时的当前元素。
	for _, name := range []string{"", "..", "../x", "a/b"} {
		if store.CopyImage(ctx, dir, libraryKey, name) == nil {
			t.Fatalf("CopyImage 应拒绝文件名 %q", name)
		}
	}
	// outside 是遍历 []string 时的当前元素。
	for _, outside := range []string{root, filepath.Join(root, "publish_batches"), filepath.Join(root, "library"), t.TempDir()} {
		if store.CopyImage(ctx, outside, libraryKey, "x.png") == nil {
			t.Fatalf("CopyImage 应拒绝目录 %q", outside)
		}
		if store.Discard(ctx, outside) == nil {
			t.Fatalf("Discard 应拒绝目录 %q", outside)
		}
	}
	if store.CopyImage(ctx, dir, "1/"+strings.Repeat("b", 64)+".png", "y.png") == nil {
		t.Fatal("源文件缺失必须失败")
	}
	// discardErr 保存 Discard 调用的失败原因。
	if discardErr := store.Discard(ctx, dir); discardErr != nil {
		t.Fatal(discardErr)
	}
	// statErr 保存 Stat 调用的失败原因。
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatal("批次目录应已删除")
	}
	// 上传根目录不可用时 Prepare 失败。
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	// blocked 保存 NewLibraryFileStore 的返回结果。
	blocked, _ := NewLibraryFileStore(blocker)
	// prepareErr 保存 Prepare 调用的失败原因。
	if _, prepareErr := blocked.Prepare(ctx, "batch_x"); prepareErr == nil {
		t.Fatal("根目录不可用时 Prepare 必须失败")
	}
}

// TestLibraryImageInspector 覆盖受支持格式、拒绝格式与空内容。
func TestLibraryImageInspector(t *testing.T) {
	// samples 保存各格式的最小魔数及期望扩展名。
	samples := map[string]struct {
		// data 是魔数字节。
		data []byte
		// ext 是期望扩展名。
		ext string
	}{
		"png":  {[]byte("\x89PNG\r\n\x1a\n0000"), "png"},
		"jpeg": {[]byte("\xff\xd8\xff\xe0\x00\x10JFIF"), "jpg"},
		"gif":  {[]byte("GIF89a0000"), "gif"},
		"webp": {[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "webp"},
	}
	// name、sample 是当前格式样本。
	for name, sample := range samples {
		// ext、err 是图片类型嗅探得到的扩展名及其失败原因。
		if _, ext, err := (LibraryImageInspector{}).Inspect(sample.data); err != nil || ext != sample.ext {
			t.Fatalf("%s: ext=%q err=%v", name, ext, err)
		}
	}
	// bad 是遍历 [][]byte 时的当前元素。
	for _, bad := range [][]byte{nil, []byte("hello world"), []byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"), []byte("BM0000000000")} {
		// err 是嗅探非图片内容时应返回的失败原因。
		if _, _, err := (LibraryImageInspector{}).Inspect(bad); err == nil {
			t.Fatalf("应拒绝 %q", bad)
		}
	}
}
