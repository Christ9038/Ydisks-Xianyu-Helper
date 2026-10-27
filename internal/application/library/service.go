package library

// Service 把图片库、商品模板和模板发布预检三个用例组合为一个应用入口，供传输层通过单一消费者端口使用。
// 三个服务的方法名互不冲突，嵌入后各自的锁和依赖保持独立。
type Service struct {
	*ImageService
	*TemplateService
	*TemplatePublishService
}

// NewService 组合三个已构造完成的服务。
func NewService(images *ImageService, templates *TemplateService, publish *TemplatePublishService) *Service {
	return &Service{ImageService: images, TemplateService: templates, TemplatePublishService: publish}
}
