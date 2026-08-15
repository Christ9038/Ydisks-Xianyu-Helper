package orders

// ServiceSet 聚合订单领域的应用服务实例，由应用层统一负责构造。
// HTTP/Server 适配层只持有该集合，不再分别创建业务服务实现。
type ServiceSet struct {
	// List 负责订单列表分页和筛选用例。
	List *ListService
	// Detail 负责订单详情读取和商品补全用例。
	Detail *DetailService
	// Delete 负责订单逻辑删除用例。
	Delete *DeleteService
	// Update 负责订单字段与商品标题更新用例。
	Update *UpdateService
	// Import 负责订单文件导入用例。
	Import *ImportService
	// ManualShip 负责手动发货和补偿用例。
	ManualShip *ManualShipService
	// Refresh 负责订单发现、详情刷新和缺失清理用例。
	Refresh *RefreshService
	// RefreshJobs 负责订单刷新后台任务的持久化操作。
	RefreshJobs RefreshJobRepository
}

// NewServiceSet 使用应用层 Port 构造完整订单服务集合。
func NewServiceSet(repository Repository, refreshRepository RefreshRepository, manualRuntime ManualShipRuntime, refreshRuntime RefreshRuntime, refreshJobs RefreshJobRepository, detailChunkSize int) *ServiceSet {
	return &ServiceSet{
		List:        NewListService(repository),
		Detail:      NewDetailService(repository),
		Delete:      NewDeleteService(repository),
		Update:      NewUpdateService(repository),
		Import:      NewImportService(repository),
		ManualShip:  NewManualShipService(repository, manualRuntime),
		Refresh:     NewRefreshService(refreshRepository, refreshRuntime, detailChunkSize),
		RefreshJobs: refreshJobs,
	}
}
