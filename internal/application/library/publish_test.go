package library

import (
	"context"
	"errors"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// fakeSnapshotStore 记录快照复制与回收调用。
type fakeSnapshotStore struct {
	// prepared 保存 Prepare 收到的批次标识。
	prepared []string
	// copied 保存复制的「存储键→文件名」。
	copied map[string]string
	// discarded 保存被回收的目录。
	discarded []string
	// prepareErr、copyErr 模拟故障。
	prepareErr, copyErr error
}

// Prepare 返回固定目录。
func (f *fakeSnapshotStore) Prepare(_ context.Context, batchID string) (string, error) {
	f.prepared = append(f.prepared, batchID)
	return "/uploads/" + batchID, f.prepareErr
}

// CopyImage 记录复制。
func (f *fakeSnapshotStore) CopyImage(_ context.Context, _ string, key, name string) error {
	if f.copyErr != nil {
		return f.copyErr
	}
	if f.copied == nil {
		f.copied = map[string]string{}
	}
	f.copied[name] = key
	return nil
}

// Discard 记录回收。
func (f *fakeSnapshotStore) Discard(_ context.Context, dir string) error {
	f.discarded = append(f.discarded, dir)
	return nil
}

// fakeRowsValidator 记录收到的行并返回预置结果。
type fakeRowsValidator struct {
	// input、rows 记录最近一次调用参数。
	input itemapp.BatchPreviewInput
	// rows 是收到的行。
	rows []itemapp.BatchPreviewRow
	// err 模拟校验故障。
	err error
}

// PreviewRows 原样返回收到的行。
func (f *fakeRowsValidator) PreviewRows(_ context.Context, input itemapp.BatchPreviewInput, rows []itemapp.BatchPreviewRow) ([]itemapp.BatchPreviewRow, error) {
	f.input, f.rows = input, rows
	return rows, f.err
}

// fakePersister 记录落库的批次。
type fakePersister struct {
	// batch 是最近一次落库的批次。
	batch itemapp.BatchPreviewPersistenceBatch
	// err 模拟落库故障。
	err error
}

// Persist 记录批次并返回成功结果。
func (f *fakePersister) Persist(_ context.Context, batch itemapp.BatchPreviewPersistenceBatch, rows []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error) {
	f.batch = batch
	return itemapp.BatchPreviewPersistenceResult{Success: true, PreviewID: batch.ID, Total: len(rows)}, f.err
}

// publishFixture 聚合发布预检测试依赖。
type publishFixture struct {
	// service 是待测服务。
	service *TemplatePublishService
	// templates、images 是内存仓储。
	templates *fakeTemplateRepository
	// images 是内存图片仓储。
	images *fakeImageRepository
	// snapshots、validator、persister 是端口替身。
	snapshots *fakeSnapshotStore
	// validator 是行校验替身。
	validator *fakeRowsValidator
	// persister 是落库替身。
	persister *fakePersister
}

// newPublishFixture 构造夹具，预置用户 1 的图片 1..3 与两个模板（单规格 ID1、多规格 ID2）。
func newPublishFixture(t *testing.T, maxRows int) publishFixture {
	t.Helper()
	// f 是发布预检测试的依赖夹具。
	f := publishFixture{templates: newFakeTemplateRepository(), images: newFakeImageRepository(), snapshots: &fakeSnapshotStore{}, validator: &fakeRowsValidator{}, persister: &fakePersister{}}
	// id 保存 int64 的返回结果。
	for id := int64(1); id <= 3; id++ {
		f.images.images[id] = Image{ID: id, UserID: 1, StorageKey: "1/" + string(rune('a'+id)) + ".png"}
	}
	// single 保存 validTemplate 的返回结果。
	single := validTemplate()
	single.ID, single.OriginalPriceCents, single.PostageMode, single.PostageCents = 1, 2050, "fixed", 800
	single.Name, single.Title = "单", "单规格"
	// multi 保存 validSpecTemplate 的返回结果。
	multi := validSpecTemplate()
	multi.ID, multi.PriceCents, multi.Quantity, multi.Name, multi.Title = 2, 900, 5, "多", "多规格"
	f.templates.templates[1], f.templates.templates[2] = single, multi
	// service 保存 NewTemplatePublishService 的返回结果，err 保存其失败原因。
	service, err := NewTemplatePublishService(f.templates, f.images, f.snapshots, f.validator, f.persister, func() string { return "batch_x" }, maxRows)
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	return f
}

// TestNewTemplatePublishServiceRejectsNilPorts 验证必需依赖缺失时构造失败。
func TestNewTemplatePublishServiceRejectsNilPorts(t *testing.T) {
	// err 保存 NewTemplatePublishService 调用的失败原因。
	if _, err := NewTemplatePublishService(nil, nil, nil, nil, nil, nil, 0); err == nil {
		t.Fatal("缺少依赖必须失败")
	}
}

// TestTemplatePublishPreviewExpandsTemplatesAndAccounts 验证「模板 × 账号」展开、图片快照、多规格行与批次元数据。
func TestTemplatePublishPreviewExpandsTemplatesAndAccounts(t *testing.T) {
	// f 保存 newPublishFixture 的返回结果。
	f := newPublishFixture(t, 50)
	// result 保存 PreviewPublish 的返回结果，err 保存其失败原因。
	result, err := f.service.PreviewPublish(context.Background(), PublishPreviewInput{UserID: 1, TemplateIDs: []int64{1, 2, 1}, CookieIDs: []string{" a ", "b", "a", ""},
		Location: itemapp.Location{City: "杭州"}})
	if err != nil || result.PreviewID != "batch_x" || result.Total != 4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// rows 是交给校验器的展开行：模板 1 的 a、b，模板 2 的 a、b。
	rows := f.validator.rows
	if len(rows) != 4 || rows[0].CookieID != "a" || rows[1].CookieID != "b" || rows[2].Title != "多规格" {
		t.Fatalf("展开顺序异常: %+v", rows)
	}
	if rows[0].Price != "10.00" || rows[0].OriginalPrice != "20.50" || rows[0].Postage != "8.00" || rows[0].Specs != nil {
		t.Fatalf("单规格行字段异常: %+v", rows[0])
	}
	if rows[2].Price != "9.00" || rows[2].OriginalPrice != "" || rows[2].Specs == nil || rows[2].Specs.SpecImages[0] != "lib_3.png" || rows[2].Quantity != 5 {
		t.Fatalf("多规格行字段异常: %+v", rows[2])
	}
	if len(rows[0].Images) != 2 || rows[0].Images[0] != "lib_1.png" || len(f.snapshots.copied) != 3 {
		t.Fatalf("图片快照异常: %v %v", rows[0].Images, f.snapshots.copied)
	}
	if f.validator.input.UploadDir != "/uploads/batch_x" || f.validator.input.DefaultCookieID != "a" {
		t.Fatalf("校验上下文异常: %+v", f.validator.input)
	}
	// batch 是落库的批次元数据：默认间隔 5 秒、状态 preview。
	batch := f.persister.batch
	if batch.UploadDir != "/uploads/batch_x" || batch.PublishIntervalSeconds != 5 || batch.Status != "preview" || batch.Location.City != "杭州" || batch.DefaultCookieID != "a" || batch.UserID != 1 {
		t.Fatalf("批次元数据异常: %+v", batch)
	}
	if len(f.snapshots.discarded) != 0 {
		t.Fatal("成功时不应回收目录")
	}
	// 自定义间隔被保留。
	if _, err := f.service.PreviewPublish(context.Background(), PublishPreviewInput{UserID: 1, TemplateIDs: []int64{1}, CookieIDs: []string{"a"}, PublishIntervalSeconds: 9}); err != nil || f.persister.batch.PublishIntervalSeconds != 9 {
		t.Fatalf("自定义间隔异常: %v %+v", err, f.persister.batch)
	}
}

// TestTemplatePublishPreviewFailures 覆盖输入校验、归属、图片缺失与各依赖故障后的目录回收。
func TestTemplatePublishPreviewFailures(t *testing.T) {
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	// f 保存 newPublishFixture 的返回结果。
	f := newPublishFixture(t, 3)
	// 输入校验：缺少模板、账号，以及超过行数上限。
	for name, input := range map[string]PublishPreviewInput{
		"无模板": {UserID: 1, CookieIDs: []string{"a"}},
		"无账号": {UserID: 1, TemplateIDs: []int64{1}},
		"超上限": {UserID: 1, TemplateIDs: []int64{1, 2}, CookieIDs: []string{"a", "b"}},
	} {
		// err 保存 PreviewPublish 调用的失败原因。
		if _, err := f.service.PreviewPublish(ctx, input); !errors.As(err, new(*ValidationError)) {
			t.Fatalf("%s 应为校验错误: %v", name, err)
		}
	}
	// err 保存 PreviewPublish 调用的失败原因。
	if _, err := f.service.PreviewPublish(ctx, PublishPreviewInput{UserID: 2, TemplateIDs: []int64{1}, CookieIDs: []string{"a"}}); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("他人模板应不存在: %v", err)
	}
	if len(f.snapshots.prepared) != 0 {
		t.Fatal("输入阶段失败不应创建批次目录")
	}
	// ok 是合法输入。
	ok := PublishPreviewInput{UserID: 1, TemplateIDs: []int64{1}, CookieIDs: []string{"a"}}
	// 以下每个故障都应回收已创建的批次目录。
	steps := []struct {
		// name 是故障点。
		name string
		// arm 注入故障。
		arm func()
		// disarm 清除故障。
		disarm func()
		// want 是期望错误片段。
		want string
	}{
		{"图片被删除", func() { delete(f.images.images, 2) }, func() { f.images.images[2] = Image{ID: 2, UserID: 1, StorageKey: "1/c.png"} }, "已被删除"},
		{"图片仓储故障", func() { f.images.err = errors.New("db down") }, func() { f.images.err = nil }, "db down"},
		{"复制失败", func() { f.snapshots.copyErr = errors.New("disk") }, func() { f.snapshots.copyErr = nil }, "disk"},
		{"校验故障", func() { f.validator.err = errors.New("validate") }, func() { f.validator.err = nil }, "validate"},
		{"落库故障", func() { f.persister.err = errors.New("persist") }, func() { f.persister.err = nil }, "persist"},
	}
	// step 是当前故障点。
	for _, step := range steps {
		step.arm()
		// before 保存 len 的返回结果。
		before := len(f.snapshots.discarded)
		// err 保存 PreviewPublish 调用的失败原因。
		if _, err := f.service.PreviewPublish(ctx, ok); err == nil || !strings.Contains(err.Error(), step.want) || len(f.snapshots.discarded) != before+1 {
			t.Fatalf("%s: err=%v discarded=%d", step.name, err, len(f.snapshots.discarded)-before)
		}
		step.disarm()
	}
	// 目录准备失败直接返回。
	f.snapshots.prepareErr = errors.New("mkdir")
	// err 保存 PreviewPublish 调用的失败原因。
	if _, err := f.service.PreviewPublish(ctx, ok); err == nil {
		t.Fatal("目录准备失败必须返回错误")
	}
}

// TestPublishHelpers 覆盖金额与扩展名辅助函数。
func TestPublishHelpers(t *testing.T) {
	if centsText(5) != "0.05" || centsText(123456) != "1234.56" {
		t.Fatal("金额格式化异常")
	}
	if extensionOf("1/a.b.png") != ".png" || extensionOf("noext") != "" {
		t.Fatal("扩展名提取异常")
	}
}
