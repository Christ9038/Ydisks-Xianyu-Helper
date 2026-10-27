package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// TestLoadBatchPublishSpecs 覆盖单规格、多规格含规格图、图片读取失败和快照损坏分支。
func TestLoadBatchPublishSpecs(t *testing.T) {
	// readLocal 返回固定图片内容并记录请求的引用。
	var requested []string
	// readLocal 保存 func 的返回结果。
	readLocal := func(_ string, ref string) ([]byte, string, string, error) {
		requested = append(requested, ref)
		if ref == "missing.png" {
			return nil, "", "", errors.New("图片文件不存在")
		}
		return []byte("img"), "image/png", ref, nil
	}
	// download 不应被本地引用触发。
	download := func(context.Context, string) ([]byte, string, error) { return nil, "", errors.New("unexpected") }

	// 单规格快照全部返回空值。
	specs, skus, images, quantity, err := loadBatchPublishSpecs(context.Background(), "dir", "", readLocal, download)
	if err != nil || specs != nil || skus != nil || images != nil || quantity != 0 {
		t.Fatalf("单规格应为空: %v %v %v %d %v", specs, skus, images, quantity, err)
	}
	// snapshot 是带规格图的两维度快照。
	snapshot := `{"specs":[{"property_name":"颜色","support_image":true,"values":[{"value":"红","image_index":0},{"value":"蓝","image_index":-1}]}],` +
		`"skus":[{"price_cents":100,"quantity":2,"property_list":[{"property_text":"颜色","value_text":"红"}]},{"price_cents":200,"quantity":3,"property_list":[{"property_text":"颜色","value_text":"蓝"}]}],"spec_images":["library/1.png"]}`
	specs, skus, images, quantity, err = loadBatchPublishSpecs(context.Background(), "dir", snapshot, readLocal, download)
	if err != nil || len(specs) != 1 || specs[0].Values[0].ImageIndex != 0 || len(skus) != 2 || skus[1].PropertyList[0].ValueText != "蓝" || len(images) != 1 || quantity != 5 {
		t.Fatalf("多规格转换异常: %+v %+v %d %d %v", specs, skus, len(images), quantity, err)
	}
	if len(requested) != 1 || requested[0] != "library/1.png" {
		t.Fatalf("规格图片读取引用异常: %v", requested)
	}
	// 无规格图片的多规格快照不读取图片。
	noImage := `{"specs":[{"property_name":"尺码","values":[{"value":"S","image_index":-1},{"value":"M","image_index":-1}]}],"skus":[{"price_cents":1,"quantity":1,"property_list":[{"property_text":"尺码","value_text":"S"}]}]}`
	// noImages 保存 loadBatchPublishSpecs 的返回结果，noImageErr 保存其失败原因。
	if _, _, noImages, _, noImageErr := loadBatchPublishSpecs(context.Background(), "dir", noImage, readLocal, download); noImageErr != nil || noImages != nil {
		t.Fatalf("无规格图应跳过图片读取: %v %v", noImages, noImageErr)
	}
	// 图片读取失败向上传递。
	missing := `{"specs":[{"property_name":"颜色","values":[]}],"skus":[],"spec_images":["missing.png"]}`
	// missingErr 保存 loadBatchPublishSpecs 调用的失败原因。
	if _, _, _, _, missingErr := loadBatchPublishSpecs(context.Background(), "dir", missing, readLocal, download); missingErr == nil {
		t.Fatal("规格图读取失败必须报错")
	}
	// 损坏快照报错。
	if _, _, _, _, badErr := loadBatchPublishSpecs(context.Background(), "dir", "{bad", readLocal, download); badErr == nil {
		t.Fatal("损坏快照必须报错")
	}
	// 直接转换覆盖空输入。
	emptySpecs, emptySKUs := mtopSpecsFromApplication(nil, []itemapp.PublishSKU{})
	if len(emptySpecs) != 0 || len(emptySKUs) != 0 {
		t.Fatal("空输入应返回空切片")
	}
}
