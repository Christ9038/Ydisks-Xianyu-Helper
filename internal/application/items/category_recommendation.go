package items

import (
	"context"
	"errors"
	"strings"
)

// ErrCategoryUnsupported 表示当前平台客户端没有实现类目推荐能力。
var ErrCategoryUnsupported = errors.New("当前 MTOP 客户端不支持类目推荐")

// ErrCategoryUnrecognized 表示平台没有返回完整可发布类目。
var ErrCategoryUnrecognized = errors.New("未能自动识别商品类目，请调整标题或图片后重试")

// ErrCategoryCredentialChanged 表示远端调用前后的账号凭证发生并发变化。
var ErrCategoryCredentialChanged = errors.New("账号凭证已变化，请重试")

// ErrCategoryPersistence 表示平台响应中的账号会话无法安全写回。
var ErrCategoryPersistence = errors.New("保存账号登录状态失败")

// CategoryRecommendationPort 定义类目推荐用例所需的平台能力。
type CategoryRecommendationPort interface {
	// RecommendCategory 根据用户账号和关键词返回应用层类目。
	RecommendCategory(context.Context, int64, string, string) (BatchPreviewCategory, error)
}

// CategoryRecommendationService 编排类目推荐输入校验和平台端口调用。
type CategoryRecommendationService struct {
	// port 提供平台类目推荐及会话持久化能力。
	port CategoryRecommendationPort
}

// NewCategoryRecommendationService 创建类目推荐应用服务。
func NewCategoryRecommendationService(port CategoryRecommendationPort) (*CategoryRecommendationService, error) {
	if port == nil {
		return nil, errors.New("类目推荐端口不能为空")
	}
	return &CategoryRecommendationService{port: port}, nil
}

// Recommend 校验账号和关键词后调用类目推荐端口。
func (service *CategoryRecommendationService) Recommend(ctx context.Context, userID int64, cookieID, keyword string) (BatchPreviewCategory, error) {
	if service == nil || service.port == nil {
		return BatchPreviewCategory{}, errors.New("类目推荐服务未初始化")
	}
	if userID <= 0 || strings.TrimSpace(cookieID) == "" {
		return BatchPreviewCategory{}, ErrCategoryCredentialChanged
	}
	if strings.TrimSpace(keyword) == "" {
		return BatchPreviewCategory{}, errors.New("类目关键词不能为空")
	}
	return service.port.RecommendCategory(ctx, userID, strings.TrimSpace(cookieID), strings.TrimSpace(keyword))
}
