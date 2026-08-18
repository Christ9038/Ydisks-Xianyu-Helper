package composition

import (
	"context"
	"fmt"

	"xianyu-go/internal/adapter"
	itemapp "xianyu-go/internal/application/items"
)

// itemBatchServices 是批量发布运行时、预检和本地收口服务的完整不可变组合结果。
type itemBatchServices struct {
	// coordinator 拥有批量发布 worker 与恢复扫描生命周期。
	coordinator *itemapp.BatchWorkerCoordinator
	// preview 负责导入表格预检与图片校验。
	preview *itemapp.BatchPreviewService
	// management 负责批次查询、取消和重试用例。
	management *itemapp.BatchManagementService
	// localPublish 负责远端发布成功后的本地事务收口。
	localPublish *itemapp.BatchLocalPublishService
}

// buildItemBatchServices 构造商品批量发布所需全部端口和后台组件，错误会阻止半初始化服务暴露。
func buildItemBatchServices(dependencies Dependencies, sessionRecovery adapter.SessionRecoveryHandler, automationDependencies *adapter.AutomationDependencies) (itemBatchServices, error) {
	// itemBatchPublish 是批量远端发布适配器，图片安全回调由 Server 装配时注入。
	itemBatchPublish := dependencies.ItemDependencies.NewItemBatchPublishPort(dependencies.MTopClient, dependencies.Logger, dependencies.UpdateRunningCookie, func(ctx context.Context, cookieID string, err error) {
		if sessionRecovery != nil {
			sessionRecovery(ctx, cookieID, err)
		}
	}, readBatchImageFile, downloadImageURL)
	// itemBatchLocalPublish 将远端发布成功后的本地商品、规则和检查点一次性装配。
	itemBatchLocalPublish, itemBatchLocalPublishErr := itemapp.NewBatchLocalPublishService(
		dependencies.ItemDependencies.NewItemBatchRepository(),
		dependencies.ItemDependencies.NewItemCatalogRepository(),
		automationDependencies.NewAutomationRepository(),
	)
	if itemBatchLocalPublishErr != nil {
		return itemBatchServices{}, fmt.Errorf("构造批量发布本地收口服务失败: %w", itemBatchLocalPublishErr)
	}
	// itemBatchPublisher、itemBatchPublisherErr 分别保存批量发布端口适配器及其组合期错误。
	itemBatchPublisher, itemBatchPublisherErr := adapter.NewItemBatchPublisher(itemBatchPublish, itemBatchLocalPublish)
	if itemBatchPublisherErr != nil {
		return itemBatchServices{}, fmt.Errorf("构造批量发布 publisher 失败: %w", itemBatchPublisherErr)
	}
	// itemBatchRunner、batchRunnerErr 分别保存批量发布 worker 及其必需端口装配错误。
	itemBatchRunner, batchRunnerErr := adapter.NewItemBatchRunnerApplication(dependencies.ItemDependencies.NewItemBatchRepository(), itemBatchPublisher, publishBatchLease, publishBatchFailure)
	if batchRunnerErr != nil {
		return itemBatchServices{}, fmt.Errorf("构造批量发布运行器失败: %w", batchRunnerErr)
	}
	// itemBatchRecovery 负责恢复扫描的批次状态编排；worker 生命周期由协调器拥有。
	itemBatchRecovery, batchRecoveryErr := itemapp.NewBatchRecoveryService(
		dependencies.ItemDependencies.NewItemBatchRepository(),
		itemapp.BatchRecoveryOptions{LeaseDuration: publishBatchLease},
	)
	if batchRecoveryErr != nil {
		return itemBatchServices{}, fmt.Errorf("构造批量发布恢复服务失败: %w", batchRecoveryErr)
	}
	// itemBatchCoordinator 负责批次 worker 的超时、恢复扫描和停止等待。
	itemBatchCoordinator, batchCoordinatorErr := itemapp.NewBatchWorkerCoordinator(itemBatchRunner, itemBatchRecovery, itemapp.BatchWorkerCoordinatorOptions{
		OnWorkerError: func(batchID string, err error) {
			if dependencies.Logger != nil {
				dependencies.Logger.Warn("批量发布 worker 结束", "batch", batchID, "err", err)
			}
		},
		OnRecoveryError: func(err error) {
			if dependencies.Logger != nil {
				dependencies.Logger.Warn("扫描可恢复批量发布任务失败", "err", err)
			}
		},
	})
	if batchCoordinatorErr != nil {
		return itemBatchServices{}, fmt.Errorf("构造批量发布协调器失败: %w", batchCoordinatorErr)
	}
	// itemBatchPreviewPort 提供批量预检所需的非敏感归属与本地图片校验能力。
	itemBatchPreviewPort := dependencies.ItemDependencies.NewItemBatchPreviewPort()
	// itemBatchPreview 是批量发布预检应用服务的构造结果。
	itemBatchPreview, itemBatchPreviewErr := itemapp.NewBatchPreviewService(itemBatchPreviewPort, itemBatchPreviewPort)
	if itemBatchPreviewErr != nil {
		return itemBatchServices{}, fmt.Errorf("构造批量发布预检服务失败: %w", itemBatchPreviewErr)
	}
	// itemBatchManagement 是批次管理应用服务的构造结果。
	itemBatchManagement, itemBatchManagementErr := itemapp.NewBatchManagementService(dependencies.ItemDependencies.NewItemBatchRepository(), adapter.NewBatchManagementRuntime(dependencies.LifecycleContext, itemBatchCoordinator))
	if itemBatchManagementErr != nil {
		return itemBatchServices{}, fmt.Errorf("构造批量发布管理服务失败: %w", itemBatchManagementErr)
	}
	return itemBatchServices{coordinator: itemBatchCoordinator, preview: itemBatchPreview, management: itemBatchManagement, localPublish: itemBatchLocalPublish}, nil
}

// itemCatalogServices 是商品目录、发布和预检持久化服务的完整组合结果。
type itemCatalogServices struct {
	// catalog 提供商品查询服务。
	catalog *itemapp.CatalogService
	// mutation 提供商品写入服务。
	mutation *itemapp.CatalogMutationService
	// categoryRecommendation 提供平台类目推荐服务。
	categoryRecommendation *itemapp.CategoryRecommendationService
	// previewPersistence 保存批量预检结果。
	previewPersistence *itemapp.BatchPreviewPersistenceService
	// singlePublish 提供单商品发布服务。
	singlePublish *itemapp.Service
}

// buildItemCatalogServices 构造商品目录和发布服务，并把平台会话恢复回调限制在组合根。
func buildItemCatalogServices(dependencies Dependencies, sessionRecovery adapter.SessionRecoveryHandler) (itemCatalogServices, error) {
	// itemCatalogRepository 是商品读写用例共用的数据库适配器。
	itemCatalogRepository := dependencies.ItemDependencies.NewItemCatalogRepository()
	// itemCatalog 是商品列表和详情读取用例的应用服务。
	itemCatalog, itemCatalogErr := itemapp.NewCatalogService(itemCatalogRepository)
	if itemCatalogErr != nil {
		return itemCatalogServices{}, fmt.Errorf("构造商品目录服务失败: %w", itemCatalogErr)
	}
	// itemCatalogMutation 是商品写入用例的应用服务。
	itemCatalogMutation, itemCatalogMutationErr := itemapp.NewCatalogMutationService(itemCatalogRepository)
	if itemCatalogMutationErr != nil {
		return itemCatalogServices{}, fmt.Errorf("构造商品目录写入服务失败: %w", itemCatalogMutationErr)
	}
	// itemPublishPort 是单商品与批量发布共享的平台凭证适配器。
	itemPublishPort := dependencies.ItemDependencies.NewItemPublishPort(dependencies.MTopClient, dependencies.Logger, dependencies.UpdateRunningCookie, func(ctx context.Context, cookieID string, err error) bool {
		return sessionRecovery != nil && sessionRecovery(ctx, cookieID, err)
	})
	// itemCategoryRecommendation 复用商品发布端口承载类目推荐和响应会话写回。
	itemCategoryRecommendation, itemCategoryRecommendationErr := itemapp.NewCategoryRecommendationService(itemPublishPort)
	if itemCategoryRecommendationErr != nil {
		return itemCatalogServices{}, fmt.Errorf("构造商品类目推荐服务失败: %w", itemCategoryRecommendationErr)
	}
	// itemBatchPreviewPersistence 将预检结果持久化到批次仓储，隔离数据库模型转换。
	itemBatchPreviewPersistence, itemBatchPreviewPersistenceErr := itemapp.NewBatchPreviewPersistenceService(dependencies.ItemDependencies.NewItemBatchRepository())
	if itemBatchPreviewPersistenceErr != nil {
		return itemCatalogServices{}, fmt.Errorf("构造批量预检持久化服务失败: %w", itemBatchPreviewPersistenceErr)
	}
	// itemSinglePublish 是单商品发布应用服务及其基础设施端口的构造结果。
	itemSinglePublish, itemSinglePublishErr := itemapp.NewService(
		itemPublishPort,
		dependencies.ItemDependencies.NewItemPublishRepository(),
	)
	if itemSinglePublishErr != nil {
		return itemCatalogServices{}, fmt.Errorf("构造单商品发布服务失败: %w", itemSinglePublishErr)
	}
	return itemCatalogServices{catalog: itemCatalog, mutation: itemCatalogMutation, categoryRecommendation: itemCategoryRecommendation, previewPersistence: itemBatchPreviewPersistence, singlePublish: itemSinglePublish}, nil
}
