package server

// applicationServices 聚合 Server 使用的应用服务实例，统一管理共享基础设施依赖。
type applicationServices struct {
	// orders 是订单应用服务。
	orders *orderApplicationService
	// itemPublish 是商品发布应用服务。
	itemPublish *itemPublishService
	// accountLogin 是账号登录应用服务。
	accountLogin *accountLoginService
	// communication 是聊天、通知和账号任务应用服务。
	communication *communicationService
	// analytics 是订单分析应用服务。
	analytics *analyticsService
}

// newApplicationServices 为指定 Server 装配全部应用服务实例。
func newApplicationServices(server *Server) *applicationServices {
	return &applicationServices{
		orders:        &orderApplicationService{server: server, repository: newStoreOrderRepository(server.Store)},
		itemPublish:   &itemPublishService{server: server, repository: newStoreItemPublishRepository(server.Store)},
		accountLogin:  &accountLoginService{server: server, repository: newStoreAccountLoginRepository(server.Store)},
		communication: &communicationService{server: server, repository: newStoreCommunicationRepository(server.Store)},
		analytics:     &analyticsService{repository: newStoreAnalyticsRepository(server.Store)},
	}
}

// applicationServiceSet 返回当前 Server 的应用服务集合，并兼容测试 Server。
func (s *Server) applicationServiceSet() *applicationServices {
	if s.applications != nil {
		return s.applications
	}
	return newApplicationServices(s)
}
