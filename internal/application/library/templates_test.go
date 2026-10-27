package library

import (
	"context"
	"errors"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// templateFixture 聚合模板服务测试所需的内存依赖。
type templateFixture struct {
	// service 是待测模板服务。
	service *TemplateService
	// templates 是内存模板仓储。
	templates *fakeTemplateRepository
	// images 是内存图片仓储，已预置用户 1 的图片 1、2、3。
	images *fakeImageRepository
	// validator 是可配置返回值的自动化校验替身。
	validator *fakeAutomationValidator
}

// newTemplateFixture 构造夹具。
func newTemplateFixture(t *testing.T) templateFixture {
	t.Helper()
	// images 预置三张属于用户 1 的图片和一张属于用户 2 的图片。
	images := newFakeImageRepository()
	// id 保存 int64 的返回结果。
	for id := int64(1); id <= 3; id++ {
		images.images[id] = Image{ID: id, UserID: 1, StorageKey: "1/x.png"}
	}
	images.images[4] = Image{ID: 4, UserID: 2, StorageKey: "2/x.png"}
	// templates、validator 是模板仓储与自动化校验替身。
	templates, validator := newFakeTemplateRepository(), &fakeAutomationValidator{}
	// service 保存 NewTemplateService 的返回结果，err 保存其失败原因。
	service, err := NewTemplateService(templates, images, validator)
	if err != nil {
		t.Fatal(err)
	}
	return templateFixture{service: service, templates: templates, images: images, validator: validator}
}

// validTemplate 返回合法的单规格模板输入。
func validTemplate() Template {
	return Template{UserID: 1, Name: " 模板 ", Title: " 标题 ", Description: "描述", PriceCents: 1000, Quantity: 5, PostageMode: "free", PostageCents: 99, ImageIDs: []int64{1, 2}}
}

// validSpecTemplate 返回合法的多规格模板输入，规格图使用图片 3。
func validSpecTemplate() Template {
	// template 保存 validTemplate 的返回结果。
	template := validTemplate()
	template.PriceCents, template.Quantity = 0, 0
	// property 简化 SKU 规格对构造。
	property := func(color string) []itemapp.PublishSKUProperty {
		return []itemapp.PublishSKUProperty{{PropertyText: "颜色", ValueText: color}}
	}
	template.Specs = &TemplateSpecs{
		Specs:        []itemapp.PublishSpec{{PropertyName: "颜色", SupportImage: true, Values: []itemapp.PublishSpecValue{{Value: "红", ImageIndex: 0}, {Value: "蓝", ImageIndex: -1}}}},
		SKUs:         []itemapp.PublishSKU{{PriceCents: 1500, Quantity: 2, PropertyList: property("红")}, {PriceCents: 900, Quantity: 3, PropertyList: property("蓝")}},
		SpecImageIDs: []int64{3},
	}
	return template
}

// TestNewTemplateServiceRejectsNilPorts 验证必需端口缺失时构造失败。
func TestNewTemplateServiceRejectsNilPorts(t *testing.T) {
	// err 保存 NewTemplateService 调用的失败原因。
	if _, err := NewTemplateService(nil, newFakeImageRepository(), &fakeAutomationValidator{}); err == nil {
		t.Fatal("缺少仓储必须失败")
	}
}

// TestTemplateServiceCreateUpdateDelete 覆盖单规格与多规格的创建、更新、读取和删除。
func TestTemplateServiceCreateUpdateDelete(t *testing.T) {
	// f 保存 newTemplateFixture 的返回结果。
	f := newTemplateFixture(t)
	// ctx 保存 Background 的返回结果。
	ctx := context.Background()
	// created 保存 CreateTemplate 的返回结果，err 保存其失败原因。
	created, err := f.service.CreateTemplate(ctx, validTemplate())
	if err != nil || created.Name != "模板" || created.Title != "标题" || created.PostageCents != 0 {
		t.Fatalf("单规格创建异常: %+v %v", created, err)
	}
	// spec 保存 CreateTemplate 的返回结果，err 保存其失败原因。
	spec, err := f.service.CreateTemplate(ctx, validSpecTemplate())
	if err != nil || spec.PriceCents != 900 || spec.Quantity != 5 || spec.Specs == nil {
		t.Fatalf("多规格应推导最低价和总库存: %+v %v", spec, err)
	}
	// ids 保存 ReferencedImageIDs 的返回结果。
	if ids := spec.ReferencedImageIDs(); len(ids) != 3 || ids[2] != 3 {
		t.Fatalf("引用图片应含主图和规格图: %v", ids)
	}
	if f.validator.gotSpecs == nil || len(f.validator.gotSpecs.SpecImages) != 1 {
		t.Fatalf("自动化校验应收到规格快照: %+v", f.validator.gotSpecs)
	}
	// 更新为单规格时丢弃规格配置并要求价格库存。
	update := spec
	update.Specs, update.PriceCents, update.Quantity = nil, 500, 2
	// updated 保存 UpdateTemplate 的返回结果，err 保存其失败原因。
	updated, err := f.service.UpdateTemplate(ctx, update)
	if err != nil || updated.Specs != nil || updated.PriceCents != 500 {
		t.Fatalf("更新异常: %+v %v", updated, err)
	}
	// 他人更新、读取、删除均为不存在。
	foreign := update
	foreign.UserID = 2
	// updateErr 保存 UpdateTemplate 调用的失败原因。
	if _, updateErr := f.service.UpdateTemplate(ctx, foreign); !errors.Is(updateErr, ErrTemplateNotFound) {
		t.Fatalf("他人更新应不存在: %v", updateErr)
	}
	// getErr 保存 GetTemplate 调用的失败原因。
	if _, getErr := f.service.GetTemplate(ctx, 2, created.ID); !errors.Is(getErr, ErrTemplateNotFound) {
		t.Fatalf("他人读取应不存在: %v", getErr)
	}
	// list 保存 ListTemplates 的返回结果，listErr 保存其失败原因。
	if list, listErr := f.service.ListTemplates(ctx, 1); listErr != nil || len(list) != 2 {
		t.Fatalf("list=%d err=%v", len(list), listErr)
	}
	// deleteErr 保存 DeleteTemplate 调用的失败原因。
	if deleteErr := f.service.DeleteTemplate(ctx, 1, created.ID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// deleteErr 保存 DeleteTemplate 调用的失败原因。
	if deleteErr := f.service.DeleteTemplate(ctx, 1, created.ID); !errors.Is(deleteErr, ErrTemplateNotFound) {
		t.Fatalf("重复删除应不存在: %v", deleteErr)
	}
	// 仓储写入失败向上传递。
	f.templates.err = errors.New("db down")
	// createErr 保存 CreateTemplate 调用的失败原因。
	if _, createErr := f.service.CreateTemplate(ctx, validTemplate()); createErr == nil {
		t.Fatal("创建写入失败必须返回错误")
	}
	// updateErr 保存 UpdateTemplate 调用的失败原因。
	if _, updateErr := f.service.UpdateTemplate(ctx, updated); updateErr == nil {
		t.Fatal("更新写入失败必须返回错误")
	}
}

// TestTemplateServiceValidation 覆盖保存时的全部校验分支。
func TestTemplateServiceValidation(t *testing.T) {
	// f 保存 newTemplateFixture 的返回结果。
	f := newTemplateFixture(t)
	// cases 保存对合法输入做单点破坏的变体。
	cases := []struct {
		// name 是用例名。
		name string
		// mutate 对输入做单点破坏。
		mutate func(*Template)
		// want 是期望出现在错误中的片段。
		want string
	}{
		{"名称为空", func(t *Template) { t.Name = " " }, "模板名称"},
		{"名称过长", func(t *Template) { t.Name = strings.Repeat("长", 101) }, "模板名称"},
		{"标题为空", func(t *Template) { t.Title = "" }, "商品标题"},
		{"邮费模式非法", func(t *Template) { t.PostageMode = "buyer" }, "邮费模式"},
		{"固定邮费为负", func(t *Template) { t.PostageMode, t.PostageCents = "fixed", -1 }, "固定邮费"},
		{"原价为负", func(t *Template) { t.OriginalPriceCents = -1 }, "原价"},
		{"没有图片", func(t *Template) { t.ImageIDs = nil }, "至少选择"},
		{"图片过多", func(t *Template) { t.ImageIDs = []int64{1, 2, 3, 5, 6, 7, 8, 9, 10, 11} }, "最多"},
		{"图片重复", func(t *Template) { t.ImageIDs = []int64{1, 1} }, "不能重复"},
		{"他人图片", func(t *Template) { t.ImageIDs = []int64{4} }, "不存在的图片"},
		{"价格为零", func(t *Template) { t.PriceCents = 0 }, "价格"},
		{"库存为零", func(t *Template) { t.Quantity = 0 }, "库存"},
		{"规格非法", func(t *Template) { *t = validSpecTemplate(); t.Specs.SKUs = t.Specs.SKUs[:1] }, "SKU"},
		{"规格图索引越界", func(t *Template) { *t = validSpecTemplate(); t.Specs.Specs[0].Values[0].ImageIndex = 3 }, "图片索引"},
		{"规格图不存在", func(t *Template) { *t = validSpecTemplate(); t.Specs.SpecImageIDs = []int64{9} }, "不存在的图片"},
		{"自动化校验失败", func(t *Template) {}, "卡密组不存在"},
	}
	// tc 是当前破坏用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.validator.problems = nil
			if tc.want == "卡密组不存在" {
				f.validator.problems = []string{"付款发货第1项卡密组不存在", "另一个问题"}
			}
			// input 是被破坏的合法模板。
			input := validTemplate()
			tc.mutate(&input)
			// err 保存 CreateTemplate 调用的失败原因。
			_, err := f.service.CreateTemplate(context.Background(), input)
			// validation 保存校验错误的类型化视图。
			var validation *ValidationError
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want contains %q", err, tc.want)
			}
		})
	}
	// 仓储读取图片失败时属于系统错误而非校验错误。
	f.validator.problems = nil
	f.images.err = errors.New("db down")
	// err 保存 CreateTemplate 调用的失败原因。
	if _, err := f.service.CreateTemplate(context.Background(), validTemplate()); err == nil || errors.As(err, new(*ValidationError)) {
		t.Fatalf("图片仓储故障应为系统错误: %v", err)
	}
	if (&ValidationError{Message: "x"}).Error() != "x" {
		t.Fatal("ValidationError 文本异常")
	}
}
