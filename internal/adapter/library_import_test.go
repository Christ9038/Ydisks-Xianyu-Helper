package adapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libraryapp "xianyu-go/internal/application/library"
)

// TestLibraryImportStoreLifecycle 覆盖暂存的创建、读取、清单原子写入、删除与过期清理，以及非法标识与缺失任务。
func TestLibraryImportStoreLifecycle(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 保存本次调用的返回值，用于断言：空上传根目录应构造失败。
	if _, err := NewLibraryImportStore(" "); err == nil {
		t.Fatal("空上传根目录应构造失败")
	}
	// root 是测试上传根目录。
	root := t.TempDir()
	// store 是被测暂存存储。
	store, err := NewLibraryImportStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// createdAt 是清单创建时间。
	createdAt := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// err 保存首个任务（带压缩包）的创建错误。
	if err := store.Create(ctx, "import_a", []byte("sheet"), []byte("zipdata")); err != nil {
		t.Fatal(err)
	}
	// err 保存本次调用的返回值，用于断言：重复标识应创建失败。
	if err := store.Create(ctx, "import_a", []byte("sheet"), nil); err == nil {
		t.Fatal("重复标识应创建失败")
	}
	// err 保存本次调用的返回值，用于断言：非法标识应拒绝。
	if err := store.Create(ctx, "../evil", nil, nil); err == nil {
		t.Fatal("非法标识应拒绝")
	}
	// manifest 是待保存的清单。
	manifest := libraryapp.ImportManifest{ImportID: "import_a", UserID: 7, SheetName: "t.csv", HasZip: true, CreatedAt: createdAt,
		Rows: []libraryapp.ImportManifestRow{{RowNo: 2, Status: libraryapp.ImportRowCreated, TemplateID: 5}, {RowNo: 3, Status: libraryapp.ImportRowInvalid, Errors: []string{"价格错误"}}}}
	// err 保存清单首次原子写入的错误。
	if err := store.SaveManifest(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	// loaded、loadErr 保存读回的清单。
	loaded, loadErr := store.LoadManifest(ctx, "import_a")
	if loadErr != nil || loaded.UserID != 7 || !loaded.CreatedAt.Equal(createdAt) || len(loaded.Rows) != 2 || loaded.Rows[0].TemplateID != 5 || loaded.Rows[1].Errors[0] != "价格错误" {
		t.Fatalf("清单读回异常: %+v %v", loaded, loadErr)
	}
	// statErr 保存本次调用的返回值，用于断言：原子写入后不应残留临时文件。
	if _, statErr := os.Stat(filepath.Join(root, libraryImportsDirName, "import_a", libraryImportManifestFile+".tmp")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("原子写入后不应残留临时文件")
	}
	// sheet、sheetErr 保存读回的表格。
	if sheet, sheetErr := store.ReadSheet(ctx, "import_a"); sheetErr != nil || string(sheet) != "sheet" {
		t.Fatalf("表格读回异常: %q %v", sheet, sheetErr)
	}
	// archive、size、zipErr 保存打开的压缩包。
	archive, size, zipErr := store.OpenZip(ctx, "import_a")
	if zipErr != nil || archive == nil || size != int64(len("zipdata")) {
		t.Fatalf("压缩包打开异常: %v %d %v", archive, size, zipErr)
	}
	// buffer 保存压缩包内容。
	buffer := make([]byte, size)
	// readErr 保存本次调用的返回值，用于断言：压缩包内容异常。
	if _, readErr := archive.ReadAt(buffer, 0); readErr != nil && !errors.Is(readErr, io.EOF) || string(buffer) != "zipdata" {
		t.Fatalf("压缩包内容异常: %q %v", buffer, readErr)
	}
	_ = archive.Close()
	// 没有压缩包的任务返回 nil 句柄。
	if err := store.Create(ctx, "import_b", []byte("sheet"), nil); err != nil {
		t.Fatal(err)
	}
	// none、noneErr、noneSize 保存本次调用的返回值，用于断言：无压缩包应返回空句柄。
	if none, noneSize, noneErr := store.OpenZip(ctx, "import_b"); none != nil || noneSize != 0 || noneErr != nil {
		t.Fatalf("无压缩包应返回空句柄: %v %d %v", none, noneSize, noneErr)
	}
	// 缺失任务、缺失清单与非法标识都视为不存在。
	for /* id 是不存在或非法的任务标识。 */ _, id := range []string{"import_none", "../x", ""} {
		if _, err := store.ReadSheet(ctx, id); !errors.Is(err, libraryapp.ErrImportNotFound) {
			t.Fatalf("ReadSheet(%q) 应不存在: %v", id, err)
		}
		// err 保存本次调用的返回值，用于断言：OpenZip(。
		if _, _, err := store.OpenZip(ctx, id); !errors.Is(err, libraryapp.ErrImportNotFound) {
			t.Fatalf("OpenZip(%q) 应不存在: %v", id, err)
		}
		// err 保存本次调用的返回值，用于断言：LoadManifest(。
		if _, err := store.LoadManifest(ctx, id); !errors.Is(err, libraryapp.ErrImportNotFound) {
			t.Fatalf("LoadManifest(%q) 应不存在: %v", id, err)
		}
		// err 保存本次调用的返回值，用于断言：SaveManifest(。
		if err := store.SaveManifest(ctx, libraryapp.ImportManifest{ImportID: id}); !errors.Is(err, libraryapp.ErrImportNotFound) {
			t.Fatalf("SaveManifest(%q) 应不存在: %v", id, err)
		}
	}
	// err 保存本次调用的返回值，用于断言：缺失清单应不存在。
	if _, err := store.LoadManifest(ctx, "import_b"); !errors.Is(err, libraryapp.ErrImportNotFound) {
		t.Fatalf("缺失清单应不存在: %v", err)
	}
	// 缺失表格视为不存在。
	if err := os.Remove(filepath.Join(root, libraryImportsDirName, "import_b", libraryImportSheetFile)); err != nil {
		t.Fatal(err)
	}
	// err 保存本次调用的返回值，用于断言：缺失表格应不存在。
	if _, err := store.ReadSheet(ctx, "import_b"); !errors.Is(err, libraryapp.ErrImportNotFound) {
		t.Fatalf("缺失表格应不存在: %v", err)
	}
	// 损坏清单返回错误而不是不存在。
	if err := os.WriteFile(filepath.Join(root, libraryImportsDirName, "import_b", libraryImportManifestFile), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	// err 保存本次调用的返回值，用于断言：损坏清单应返回错误。
	if _, err := store.LoadManifest(ctx, "import_b"); err == nil || errors.Is(err, libraryapp.ErrImportNotFound) {
		t.Fatalf("损坏清单应返回错误: %v", err)
	}
	// 过期清理：import_a 清单时间早于截止时间被删除；import_b 清单损坏、按目录时间（刚创建）保留；无关文件与非法目录名忽略。
	if err := os.WriteFile(filepath.Join(root, libraryImportsDirName, "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// err 保存名称不合法的干扰目录创建错误，清理时应忽略该目录。
	if err := os.Mkdir(filepath.Join(root, libraryImportsDirName, "bad name"), 0o750); err != nil {
		t.Fatal(err)
	}
	// err 保存按清单时间清理过期任务的错误。
	if err := store.CleanupBefore(ctx, createdAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// statErr 保存本次调用的返回值，用于断言：过期任务应被清理。
	if _, statErr := os.Stat(filepath.Join(root, libraryImportsDirName, "import_a")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("过期任务应被清理")
	}
	// statErr 保存本次调用的返回值，用于断言：未过期任务应保留。
	if _, statErr := os.Stat(filepath.Join(root, libraryImportsDirName, "import_b")); statErr != nil {
		t.Fatal("未过期任务应保留")
	}
	// 截止时间晚于目录修改时间时，清单损坏的任务按目录时间清理。
	if err := store.CleanupBefore(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// statErr 保存本次调用的返回值，用于断言：按目录时间过期的任务应被清理。
	if _, statErr := os.Stat(filepath.Join(root, libraryImportsDirName, "import_b")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("按目录时间过期的任务应被清理")
	}
	// err 保存本次调用的返回值，用于断言：删除不存在的任务应成功。
	if err := store.Remove(ctx, "import_none"); err != nil {
		t.Fatalf("删除不存在的任务应成功: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：非法标识删除应直接忽略。
	if err := store.Remove(ctx, "../x"); err != nil {
		t.Fatalf("非法标识删除应直接忽略: %v", err)
	}
	// err 保存待删除任务的创建错误。
	if err := store.Create(ctx, "import_c", []byte("s"), nil); err != nil {
		t.Fatal(err)
	}
	// err 保存删除已存在任务的错误。
	if err := store.Remove(ctx, "import_c"); err != nil {
		t.Fatal(err)
	}
	// err 保存本次调用的返回值，用于断言：删除后应不存在。
	if _, err := store.ReadSheet(ctx, "import_c"); !errors.Is(err, libraryapp.ErrImportNotFound) {
		t.Fatalf("删除后应不存在: %v", err)
	}
}

// TestLibraryImportStoreRootFailures 覆盖上传根目录不可用时各操作返回错误。
func TestLibraryImportStoreRootFailures(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// file 是被当作上传根目录的普通文件，导致无法创建子目录。
	file := filepath.Join(t.TempDir(), "file")
	// err 保存充当上传根目录的普通文件写入错误。
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// store 是根目录不可用的暂存存储。
	store, err := NewLibraryImportStore(file)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存本次调用的返回值，用于断言：根目录不可用时创建应失败。
	if err := store.Create(ctx, "import_a", nil, nil); err == nil {
		t.Fatal("根目录不可用时创建应失败")
	}
	// err 保存本次调用的返回值，用于断言：根目录不可用时读取应返回系统错误。
	if _, err := store.ReadSheet(ctx, "import_a"); err == nil || errors.Is(err, libraryapp.ErrImportNotFound) {
		t.Fatalf("根目录不可用时读取应返回系统错误: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：根目录不可用时删除应失败。
	if err := store.Remove(ctx, "import_a"); err == nil {
		t.Fatal("根目录不可用时删除应失败")
	}
	// err 保存本次调用的返回值，用于断言：根目录不可用时清理应失败。
	if err := store.CleanupBefore(ctx, time.Now()); err == nil {
		t.Fatal("根目录不可用时清理应失败")
	}
}

// TestLibraryRemoteImageFetcher 覆盖网址校验、状态码、大小上限、连接失败与请求取消。
func TestLibraryRemoteImageFetcher(t *testing.T) {
	// server 是本地图片服务：/ok 返回图片、/404 返回错误、/huge 返回超限内容、/slow 等待请求取消。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("IMG"))
		case "/huge":
			_, _ = w.Write([]byte(strings.Repeat("0", libraryapp.MaxImageBytes+1)))
		case "/slow":
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	// fetcher 使用本地服务客户端，避免依赖本机出站策略与 DNS。
	fetcher := &LibraryRemoteImageFetcher{client: server.Client()}
	// ctx 是测试上下文。
	ctx := context.Background()
	// data、err 保存本次调用的返回值，用于断言：下载成功异常。
	if data, err := fetcher.Fetch(ctx, server.URL+"/ok"); err != nil || string(data) != "IMG" {
		t.Fatalf("下载成功异常: %q %v", data, err)
	}
	// cases 是失败网址及期望错误片段。
	cases := map[string]string{server.URL + "/404": "HTTP 404", server.URL + "/huge": "10 MiB", "ftp://a.com/x.png": "网址无效", "https://": "网址无效", "http://%zz": "网址无效", "http://127.0.0.1:1/x.png": "无法连接"}
	// rawURL、want 是当前网址及期望错误。
	for rawURL, want := range cases {
		// err 保存本次调用的返回值，用于断言：Fetch(。
		if _, err := fetcher.Fetch(ctx, rawURL); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Fetch(%q) err=%v want %q", rawURL, err, want)
		}
	}
	// cancelCtx、cancel 模拟下载中途取消请求。
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	// err 保存本次调用的返回值，用于断言：取消请求应返回上下文错误。
	if _, err := fetcher.Fetch(cancelCtx, server.URL+"/slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("取消请求应返回上下文错误: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：未初始化下载器应失败。
	if _, err := (*LibraryRemoteImageFetcher)(nil).Fetch(ctx, server.URL+"/ok"); err == nil {
		t.Fatal("未初始化下载器应失败")
	}
	if NewLibraryRemoteImageFetcher().client == nil {
		t.Fatal("生产构造应提供受出站策略约束的客户端")
	}
}

// TestLibraryImportStoreIOFailures 用权限与同名目录确定性制造 I/O 故障：清单临时文件写入失败、重命名失败、
// 清单不可读、压缩包无法打开、导入根目录不可列举。
func TestLibraryImportStoreIOFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 用户不受文件权限限制，无法制造权限故障")
	}
	// ctx 是测试上下文。
	ctx := context.Background()
	// root 是测试上传根目录。
	root := t.TempDir()
	// store 是被测暂存存储。
	store, err := NewLibraryImportStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// dir 返回任务目录路径。
	dir := func(id string) string { return filepath.Join(root, libraryImportsDirName, id) }
	// 1. 任务目录只读：临时清单写入失败。
	if err := store.Create(ctx, "import_ro", []byte("s"), nil); err != nil {
		t.Fatal(err)
	}
	// err 保存把任务目录改为只读的权限修改错误。
	if err := os.Chmod(dir("import_ro"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir("import_ro"), 0o750) })
	// err 保存本次调用的返回值，用于断言：只读目录写清单应失败。
	if err := store.SaveManifest(ctx, libraryapp.ImportManifest{ImportID: "import_ro"}); err == nil || !strings.Contains(err.Error(), "保存导入进度失败") {
		t.Fatalf("只读目录写清单应失败: %v", err)
	}
	// 2. manifest.json 是非空目录：重命名失败，读取清单失败且不是“不存在”。
	if err := store.Create(ctx, "import_dir", []byte("s"), []byte("zip")); err != nil {
		t.Fatal(err)
	}
	// err 保存制造同名非空目录（使清单重命名失败）的创建错误。
	if err := os.MkdirAll(filepath.Join(dir("import_dir"), libraryImportManifestFile, "keep"), 0o750); err != nil {
		t.Fatal(err)
	}
	// err 保存本次调用的返回值，用于断言：清单重命名应失败。
	if err := store.SaveManifest(ctx, libraryapp.ImportManifest{ImportID: "import_dir"}); err == nil || !strings.Contains(err.Error(), "保存导入进度失败") {
		t.Fatalf("清单重命名应失败: %v", err)
	}
	// err 保存本次调用的返回值，用于断言：清单为目录时应返回系统错误。
	if _, err := store.LoadManifest(ctx, "import_dir"); err == nil || errors.Is(err, libraryapp.ErrImportNotFound) {
		t.Fatalf("清单为目录时应返回系统错误: %v", err)
	}
	// 3. 压缩包不可读：打开失败。
	if err := os.Chmod(filepath.Join(dir("import_dir"), libraryImportZipFile), 0o000); err != nil {
		t.Fatal(err)
	}
	// err 保存本次调用的返回值，用于断言：压缩包不可读应返回系统错误。
	if _, _, err := store.OpenZip(ctx, "import_dir"); err == nil || errors.Is(err, libraryapp.ErrImportNotFound) {
		t.Fatalf("压缩包不可读应返回系统错误: %v", err)
	}
	// 4. 导入根目录不可列举：过期清理失败。
	if err := os.Chmod(filepath.Join(root, libraryImportsDirName), 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, libraryImportsDirName), 0o750) })
	// err 保存本次调用的返回值，用于断言：导入根目录不可列举时清理应失败。
	if err := store.CleanupBefore(ctx, time.Now()); err == nil {
		t.Fatal("导入根目录不可列举时清理应失败")
	}
}

// TestLibraryRemoteImageFetcherTruncatedBody 覆盖响应体提前断开时的读取失败。
func TestLibraryRemoteImageFetcherTruncatedBody(t *testing.T) {
	// server 声明 100 字节却只写 3 字节，客户端读取时得到意外 EOF。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("IMG"))
	}))
	defer server.Close()
	// fetcher 使用本地服务客户端。
	fetcher := &LibraryRemoteImageFetcher{client: server.Client()}
	// err 保存本次调用的返回值，用于断言：响应体截断应读取失败。
	if _, err := fetcher.Fetch(context.Background(), server.URL+"/x.png"); err == nil || !strings.Contains(err.Error(), "读取图片失败") {
		t.Fatalf("响应体截断应读取失败: %v", err)
	}
}

// TestLibraryRemoteImageFetcherCanceledDuringBody 覆盖读取响应体时请求被取消：返回上下文错误，由导入服务把该行保持为待导入。
func TestLibraryRemoteImageFetcherCanceledDuringBody(t *testing.T) {
	// server 先发送响应头和部分内容，再等待客户端取消。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("IMG"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	// fetcher 使用本地服务客户端。
	fetcher := &LibraryRemoteImageFetcher{client: server.Client()}
	// ctx、cancel 在读取响应体期间超时。
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// err 保存本次调用的返回值，用于断言：读取中取消应返回上下文错误。
	if _, err := fetcher.Fetch(ctx, server.URL+"/x.png"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("读取中取消应返回上下文错误: %v", err)
	}
}
