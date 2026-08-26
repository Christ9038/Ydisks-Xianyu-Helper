package items

import (
	"context"
	"strings"
	"testing"
)

// TestBatchPreviewValidateAutomationCoversAllRuleBranches 覆盖付款发货、评价赠品和求评价规则的启用与参数校验。
func TestBatchPreviewValidateAutomationCoversAllRuleBranches(t *testing.T) {
	// service 保存允许卡券组 9 的预检服务。
	service, err := NewBatchPreviewService(batchPreviewOwnershipFake{cardOwned: 9}, batchPreviewImageFake{})
	if err != nil {
		t.Fatalf("构造预检服务失败: %v", err)
	}
	// invalidConfig 保存同时包含解析、归属、数量、延迟和求评价错误的自动化配置。
	invalidConfig := BatchPreviewAutomation{
		PaidDelivery:  BatchPreviewCardAutomation{Enabled: true, ParseError: "动作解析失败"},
		ReviewGift:    BatchPreviewCardAutomation{Enabled: true, Actions: []BatchPreviewCardAction{{CardID: 8, DeliveryCount: 0, DelaySeconds: 3601}}},
		ReviewRequest: BatchPreviewReviewRequest{Enabled: true, AfterShippedHours: 0, Message: " ", MaxAttempts: 0},
	}
	// invalidErrors 保存无效自动化配置的提示集合。
	invalidErrors := service.validateAutomation(context.Background(), 7, invalidConfig)
	// invalidMessage 保存拼接后的校验提示。
	invalidMessage := strings.Join(invalidErrors, "|")
	// expected 表示当前必须出现的自动化校验提示。
	for _, expected := range []string{"付款发货动作解析失败", "评价赠品第1项卡密组不存在", "评价赠品第1项每件份数必须大于0", "评价赠品第1项延迟秒必须在 0 到 3600 之间", "求评价等待小时必须大于 0", "求评价文案不能为空", "求评价最多次数必须大于 0"} {
		if !strings.Contains(invalidMessage, expected) {
			t.Errorf("自动化校验提示缺少 %q: %s", expected, invalidMessage)
		}
	}

	// emptyActionErrors 保存启用但没有动作的卡券规则错误。
	emptyActionErrors := service.validateAutomation(context.Background(), 7, BatchPreviewAutomation{PaidDelivery: BatchPreviewCardAutomation{Enabled: true}})
	if len(emptyActionErrors) != 1 || !strings.Contains(emptyActionErrors[0], "至少配置一条") {
		t.Fatalf("空卡券动作校验异常: %v", emptyActionErrors)
	}

	// validConfig 保存所有启用规则均合法的配置。
	validConfig := BatchPreviewAutomation{
		PaidDelivery:  BatchPreviewCardAutomation{Enabled: true, Actions: []BatchPreviewCardAction{{CardID: 9, DeliveryCount: 1, DelaySeconds: 0}}},
		ReviewGift:    BatchPreviewCardAutomation{Enabled: false},
		ReviewRequest: BatchPreviewReviewRequest{Enabled: true, AfterShippedHours: 24, Message: "请评价", MaxAttempts: 2},
	}
	// validErrors 保存合法自动化配置的校验结果。
	if validErrors := service.validateAutomation(context.Background(), 7, validConfig); len(validErrors) != 0 {
		t.Fatalf("合法自动化配置不应报错: %v", validErrors)
	}
}

// TestBatchPreviewValidateRowCoversPricePostageAndImageLimits 覆盖原价、固定邮费和图片数量边界校验。
func TestBatchPreviewValidateRowCoversPricePostageAndImageLimits(t *testing.T) {
	// service 保存允许账号且会拒绝指定坏图片的预检服务。
	service, err := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "account"}, batchPreviewImageFake{invalid: "bad.png"})
	if err != nil {
		t.Fatalf("构造预检服务失败: %v", err)
	}
	// images 保存超过平台上限的图片引用集合。
	images := []string{"bad.png", "2.png", "3.png", "4.png", "5.png", "6.png", "7.png", "8.png", "9.png", "10.png"}
	// row 保存含有原价、固定邮费和超量图片的预检行。
	row := BatchPreviewRow{CookieID: "account", Title: "商品", Price: "1.00", OriginalPrice: "bad", Quantity: 1, PostageMode: "fixed", Postage: "bad", Images: images}
	// input 保存图片目录和用户身份校验参数。
	input := BatchPreviewInput{UserID: 7, UploadDir: "/tmp/uploads"}
	service.validateRow(context.Background(), input, &row)
	// message 保存当前行全部业务提示。
	message := strings.Join(row.Errors, "|")
	// expected 表示当前必须出现的行校验提示。
	for _, expected := range []string{"原价格式错误", "固定邮费格式错误", "商品图片最多 9 张", "图片文件不存在"} {
		if !strings.Contains(message, expected) {
			t.Errorf("行校验提示缺少 %q: %s", expected, message)
		}
	}
}

// TestBatchPreviewValidateRowCoversMissingRequiredFields 覆盖账号、标题、售价、库存、邮费和图片缺失校验。
func TestBatchPreviewValidateRowCoversMissingRequiredFields(t *testing.T) {
	// service 保存不需要访问外部平台的预检服务。
	service, err := NewBatchPreviewService(batchPreviewOwnershipFake{}, batchPreviewImageFake{})
	if err != nil {
		t.Fatalf("构造预检服务失败: %v", err)
	}
	// row 保存同时缺少多个商品必填字段的预检行。
	row := BatchPreviewRow{Price: "bad", Quantity: 0, PostageMode: "unknown"}
	// input 保存当前用户身份和上传目录校验参数。
	input := BatchPreviewInput{UserID: 7, UploadDir: "/tmp/uploads"}
	service.validateRow(context.Background(), input, &row)
	// message 保存当前行所有缺失字段提示。
	message := strings.Join(row.Errors, "|")
	// expectedMessages 表示本场景必须出现的业务校验提示。
	expectedMessages := []string{"缺少账号ID", "缺少标题", "价格必须大于 0", "库存必须大于 0", "邮费模式必须是 free 或 fixed", "缺少图片"}
	// expected 表示当前待断言的错误提示文本。
	for _, expected := range expectedMessages {
		if !strings.Contains(message, expected) {
			t.Errorf("缺失字段提示缺少 %q: %s", expected, message)
		}
	}
}
