package server

import orderapp "xianyu-go/internal/application/orders"
import itemapp "xianyu-go/internal/application/items"

// applicationServices 聚合 Server 使用的应用服务实例，统一管理共享基础设施依赖。
type applicationServices struct {
	// orders 是订单 HTTP 适配器；业务服务集合由应用层统一构造。
	orders *orderHTTPAdapter
	// itemPublish 是商品发布应用服务。
	itemPublish *itemPublishService
	// itemSinglePublish 是仅负责单商品发布用例的纯应用服务。
	itemSinglePublish *itemapp.Service
	// accountLogin 是账号登录应用服务。
	accountLogin *accountLoginService
	// communication 是聊天、通知和账号任务应用服务。
	communication *communicationService
	// analytics 是订单分析应用服务。
	analytics *analyticsService
}

// newApplicationServices 为指定 Server 装配全部应用服务实例。
func newApplicationServices(server *Server) *applicationServices {
	// orderRepository 保存订单应用服务共享的基础设施适配器。
	orderRepository := newStoreOrderRepository(server.Store)
	// orderRuntime 保存订单服务共享的运行时能力适配器。
	orderRuntime := newServerOrderRuntime(server)
	// orderServices 保存应用层统一构造的订单业务服务集合。
	orderServices := orderapp.NewServiceSet(orderRepository, orderRepository, orderRuntime, orderRuntime, newStoreOrderRefreshJobRepository(server.Store), refreshOrderChunkSize)
	return &applicationServices{
		orders:            &orderHTTPAdapter{services: orderServices, repository: orderRepository},
		itemPublish:       &itemPublishService{server: server, repository: newStoreItemPublishRepository(server.Store)},
		itemSinglePublish: newItemPublishApplication(server),
		accountLogin:      &accountLoginService{server: server, repository: newStoreAccountLoginRepository(server.Store)},
		communication:     &communicationService{server: server, repository: newStoreCommunicationRepository(server.Store)},
		analytics:         &analyticsService{repository: newStoreAnalyticsRepository(server.Store)},
	}
}

// applicationServiceSet 返回当前 Server 的应用服务集合，并兼容测试 Server。
func (s *Server) applicationServiceSet() *applicationServices {
	if s.applications != nil {
		return s.applications
	}
	return newApplicationServices(s)
}
