package library

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
)

// importOwnershipFake 是批量预检的账号与卡密归属替身：只有卡密组 9 属于用户。
type importOwnershipFake struct{}

// CookieOwned 模板导入不校验账号，任何调用都视为不归属。
func (importOwnershipFake) CookieOwned(context.Context, int64, string) (bool, error) {
	return false, nil
}

// CardOwned 只承认卡密组 9。
func (importOwnershipFake) CardOwned(_ context.Context, _ int64, cardID int64) (bool, error) {
	return cardID == 9, nil
}

// importImagePortFake 是批量预检的图片引用替身；模板导入模式不会调用它。
type importImagePortFake struct{}

// ValidateImageReference 总是失败，用于证明模板导入不校验上传目录文件。
func (importImagePortFake) ValidateImageReference(string, string) error {
	return errors.New("不应校验上传目录图片")
}

// fakeImportStaging 是内存版导入暂存，可注入各操作故障。
type fakeImportStaging struct {
	// mu 保护全部字段。
	mu sync.Mutex
	// sheets、zips、manifests 按任务标识保存暂存内容。
	sheets, zips map[string][]byte
	// manifests 保存进度清单。
	manifests map[string]ImportManifest
	// createErr、saveErr、readErr、zipErr、loadErr 模拟对应操作故障。
	createErr, saveErr, readErr, zipErr, loadErr error
	// saveFailAfter 大于 0 时，第 N 次 SaveManifest 起返回 saveErr。
	saveFailAfter int
	// saves 统计 SaveManifest 调用次数。
	saves int
	// cutoffs 记录 CleanupBefore 收到的截止时间。
	cutoffs []time.Time
	// removed 记录被删除的任务。
	removed []string
	// sheetOverride 非空时 ReadSheet 返回它，模拟暂存表格与预检时不同。
	sheetOverride []byte
}

// newFakeImportStaging 创建空暂存。
func newFakeImportStaging() *fakeImportStaging {
	return &fakeImportStaging{sheets: map[string][]byte{}, zips: map[string][]byte{}, manifests: map[string]ImportManifest{}}
}

// Create 记录表格与压缩包。
func (f *fakeImportStaging) Create(_ context.Context, importID string, sheet, zipData []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.sheets[importID] = sheet
	if len(zipData) > 0 {
		f.zips[importID] = zipData
	}
	return nil
}

// ReadSheet 返回暂存表格或覆盖内容。
func (f *fakeImportStaging) ReadSheet(_ context.Context, importID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.sheetOverride != nil {
		return f.sheetOverride, nil
	}
	// sheet、ok 保存暂存表格。
	sheet, ok := f.sheets[importID]
	if !ok {
		return nil, ErrImportNotFound
	}
	return sheet, nil
}

// nopArchive 把内存字节包装为可关闭的随机读取句柄。
type nopArchive struct{ *bytes.Reader }

// Close 无需释放资源。
func (nopArchive) Close() error { return nil }

// OpenZip 返回暂存压缩包句柄。
func (f *fakeImportStaging) OpenZip(_ context.Context, importID string) (ImportArchiveFile, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.zipErr != nil {
		return nil, 0, f.zipErr
	}
	// data、ok 保存压缩包字节。
	data, ok := f.zips[importID]
	if !ok {
		return nil, 0, nil
	}
	return nopArchive{bytes.NewReader(data)}, int64(len(data)), nil
}

// SaveManifest 保存清单副本。
func (f *fakeImportStaging) SaveManifest(_ context.Context, manifest ImportManifest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.saveErr != nil && f.saves >= f.saveFailAfter {
		return f.saveErr
	}
	manifest.Rows = append([]ImportManifestRow(nil), manifest.Rows...)
	f.manifests[manifest.ImportID] = manifest
	return nil
}

// LoadManifest 读取清单副本。
func (f *fakeImportStaging) LoadManifest(_ context.Context, importID string) (ImportManifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return ImportManifest{}, f.loadErr
	}
	// manifest、ok 保存清单。
	manifest, ok := f.manifests[importID]
	if !ok {
		return ImportManifest{}, ErrImportNotFound
	}
	manifest.Rows = append([]ImportManifestRow(nil), manifest.Rows...)
	return manifest, nil
}

// Remove 删除任务全部暂存。
func (f *fakeImportStaging) Remove(_ context.Context, importID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sheets, importID)
	delete(f.zips, importID)
	delete(f.manifests, importID)
	f.removed = append(f.removed, importID)
	return nil
}

// CleanupBefore 记录截止时间并返回故障，证明清理失败不影响预检。
func (f *fakeImportStaging) CleanupBefore(_ context.Context, cutoff time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cutoffs = append(f.cutoffs, cutoff)
	return errors.New("cleanup 失败也不影响预检")
}

// fakeFetcher 是网址图片下载替身：按网址返回预置字节或错误，并可在每次下载后推进时钟。
type fakeFetcher struct {
	// data 是网址到图片字节的映射。
	data map[string][]byte
	// calls 记录下载过的网址。
	calls []string
	// onFetch 在每次下载时调用，可推进时钟或取消请求。
	onFetch func(ctx context.Context) error
}

// Fetch 返回预置结果。
func (f *fakeFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	f.calls = append(f.calls, rawURL)
	if f.onFetch != nil {
		// err 保存钩子返回的错误。
		if err := f.onFetch(ctx); err != nil {
			return nil, err
		}
	}
	// data、ok 保存预置字节。
	data, ok := f.data[rawURL]
	if !ok {
		return nil, errors.New("HTTP 404")
	}
	return data, nil
}

// importFixture 聚合导入服务及其依赖替身。
type importFixture struct {
	// service 是被测导入服务。
	service *TemplateImportService
	// staging 是内存暂存。
	staging *fakeImportStaging
	// fetcher 是下载替身。
	fetcher *fakeFetcher
	// templates 是模板仓储替身。
	templates *fakeTemplateRepository
	// images 是图片仓储替身。
	images *fakeImageRepository
	// blobs 是图片字节替身。
	blobs *fakeBlobStore
	// validator 是模板保存时使用的自动化校验替身。
	validator *fakeAutomationValidator
	// clock 是可推进的当前时间。
	clock *time.Time
}

// newImportFixture 构造使用真实批量预检行解析的导入服务。
func newImportFixture(t *testing.T) importFixture {
	t.Helper()
	// parser 是真实的批量预检服务，验证导入与批量铺货共用同一套行解析。
	parser, err := itemapp.NewBatchPreviewService(importOwnershipFake{}, importImagePortFake{})
	if err != nil {
		t.Fatal(err)
	}
	// imageService、imageRepository、blobs 是图片库服务与内存依赖。
	imageService, imageRepository, blobs := newTestImageService(t)
	// templateRepository、validator 是模板仓储与自动化校验替身。
	templateRepository, validator := newFakeTemplateRepository(), &fakeAutomationValidator{}
	// templateService 保存模板服务。
	templateService, err := NewTemplateService(templateRepository, imageRepository, validator)
	if err != nil {
		t.Fatal(err)
	}
	// clock 是测试时钟。
	clock := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	// staging、fetcher 是暂存与下载替身。
	staging, fetcher := newFakeImportStaging(), &fakeFetcher{data: map[string][]byte{}}
	// sequence 是导入标识序号。
	sequence := 0
	// service 保存被测导入服务。
	service, err := NewTemplateImportService(parser, fetcher, staging, imageService, templateService, func() string {
		sequence++
		return fmt.Sprintf("import_%d", sequence)
	}, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	return importFixture{service: service, staging: staging, fetcher: fetcher, templates: templateRepository, images: imageRepository, blobs: blobs, validator: validator, clock: &clock}
}

// buildZip 构造内存压缩包；entries 是条目名到内容的映射。
func buildZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	// buffer 保存压缩包字节。
	buffer := &bytes.Buffer{}
	// writer 写入压缩包条目。
	writer := zip.NewWriter(buffer)
	// name、content 是当前条目。
	for name, content := range entries {
		// entry、err 保存条目写入器。
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		// writeErr 保存条目内容写入错误。
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	// closeErr 保存压缩包收尾错误。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return buffer.Bytes()
}

// importHeader 是测试表格的表头。
const importHeader = "模板名称,标题,描述,价格,原价,库存,邮费模式,邮费,图片\n"

// TestTemplateImportPreviewAndCommitHappyPath 覆盖预检逐行结果、暂存清单与一次提交完成的导入。
func TestTemplateImportPreviewAndCommitHappyPath(t *testing.T) {
	// f 是导入夹具。
	f := newImportFixture(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	f.templates.templates[99] = Template{ID: 99, UserID: 1, Name: "旧", Title: "已有商品"}
	// sheet 包含：zip 图片行、网址图片行（与已有模板同标题）、价格非法行、与第 2 行同标题的行。
	sheet := importHeader +
		"网盘A,资源A,描述A,9.90,19.90,10,none,,images/a.png;images/a2.png\n" +
		",已有商品,,5,,1,fixed,8,https://img.example.com/b.png?x=1\n" +
		",价格错,,abc,,1,free,,images/a.png\n" +
		",资源A,,1,,1,free,,images/a.png\n"
	f.fetcher.data["https://img.example.com/b.png?x=1"] = []byte("IMGb")
	// zipData 包含两张内容相同的图片，导入时应去重。
	zipData := buildZip(t, map[string]string{"images/a.png": "IMGa", "images/a2.png": "IMGa", "images/": ""})
	// preview、err 保存预检结果。
	preview, err := f.service.PreviewImport(ctx, PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(sheet), Zip: zipData})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ImportID != "import_1" || preview.Total != 4 || preview.Valid != 3 || preview.Invalid != 1 || preview.RemoteImages != 1 {
		t.Fatalf("预检汇总异常: %+v", preview)
	}
	if preview.Rows[0].Name != "网盘A" || preview.Rows[1].Name != "已有商品" || preview.Rows[0].ImageCount != 2 || preview.Rows[1].RemoteImageCount != 1 {
		t.Fatalf("预检行异常: %+v", preview.Rows)
	}
	if !strings.Contains(strings.Join(preview.Rows[1].Warnings, "|"), "已有同标题") || !strings.Contains(strings.Join(preview.Rows[3].Warnings, "|"), "与第 2 行标题相同") {
		t.Fatalf("重名提示异常: %+v", preview.Rows)
	}
	if len(f.staging.cutoffs) != 1 || !f.staging.cutoffs[0].Equal(f.clock.Add(-24*time.Hour)) {
		t.Fatalf("过期清理截止时间异常: %v", f.staging.cutoffs)
	}
	// manifest 是暂存清单，无效行带错误，其余待导入。
	manifest := f.staging.manifests["import_1"]
	if manifest.UserID != 1 || !manifest.HasZip || manifest.Rows[2].Status != ImportRowInvalid || manifest.Rows[0].Status != ImportRowPending || len(manifest.Rows[2].Errors) == 0 {
		t.Fatalf("清单异常: %+v", manifest)
	}
	// progress、commitErr 保存提交结果。
	progress, commitErr := f.service.CommitImport(ctx, 1, "import_1")
	if commitErr != nil {
		t.Fatal(commitErr)
	}
	if !progress.Done || progress.Created != 3 || progress.Invalid != 1 || progress.Pending != 0 || len(progress.CreatedTemplateIDs) != 3 {
		t.Fatalf("提交进度异常: %+v", progress)
	}
	// first 是第一行创建的模板，两张相同内容的图片只保留一张。
	first := f.templates.templates[progress.CreatedTemplateIDs[0]]
	if first.Name != "网盘A" || first.PriceCents != 990 || first.OriginalPriceCents != 1990 || first.PostageMode != "none" || len(first.ImageIDs) != 1 || first.Quantity != 10 {
		t.Fatalf("模板内容异常: %+v", first)
	}
	// second 是网址图片行创建的模板，固定邮费换算为分。
	second := f.templates.templates[progress.CreatedTemplateIDs[1]]
	if second.PostageCents != 800 || len(second.ImageIDs) != 1 || f.images.images[second.ImageIDs[0]].Filename != "b.png" {
		t.Fatalf("网址图片模板异常: %+v", second)
	}
	if len(f.staging.removed) != 1 || f.staging.manifests["import_1"].ImportID != "" {
		t.Fatalf("完成后应删除暂存: removed=%v", f.staging.removed)
	}
	// again、againErr 保存完成后再次提交的结果，任务已不存在。
	if _, againErr := f.service.CommitImport(ctx, 1, "import_1"); !errors.Is(againErr, ErrImportNotFound) {
		t.Fatalf("完成后的任务应不存在: %v", againErr)
	}
}

// TestTemplateImportPreviewRejectsBadInput 覆盖整体拒绝的输入：超限、空表、超行数、损坏或不安全的压缩包。
func TestTemplateImportPreviewRejectsBadInput(t *testing.T) {
	// f 是导入夹具。
	f := newImportFixture(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// many 是 301 行数据的表格。
	many := importHeader + strings.Repeat(",t,,1,,1,free,,a.png\n", MaxImportRows+1)
	// tooManyFiles 是超过 500 个文件的压缩包条目。
	tooManyFiles := map[string]string{}
	// index 是条目序号。
	for index := 0; index <= maxImportArchiveFiles; index++ {
		tooManyFiles[fmt.Sprintf("f%d.png", index)] = "IMG"
	}
	// cases 是非法输入及期望错误片段。
	cases := []struct {
		name  string
		input PreviewImportInput
		want  string
	}{
		{"表格超限", PreviewImportInput{Sheet: make([]byte, MaxImportSheetBytes+1)}, "20 MiB"},
		{"压缩包超限", PreviewImportInput{Sheet: []byte(importHeader), Zip: make([]byte, MaxImportZipBytes+1)}, "200 MiB"},
		{"空表格", PreviewImportInput{SheetName: "t.csv"}, "导入内容为空"},
		{"只有表头", PreviewImportInput{SheetName: "t.csv", Sheet: []byte(importHeader)}, "至少需要表头和一行数据"},
		{"只有空行", PreviewImportInput{SheetName: "t.csv", Sheet: []byte(importHeader + ",,,,,,,,\n")}, "没有有效数据行"},
		{"超过行数", PreviewImportInput{SheetName: "t.csv", Sheet: []byte(many)}, "300"},
		{"损坏压缩包", PreviewImportInput{SheetName: "t.csv", Sheet: []byte(importHeader + ",t,,1,,1,free,,a.png\n"), Zip: []byte("not zip")}, "解析图片 zip 失败"},
		{"路径穿越", PreviewImportInput{SheetName: "t.csv", Sheet: []byte(importHeader + ",t,,1,,1,free,,a.png\n"), Zip: buildZip(t, map[string]string{"../evil.png": "IMG"})}, "图片路径不安全"},
		{"文件过多", PreviewImportInput{SheetName: "t.csv", Sheet: []byte(importHeader + ",t,,1,,1,free,,a.png\n"), Zip: buildZip(t, tooManyFiles)}, "文件数不能超过"},
		{"单图过大", PreviewImportInput{SheetName: "t.csv", Sheet: []byte(importHeader + ",t,,1,,1,free,,a.png\n"), Zip: buildZip(t, map[string]string{"a.png": strings.Repeat("0", MaxImageBytes+1)})}, "不能超过 10 MiB"},
	}
	// item 是当前非法输入。
	for _, item := range cases {
		// err 保存预检错误。
		_, err := f.service.PreviewImport(ctx, item.input)
		// validation 保存校验错误。
		var validation *ValidationError
		if !errors.As(err, &validation) || !strings.Contains(validation.Message, item.want) {
			t.Fatalf("%s: err=%v want=%q", item.name, err, item.want)
		}
	}
	if len(f.staging.sheets) != 0 {
		t.Fatal("整体拒绝时不应建立暂存")
	}
}

// TestTemplateImportPreviewRowChecks 覆盖逐行检查：压缩包缺图、未上传压缩包、网址无效、名称回退截断与超长、全部无效时不暂存。
func TestTemplateImportPreviewRowChecks(t *testing.T) {
	// f 是导入夹具。
	f := newImportFixture(t)
	// longTitle 是 120 个字符的标题，名称回退时应截断为 100 个字符。
	longTitle := strings.Repeat("长", 120)
	// longName 是 101 个字符的模板名称。
	longName := strings.Repeat("名", 101)
	// hugeTitle 是 256 个字符的标题。
	hugeTitle := strings.Repeat("超", 256)
	// sheet 中所有行都不可导入。
	sheet := importHeader +
		",缺图,,1,,1,free,,images/missing.png\n" +
		",网址错,,1,,1,free,,http://\n" +
		longName + ",名称超长,,1,,1,free,,https://img.example.com/a.png\n" +
		"," + hugeTitle + ",,1,,1,free,,https://img.example.com/a.png\n" +
		"," + longTitle + ",,0,,1,free,,https://img.example.com/a.png\n"
	// preview、err 保存带压缩包的预检结果。
	preview, err := f.service.PreviewImport(context.Background(), PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(sheet), Zip: buildZip(t, map[string]string{"images/a.png": "IMG"})})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ImportID != "" || preview.Valid != 0 || preview.Invalid != 5 || len(f.staging.sheets) != 0 {
		t.Fatalf("全部无效时不应暂存: %+v", preview)
	}
	// wants 是各行期望错误片段。
	wants := []string{"图片不在压缩包中: images/missing.png", "图片网址无效", "模板名称不能超过 100", "商品标题不能超过 255", "价格必须大于 0"}
	// index、want 是行下标及期望错误。
	for index, want := range wants {
		if !strings.Contains(strings.Join(preview.Rows[index].Errors, "|"), want) {
			t.Fatalf("第 %d 行错误 %v 缺少 %q", index, preview.Rows[index].Errors, want)
		}
	}
	// got 保存本次调用的返回值，用于断言：名称应截断为 100 个字符。
	if got := []rune(preview.Rows[4].Name); len(got) != 100 {
		t.Fatalf("名称应截断为 100 个字符: %d", len(got))
	}
	// noZip 保存未上传压缩包时的预检结果。
	noZip, noZipErr := f.service.PreviewImport(context.Background(), PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(importHeader + ",t,,1,,1,free,,a.png\n")})
	if noZipErr != nil || !strings.Contains(strings.Join(noZip.Rows[0].Errors, "|"), "未上传图片 zip") {
		t.Fatalf("未上传压缩包提示异常: %+v %v", noZip, noZipErr)
	}
}

// TestTemplateImportPreviewDependencyFailures 覆盖模板列表、暂存创建与清单写入故障，以及清单写入失败后的回收。
func TestTemplateImportPreviewDependencyFailures(t *testing.T) {
	// input 是一行合法输入。
	input := PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(importHeader + ",t,,1,,1,free,,https://img.example.com/a.png\n")}
	// f 是导入夹具。
	f := newImportFixture(t)
	f.staging.createErr = errors.New("disk full")
	// createErr 保存暂存创建故障。
	if _, createErr := f.service.PreviewImport(context.Background(), input); createErr == nil || !strings.Contains(createErr.Error(), "disk full") {
		t.Fatalf("暂存创建故障应返回: %v", createErr)
	}
	f.staging.createErr = nil
	f.staging.saveErr = errors.New("manifest")
	// saveErr 保存清单写入故障。
	if _, saveErr := f.service.PreviewImport(context.Background(), input); saveErr == nil || len(f.staging.removed) != 1 {
		t.Fatalf("清单写入失败应回收暂存: err=%v removed=%v", saveErr, f.staging.removed)
	}
	f.staging.saveErr = nil
	// failing 是使用返回系统错误的解析替身构造的服务。
	failing, failingErr := NewTemplateImportService(failingParser{}, f.fetcher, f.staging, f.service.images, f.service.templates, func() string { return "x" }, nil)
	if failingErr != nil {
		t.Fatal(failingErr)
	}
	// parserErr 保存解析系统错误。
	if _, parserErr := failing.PreviewImport(context.Background(), input); parserErr == nil || !strings.Contains(parserErr.Error(), "parser down") {
		t.Fatalf("解析系统错误应返回: %v", parserErr)
	}
}

// failingParser 是总是返回系统错误的行解析替身。
type failingParser struct{}

// PreviewTemplateRows 返回系统错误。
func (failingParser) PreviewTemplateRows(context.Context, int64, []map[string]any) ([]itemapp.BatchPreviewRow, error) {
	return nil, errors.New("parser down")
}

// TestTemplateImportCommitRowFailures 覆盖逐行失败：下载失败、图片内容无效、模板保存校验失败；失败行不影响其他行。
func TestTemplateImportCommitRowFailures(t *testing.T) {
	// f 是导入夹具。
	f := newImportFixture(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// sheet 包含：下载 404、非图片内容、模板保存校验失败的三行。
	sheet := importHeader +
		",下载失败,,1,,1,free,,https://img.example.com/404.png\n" +
		",不是图片,,1,,1,free,,https://img.example.com/text.png\n" +
		",保存失败,,1,,1,free,,https://img.example.com/ok.png\n"
	f.fetcher.data["https://img.example.com/text.png"] = []byte("hello")
	f.fetcher.data["https://img.example.com/ok.png"] = []byte("IMGok")
	// preview、err 保存预检结果。
	preview, err := f.service.PreviewImport(ctx, PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(sheet)})
	if err != nil || preview.Valid != 3 {
		t.Fatalf("预检异常: %+v %v", preview, err)
	}
	f.validator.problems = []string{"自动化配置无效"}
	// progress、commitErr 保存提交结果。
	progress, commitErr := f.service.CommitImport(ctx, 1, preview.ImportID)
	if commitErr != nil || !progress.Done || progress.Failed != 3 || progress.Created != 0 {
		t.Fatalf("提交结果异常: %+v %v", progress, commitErr)
	}
	// wants 是各行期望失败原因。
	wants := []string{"图片下载失败（https://img.example.com/404.png）：HTTP 404", "图片「text.png」无效", "自动化配置无效"}
	// index、want 是行下标及期望原因。
	for index, want := range wants {
		if !strings.Contains(strings.Join(progress.Rows[index].Errors, "|"), want) {
			t.Fatalf("第 %d 行原因 %v 缺少 %q", index, progress.Rows[index].Errors, want)
		}
	}
}

// TestTemplateImportCommitResumesAcrossBudgetAndCancellation 覆盖分段提交：超过开始新行的预算后停止、下次继续，
// 以及请求取消导致当前行保持待导入。
func TestTemplateImportCommitResumesAcrossBudgetAndCancellation(t *testing.T) {
	// f 是导入夹具。
	f := newImportFixture(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// sheet 是三行网址图片。
	sheet := importHeader + ",一,,1,,1,free,,https://img.example.com/1.png\n,二,,1,,1,free,,https://img.example.com/2.png\n,三,,1,,1,free,,https://img.example.com/3.png\n"
	// index 是网址序号。
	for index := 1; index <= 3; index++ {
		f.fetcher.data[fmt.Sprintf("https://img.example.com/%d.png", index)] = []byte(fmt.Sprintf("IMG%d", index))
	}
	// preview、err 保存预检结果。
	preview, err := f.service.PreviewImport(ctx, PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(sheet)})
	if err != nil {
		t.Fatal(err)
	}
	// 每次下载耗时 30 秒，超过 20 秒预算：每次提交只完成一行。
	f.fetcher.onFetch = func(context.Context) error {
		*f.clock = f.clock.Add(30 * time.Second)
		return nil
	}
	// first、firstErr 保存第一次提交结果。
	first, firstErr := f.service.CommitImport(ctx, 1, preview.ImportID)
	if firstErr != nil || first.Done || first.Created != 1 || first.Pending != 2 {
		t.Fatalf("第一次提交应只完成一行: %+v %v", first, firstErr)
	}
	// cancelCtx、cancel 模拟用户在下载时取消请求。
	cancelCtx, cancel := context.WithCancel(ctx)
	f.fetcher.onFetch = func(context.Context) error {
		cancel()
		return context.Canceled
	}
	// canceled、canceledErr 保存取消时的提交结果：不报错、行保持待导入。
	canceled, canceledErr := f.service.CommitImport(cancelCtx, 1, preview.ImportID)
	if canceledErr != nil || canceled.Created != 1 || canceled.Pending != 2 || canceled.Failed != 0 {
		t.Fatalf("取消时应保持待导入: %+v %v", canceled, canceledErr)
	}
	f.fetcher.onFetch = nil
	// final、finalErr 保存最后一次提交结果。
	final, finalErr := f.service.CommitImport(ctx, 1, preview.ImportID)
	if finalErr != nil || !final.Done || final.Created != 3 {
		t.Fatalf("续传后应全部完成: %+v %v", final, finalErr)
	}
	// alreadyCanceled 是提交前就已取消的上下文，不应开始任何行。
	f2 := newImportFixture(t)
	f2.fetcher.data["https://img.example.com/1.png"] = []byte("IMG1")
	// preview2 保存第二个任务的预检结果。
	preview2, _ := f2.service.PreviewImport(ctx, PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(importHeader + ",一,,1,,1,free,,https://img.example.com/1.png\n")})
	// doneCtx 是已取消的上下文。
	doneCtx, doneCancel := context.WithCancel(ctx)
	doneCancel()
	// progress、progressErr 保存本次调用的返回值，用于断言：已取消的请求不应开始新行。
	if progress, progressErr := f2.service.CommitImport(doneCtx, 1, preview2.ImportID); progressErr != nil || progress.Pending != 1 || len(f2.fetcher.calls) != 0 {
		t.Fatalf("已取消的请求不应开始新行: %+v %v", progress, progressErr)
	}
}

// TestTemplateImportCommitSystemErrorsAndOwnership 覆盖系统错误保留进度、归属核对、并发占用和表格变化。
func TestTemplateImportCommitSystemErrorsAndOwnership(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// sheet 是两行网址图片。
	sheet := importHeader + ",一,,1,,1,free,,https://img.example.com/1.png\n,二,,1,,1,free,,https://img.example.com/2.png\n"
	// prepare 创建夹具并完成预检。
	prepare := func(t *testing.T) (importFixture, string) {
		// f 是导入夹具。
		f := newImportFixture(t)
		f.fetcher.data["https://img.example.com/1.png"] = []byte("IMG1")
		f.fetcher.data["https://img.example.com/2.png"] = []byte("IMG2")
		// preview、err 保存预检结果。
		preview, err := f.service.PreviewImport(ctx, PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(sheet)})
		if err != nil {
			t.Fatal(err)
		}
		return f, preview.ImportID
	}
	// f 是归属与占用测试夹具。
	f, importID := prepare(t)
	// err 保存本次调用的返回值，用于断言：他人任务应不存在。
	if _, err := f.service.CommitImport(ctx, 2, importID); !errors.Is(err, ErrImportNotFound) {
		t.Fatalf("他人任务应不存在: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：空标识应不存在。
	if _, err := f.service.CommitImport(ctx, 1, " "); !errors.Is(err, ErrImportNotFound) {
		t.Fatalf("空标识应不存在: %v", err)
	}
	// release 模拟另一个请求正在处理该任务。
	release, acquireErr := f.service.acquire(importID)
	if acquireErr != nil {
		t.Fatal(acquireErr)
	}
	// err 保存本次调用的返回值，用于断言：并发提交应返回占用。
	if _, err := f.service.CommitImport(ctx, 1, importID); !errors.Is(err, ErrImportBusy) {
		t.Fatalf("并发提交应返回占用: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：处理中的任务不能丢弃。
	if err := f.service.DiscardImport(ctx, 1, importID); !errors.Is(err, ErrImportBusy) {
		t.Fatalf("处理中的任务不能丢弃: %v", err)
	}
	release()
	// failures 是各类系统故障及注入方式。
	failures := []struct {
		name string
		arm  func(importFixture)
	}{
		{"读取表格", func(f importFixture) { f.staging.readErr = errors.New("sys") }},
		{"打开压缩包", func(f importFixture) { f.staging.zipErr = errors.New("sys") }},
		{"图片入库", func(f importFixture) { f.blobs.saveErr = errors.New("sys") }},
		{"模板写入", func(f importFixture) { f.templates.err = errors.New("sys") }},
		{"清单写入", func(f importFixture) { f.staging.saveErr, f.staging.saveFailAfter = errors.New("sys"), 2 }},
	}
	// item 是当前故障。
	for _, item := range failures {
		// failing、failingID 是独立夹具。
		failing, failingID := prepare(t)
		item.arm(failing)
		// err 保存注入故障后的提交错误，应原样返回系统错误。
		if _, err := failing.service.CommitImport(ctx, 1, failingID); err == nil || !strings.Contains(err.Error(), "sys") {
			t.Fatalf("%s 故障应返回系统错误: %v", item.name, err)
		}
		if failing.staging.manifests[failingID].ImportID == "" {
			t.Fatalf("%s 故障后清单应保留", item.name)
		}
	}
	// changed 是暂存表格比预检时少一行的夹具。
	changed, changedID := prepare(t)
	changed.staging.sheetOverride = []byte(importHeader + ",一,,1,,1,free,,https://img.example.com/1.png\n")
	// progress、err 保存表格变化后的提交结果。
	progress, err := changed.service.CommitImport(ctx, 1, changedID)
	if err != nil || progress.Created != 1 || progress.Failed != 1 || !strings.Contains(strings.Join(progress.Rows[1].Errors, "|"), "表格内容已变化") {
		t.Fatalf("表格变化处理异常: %+v %v", progress, err)
	}
}

// TestTemplateImportDiscard 覆盖丢弃：不存在与他人任务都按成功返回且不删除、正常删除暂存、空标识报不存在。
func TestTemplateImportDiscard(t *testing.T) {
	// f 是导入夹具。
	f := newImportFixture(t)
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 保存本次调用的返回值，用于断言：不存在的任务丢弃应成功。
	if err := f.service.DiscardImport(ctx, 1, "import_none"); err != nil {
		t.Fatalf("不存在的任务丢弃应成功: %v", err)
	}
	// preview 保存预检结果。
	preview, _ := f.service.PreviewImport(ctx, PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(importHeader + ",一,,1,,1,free,,https://img.example.com/1.png\n")})
	// 他人任务与不存在同样按成功返回且不删除，避免泄露任务是否存在。
	if err := f.service.DiscardImport(ctx, 2, preview.ImportID); err != nil || f.staging.manifests[preview.ImportID].ImportID == "" {
		t.Fatalf("他人任务不应被删除: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：丢弃应删除暂存。
	if err := f.service.DiscardImport(ctx, 1, preview.ImportID); err != nil || f.staging.manifests[preview.ImportID].ImportID != "" {
		t.Fatalf("丢弃应删除暂存: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：空标识应不存在。
	if err := f.service.DiscardImport(ctx, 1, " "); !errors.Is(err, ErrImportNotFound) {
		t.Fatalf("空标识应不存在: %v", err)
	}
}

// TestTemplateImportHelpers 覆盖构造校验、文件名提取、压缩包路径规范化与网址判断。
func TestTemplateImportHelpers(t *testing.T) {
	// err 保存本次调用的返回值，用于断言：缺少依赖必须失败。
	if _, err := NewTemplateImportService(nil, nil, nil, nil, nil, nil, nil); err == nil {
		t.Fatal("缺少依赖必须失败")
	}
	// f 是导入夹具，用于验证 now 缺省值。
	f := newImportFixture(t)
	// defaulted 是未注入时钟的服务。
	defaulted, err := NewTemplateImportService(f.service.parser, f.fetcher, f.staging, f.service.images, f.service.templates, func() string { return "x" }, nil)
	if err != nil || defaulted.now == nil {
		t.Fatalf("缺省时钟异常: %v", err)
	}
	// names 是引用到展示文件名的期望。
	names := map[string]string{"https://a.com/p/x.png?y=1#z": "x.png", "images\\b.jpg": "b.jpg", "c.png": "c.png", "https://a.com/": "a.com", "": "image"}
	// reference、want 是当前引用及期望文件名。
	for reference, want := range names {
		// got 保存本次调用的返回值，用于断言：imageFilename(。
		if got := imageFilename(reference); got != want {
			t.Fatalf("imageFilename(%q)=%q want %q", reference, got, want)
		}
	}
	// paths 是路径规范化期望，空串表示应拒绝。
	paths := map[string]string{"a/../b.png": "b.png", " images\\c.png ": "images/c.png", "/abs.png": "", "../x.png": "", "": "", "a/../..": ""}
	// raw、want 是当前路径及期望。
	for raw, want := range paths {
		// got、pathErr 保存规范化结果。
		got, pathErr := normalizeArchivePath(raw)
		if (want == "") != (pathErr != nil) || (want != "" && got != want) {
			t.Fatalf("normalizeArchivePath(%q)=%q,%v want %q", raw, got, pathErr, want)
		}
	}
	if !validRemoteImageURL("https://a.com/x.png") || validRemoteImageURL("https://") || validRemoteImageURL("ftp://a.com/x") || validRemoteImageURL("http://%zz") {
		t.Fatal("网址判断异常")
	}
	// nilArchive 是未上传压缩包时的空索引。
	var nilArchive *importArchive
	if nilArchive.has("a.png") {
		t.Fatal("空索引不应包含图片")
	}
	// readErr 保存空索引读取错误。
	if _, readErr := nilArchive.read("a.png"); readErr == nil {
		t.Fatal("空索引读取应失败")
	}
	// badPathErr 保存不安全路径的读取错误。
	if _, badPathErr := nilArchive.read("../a.png"); badPathErr == nil {
		t.Fatal("不安全路径读取应失败")
	}
	if readerOf(nil) != nil || readerOf([]byte("x")) == nil {
		t.Fatal("readerOf 异常")
	}
	// truncated 是超过 10 MiB 的条目实际读取结果，应截断为上限加一字节。
	archive, archiveErr := newImportArchive(bytes.NewReader(buildZip(t, map[string]string{"a.png": "IMG"})), int64(len(buildZip(t, map[string]string{"a.png": "IMG"}))))
	if archiveErr != nil {
		t.Fatal(archiveErr)
	}
	// data、dataErr 保存本次调用的返回值，用于断言：读取压缩包图片异常。
	if data, dataErr := archive.read("a.png"); dataErr != nil || string(data) != "IMG" {
		t.Fatalf("读取压缩包图片异常: %q %v", data, dataErr)
	}
}

// TestTemplateImportRemainingBranches 覆盖其余确定性分支：模板列表故障、丢弃时清单读取故障、暂存表格重新解析失败、
// 重新解析后行不再合法、缺行时清单写入故障、非一口价邮费忽略「邮费」列，以及金额换算失败。
func TestTemplateImportRemainingBranches(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// input 是两行合法输入，第二行邮费模式为包邮但「邮费」列填了非法值。
	input := PreviewImportInput{UserID: 1, SheetName: "t.csv", Sheet: []byte(importHeader + ",一,,1,,1,free,,https://img.example.com/1.png\n,二,,1,,1,free,abc,https://img.example.com/1.png\n")}
	// f 是导入夹具。
	f := newImportFixture(t)
	f.fetcher.data["https://img.example.com/1.png"] = []byte("IMG1")
	f.templates.listErr = errors.New("list down")
	// listErr 保存模板列表故障。
	if _, listErr := f.service.PreviewImport(ctx, input); listErr == nil || !strings.Contains(listErr.Error(), "list down") {
		t.Fatalf("模板列表故障应返回: %v", listErr)
	}
	f.templates.listErr = nil
	// preview、err 保存预检结果。
	preview, err := f.service.PreviewImport(ctx, input)
	if err != nil || preview.Valid != 2 {
		t.Fatalf("预检异常: %+v %v", preview, err)
	}
	f.staging.loadErr = errors.New("load down")
	// discardErr 保存清单读取系统错误。
	if discardErr := f.service.DiscardImport(ctx, 1, preview.ImportID); discardErr == nil || !strings.Contains(discardErr.Error(), "load down") {
		t.Fatalf("清单读取故障应返回: %v", discardErr)
	}
	f.staging.loadErr = nil
	f.staging.sheetOverride = []byte("")
	// parseErr 保存暂存表格无法解析时的系统错误。
	if _, parseErr := f.service.CommitImport(ctx, 1, preview.ImportID); parseErr == nil {
		t.Fatal("暂存表格无法解析应返回错误")
	}
	// 第一行价格被改为非法，重新解析后不再合法；第二行忽略非法「邮费」列正常导入。
	f.staging.sheetOverride = []byte(importHeader + ",一,,abc,,1,free,,https://img.example.com/1.png\n,二,,1,,1,free,abc,https://img.example.com/1.png\n")
	// progress、commitErr 保存提交结果。
	progress, commitErr := f.service.CommitImport(ctx, 1, preview.ImportID)
	if commitErr != nil || progress.Failed != 1 || progress.Created != 1 || !strings.Contains(strings.Join(progress.Rows[0].Errors, "|"), "价格必须大于 0") {
		t.Fatalf("重新校验结果异常: %+v %v", progress, commitErr)
	}
	// missing 是暂存表格缺行且清单写入失败的夹具。
	missing := newImportFixture(t)
	missing.fetcher.data["https://img.example.com/1.png"] = []byte("IMG1")
	// missingPreview 保存预检结果。
	missingPreview, _ := missing.service.PreviewImport(ctx, input)
	missing.staging.sheetOverride = []byte(importHeader + ",一,,1,,1,free,,https://img.example.com/1.png\n")
	missing.staging.saveErr, missing.staging.saveFailAfter = errors.New("save down"), 3
	// missingErr 保存缺行后清单写入故障。
	if _, missingErr := missing.service.CommitImport(ctx, 1, missingPreview.ImportID); missingErr == nil || !strings.Contains(missingErr.Error(), "save down") {
		t.Fatalf("缺行时清单写入故障应返回: %v", missingErr)
	}
	// rowResult 保存行内金额无法换算时的导入结果：该行失败而非系统错误。
	rowResult, interrupted, rowErr := f.service.importRow(ctx, 1, itemapp.BatchPreviewRow{RowNo: 9, Title: "t", Price: "abc", Images: []string{"https://img.example.com/1.png"}}, nil)
	if rowErr != nil || interrupted || rowResult.Status != ImportRowFailed || !strings.Contains(strings.Join(rowResult.Errors, "|"), "金额格式错误") {
		t.Fatalf("金额换算失败应记为行失败: %+v %v %v", rowResult, interrupted, rowErr)
	}
	// buildErr 保存金额无法换算时的错误。
	if _, buildErr := templateFromRow(1, itemapp.BatchPreviewRow{Price: "abc"}, nil); buildErr == nil {
		t.Fatal("金额无法换算应报错")
	}
	// fixed 保存一口价邮费的换算结果。
	if fixed, fixedErr := templateFromRow(1, itemapp.BatchPreviewRow{Price: "1", PostageMode: "fixed", Postage: "8.5"}, nil); fixedErr != nil || fixed.PostageCents != 850 {
		t.Fatalf("一口价邮费换算异常: %+v %v", fixed, fixedErr)
	}
}

// TestImportArchiveDeclaredAndCorruptEntries 覆盖压缩包声明解压总量超限、未知压缩算法与数据损坏的读取失败。
func TestImportArchiveDeclaredAndCorruptEntries(t *testing.T) {
	// rawZip 用 CreateRaw 写入声明大小与真实数据不一致的条目，method 为压缩算法。
	rawZip := func(t *testing.T, count int, declared uint64, method uint16, payload []byte) []byte {
		t.Helper()
		// buffer 保存压缩包字节。
		buffer := &bytes.Buffer{}
		// writer 写入原始条目。
		writer := zip.NewWriter(buffer)
		// index 是条目序号。
		for index := 0; index < count; index++ {
			// entry、err 保存原始条目写入器。
			entry, err := writer.CreateRaw(&zip.FileHeader{Name: fmt.Sprintf("f%d.png", index), Method: method, CompressedSize64: uint64(len(payload)), UncompressedSize64: declared})
			if err != nil {
				t.Fatal(err)
			}
			// writeErr 保存条目数据写入错误。
			if _, writeErr := entry.Write(payload); writeErr != nil {
				t.Fatal(writeErr)
			}
		}
		// closeErr 保存压缩包收尾错误。
		if closeErr := writer.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		return buffer.Bytes()
	}
	// huge 声明 51 个 10 MiB 文件，解压总量超过 500 MiB。
	huge := rawZip(t, 51, MaxImageBytes, zip.Store, []byte("x"))
	// hugeErr 保存解压总量超限错误。
	if _, hugeErr := newImportArchive(bytes.NewReader(huge), int64(len(huge))); hugeErr == nil || !strings.Contains(hugeErr.Error(), "500 MiB") {
		t.Fatalf("解压总量超限应拒绝: %v", hugeErr)
	}
	// unknown 使用未注册的压缩算法，打开条目失败。
	unknown := rawZip(t, 1, 3, 99, []byte("abc"))
	// unknownArchive 保存未知算法压缩包的索引。
	unknownArchive, err := newImportArchive(bytes.NewReader(unknown), int64(len(unknown)))
	if err != nil {
		t.Fatal(err)
	}
	// openErr 保存打开条目的错误。
	if _, openErr := unknownArchive.read("f0.png"); openErr == nil || !strings.Contains(openErr.Error(), "读取压缩包图片失败") {
		t.Fatalf("未知压缩算法应读取失败: %v", openErr)
	}
	// corrupt 声明为 Deflate 但数据不是合法压缩流，读取失败。
	corrupt := rawZip(t, 1, 3, zip.Deflate, []byte{0xff, 0xff, 0xff})
	// corruptArchive 保存损坏压缩包的索引。
	corruptArchive, err := newImportArchive(bytes.NewReader(corrupt), int64(len(corrupt)))
	if err != nil {
		t.Fatal(err)
	}
	// readErr 保存读取损坏条目的错误。
	if _, readErr := corruptArchive.read("f0.png"); readErr == nil || !strings.Contains(readErr.Error(), "读取压缩包图片失败") {
		t.Fatalf("损坏数据应读取失败: %v", readErr)
	}
	// missingErr 保存读取不存在条目的错误。
	if _, missingErr := corruptArchive.read("none.png"); missingErr == nil {
		t.Fatal("不存在的条目应读取失败")
	}
}
