package server

import (
	accountapp "xianyu-go/internal/application/account"
	chatapp "xianyu-go/internal/application/chat"
	itemapp "xianyu-go/internal/application/items"
	orderapp "xianyu-go/internal/application/orders"
)

// applicationServices 聚合 Server 使用的应用服务实例，统一管理共享基础设施依赖。
type applicationServices struct {
	// orders 是订单 HTTP 适配器；业务服务集合由应用层统一构造。
	orders *orderHTTPAdapter
	// itemPublish 是商品发布应用服务。
	itemPublish *itemPublishService
	// itemSinglePublish 是仅负责单商品发布用例的纯应用服务。
	itemSinglePublish *itemapp.Service
	// itemBatchRunner 是商品批量发布 worker 的应用层编排器。
	itemBatchRunner *itemapp.BatchRunner
	// accountLogin 是账号登录应用服务。
	accountLogin *accountLoginService
	// accountProfile 是账号资料刷新应用服务。
	accountProfile *accountapp.ProfileService
	// communication 是聊天、通知和账号任务应用服务。
	communication *communicationService
	// chat 是聊天历史查询应用服务，负责用户归属和分页编排。
	chat *chatapp.Service
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
	// accountProfile 保存账号资料刷新用例的构造结果。
	accountProfile, profileErr := newAccountProfileApplication(server)
	if profileErr != nil {
		panic(profileErr)
	}
	// itemBatchRunner 是批量发布 worker 的构造结果，失败表示必需端口装配错误。
	itemBatchRunner, batchRunnerErr := newItemBatchRunnerApplication(server)
	if batchRunnerErr != nil {
		panic(batchRunnerErr)
	}
	// accountLoginCreate 是手动 Cookie 登录应用服务的构造结果。
	accountLoginCreate, accountLoginCreateErr := newAccountLoginCreateApplication(server)
	if accountLoginCreateErr != nil {
		panic(accountLoginCreateErr)
	}
	// accountQRLogin 是扫码成功凭证持久化应用服务的构造结果；零值测试 Server 暂不装配数据库端口。
	var accountQRLogin *accountapp.QRLoginService
	if server.Store != nil {
		// accountQRLoginErr 保存扫码应用服务装配失败原因。
		var accountQRLoginErr error
		accountQRLogin, accountQRLoginErr = newAccountQRLoginApplication(server)
		if accountQRLoginErr != nil {
			panic(accountQRLoginErr)
		}
	}
	return &applicationServices{
		orders:            &orderHTTPAdapter{services: orderServices, repository: orderRepository},
		itemPublish:       &itemPublishService{server: server, repository: newStoreItemPublishRepository(server.Store)},
		itemSinglePublish: newItemPublishApplication(server),
		itemBatchRunner:   itemBatchRunner,
		accountLogin:      &accountLoginService{server: server, repository: newStoreAccountLoginRepository(server.Store), createApplication: accountLoginCreate, qrApplication: accountQRLogin},
		accountProfile:    accountProfile,
		communication:     &communicationService{server: server, repository: newStoreCommunicationRepository(server.Store)},
		chat:              chatapp.New(newStoreChatApplicationRepository(server.Store)),
		analytics:         &analyticsService{repository: newStoreAnalyticsRepository(server.Store)},
	}
}

// itemBatchRunnerApplication 返回当前 Server 绑定的批量发布 worker 编排器。
func (s *Server) itemBatchRunnerApplication() *itemapp.BatchRunner {
	return s.applicationServiceSet().itemBatchRunner
}

// accountProfileApplication 返回当前 Server 绑定的账号资料应用服务。
func (s *Server) accountProfileApplication() *accountapp.ProfileService {
	return s.applicationServiceSet().accountProfile
}

// applicationServiceSet 返回当前 Server 的应用服务集合，并兼容测试 Server。
func (s *Server) applicationServiceSet() *applicationServices {
	if s.applications != nil {
		return s.applications
	}
	return newApplicationServices(s)
}
