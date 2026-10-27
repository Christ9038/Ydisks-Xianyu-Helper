package library

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	itemapp "xianyu-go/internal/application/items"
)

// fakeImageRepository 是内存版图片元数据仓储。
type fakeImageRepository struct {
	// mu 保护全部字段。
	mu sync.Mutex
	// images 按主键保存图片。
	images map[int64]Image
	// nextID 是下一个自增主键。
	nextID int64
	// refs 是 TemplateImageRefs 的返回值。
	refs []TemplateImageRef
	// err 非空时所有方法返回该错误，模拟仓储故障。
	err error
	// insertErr 非空时 Insert 返回该错误。
	insertErr error
	// raceExisting 非空时 Insert 模拟并发：返回该已有记录和 created=false。
	raceExisting *Image
}

// newFakeImageRepository 创建空仓储。
func newFakeImageRepository() *fakeImageRepository {
	return &fakeImageRepository{images: map[int64]Image{}, nextID: 1}
}

// List 返回按主键倒序的图片。
func (f *fakeImageRepository) List(context.Context, int64) ([]Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	// out 保存全部图片。
	var out []Image
	// image 是遍历 f.images 时的当前元素。
	for _, image := range f.images {
		out = append(out, image)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Get 读取单张图片并核对用户。
func (f *fakeImageRepository) Get(_ context.Context, userID, id int64) (Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return Image{}, f.err
	}
	// image、ok 保存查询结果。
	image, ok := f.images[id]
	if !ok || image.UserID != userID {
		return Image{}, ErrImageNotFound
	}
	return image, nil
}

// GetMany 读取属于用户的图片。
func (f *fakeImageRepository) GetMany(_ context.Context, userID int64, ids []int64) ([]Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	// out 保存命中的图片。
	var out []Image
	// id 是遍历 ids 时的当前元素。
	for _, id := range ids {
		// image、ok 是查找到的图片及其是否存在。
		if image, ok := f.images[id]; ok && image.UserID == userID {
			out = append(out, image)
		}
	}
	return out, nil
}

// FindBySHA256 按摘要查找。
func (f *fakeImageRepository) FindBySHA256(_ context.Context, userID int64, sha string) (Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return Image{}, f.err
	}
	// image 是遍历 f.images 时的当前元素。
	for _, image := range f.images {
		if image.UserID == userID && image.SHA256 == sha {
			return image, nil
		}
	}
	return Image{}, ErrImageNotFound
}

// Insert 写入图片。
func (f *fakeImageRepository) Insert(_ context.Context, image Image) (Image, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return Image{}, false, f.insertErr
	}
	if f.raceExisting != nil {
		return *f.raceExisting, false, nil
	}
	image.ID = f.nextID
	f.nextID++
	f.images[image.ID] = image
	return image, true, nil
}

// Delete 删除图片。
func (f *fakeImageRepository) Delete(_ context.Context, userID, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// image、ok 是查找到的图片及其是否存在。
	if image, ok := f.images[id]; !ok || image.UserID != userID {
		return ErrImageNotFound
	}
	delete(f.images, id)
	return nil
}

// TemplateImageRefs 返回预置的模板引用。
func (f *fakeImageRepository) TemplateImageRefs(context.Context, int64) ([]TemplateImageRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.refs, nil
}

// fakeBlobStore 是内存版图片字节存储。
type fakeBlobStore struct {
	// mu 保护 files。
	mu sync.Mutex
	// files 按存储键保存字节。
	files map[string][]byte
	// saveErr、readErr 模拟存储故障。
	saveErr, readErr error
}

// newFakeBlobStore 创建空存储。
func newFakeBlobStore() *fakeBlobStore { return &fakeBlobStore{files: map[string][]byte{}} }

// Save 写入字节。
func (f *fakeBlobStore) Save(_ context.Context, key string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.files[key] = data
	return nil
}

// Read 读取字节。
func (f *fakeBlobStore) Read(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	// data、ok 是快照文件内容及其是否存在。
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("文件不存在")
	}
	return data, nil
}

// Remove 删除字节。
func (f *fakeBlobStore) Remove(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, key)
	return nil
}

// fakeInspector 把以 "IMG" 开头的字节视为 png 图片。
type fakeInspector struct{}

// Inspect 实现内容嗅探。
func (fakeInspector) Inspect(data []byte) (string, string, error) {
	if strings.HasPrefix(string(data), "IMG") {
		return "image/png", "png", nil
	}
	return "", "", errors.New("不是有效图片")
}

// fakeTemplateRepository 是内存版模板仓储。
type fakeTemplateRepository struct {
	// templates 按主键保存模板。
	templates map[int64]Template
	// nextID 是下一个自增主键。
	nextID int64
	// err 非空时写方法返回该错误。
	err error
}

// newFakeTemplateRepository 创建空仓储。
func newFakeTemplateRepository() *fakeTemplateRepository {
	return &fakeTemplateRepository{templates: map[int64]Template{}, nextID: 1}
}

// List 返回用户模板。
func (f *fakeTemplateRepository) List(_ context.Context, userID int64) ([]Template, error) {
	// out 保存当前用户的模板。
	var out []Template
	// template 是遍历 f.templates 时的当前元素。
	for _, template := range f.templates {
		if template.UserID == userID {
			out = append(out, template)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get 读取模板并核对用户。
func (f *fakeTemplateRepository) Get(_ context.Context, userID, id int64) (Template, error) {
	// template、ok 是查找到的模板及其是否存在。
	template, ok := f.templates[id]
	if !ok || template.UserID != userID {
		return Template{}, ErrTemplateNotFound
	}
	return template, nil
}

// Create 写入模板。
func (f *fakeTemplateRepository) Create(_ context.Context, template Template) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	template.ID = f.nextID
	f.nextID++
	f.templates[template.ID] = template
	return template.ID, nil
}

// Update 覆盖模板。
func (f *fakeTemplateRepository) Update(_ context.Context, template Template) error {
	if f.err != nil {
		return f.err
	}
	f.templates[template.ID] = template
	return nil
}

// Delete 删除模板。
func (f *fakeTemplateRepository) Delete(_ context.Context, userID, id int64) error {
	// template、ok 是查找到的模板及其是否存在。
	if template, ok := f.templates[id]; !ok || template.UserID != userID {
		return ErrTemplateNotFound
	}
	delete(f.templates, id)
	return nil
}

// fakeAutomationValidator 返回预置的校验提示。
type fakeAutomationValidator struct {
	// problems 是每次校验返回的提示。
	problems []string
	// gotSpecs 记录最近一次收到的规格快照。
	gotSpecs *itemapp.BatchSpecs
}

// ValidateAutomationConfig 记录规格并返回预置提示。
func (f *fakeAutomationValidator) ValidateAutomationConfig(_ context.Context, _ int64, _ itemapp.BatchPreviewAutomation, specs *itemapp.BatchSpecs) []string {
	f.gotSpecs = specs
	return f.problems
}
