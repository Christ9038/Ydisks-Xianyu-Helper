package adapter

import (
	"context"
	"errors"
	"time"

	itemapp "xianyu-go/internal/application/items"
)

// itemBatchPublisher 将平台远端发布和本地成功收口组合为应用层 BatchPublisher，不依赖 HTTP Server。
type itemBatchPublisher struct {
	// remotePort 负责平台发布与会话变化收集，不读取 HTTP 请求或响应对象。
	remotePort itemapp.BatchPublishPort
	// localService 负责远端成功后的本地商品、规则与检查点事务收口。
	localService itemapp.BatchLocalPublisher
}

// NewItemBatchPublisher 构造批量发布的应用端口适配器，并拒绝缺失的远端或本地依赖。
func NewItemBatchPublisher(remotePort itemapp.BatchPublishPort, localService itemapp.BatchLocalPublisher) (itemapp.BatchPublisher, error) {
	if remotePort == nil || localService == nil {
		return nil, errors.New("批量发布 publisher 依赖不完整")
	}
	return itemBatchPublisher{remotePort: remotePort, localService: localService}, nil
}

// PublishRow 执行单行远端发布并同步本地完成状态；远端结果不确定时保留人工核对语义。
func (publisher itemBatchPublisher) PublishRow(ctx context.Context, userID int64, row itemapp.BatchRow, workerToken string, beforePublish func(context.Context) error) error {
	// outcome、publishErr 分别保存远端发布结果及其错误，远端调用失败时不得写入本地成功状态。
	outcome, publishErr := publisher.remotePort.PublishRemoteRow(ctx, userID, row, workerToken, beforePublish)
	if publishErr != nil {
		// uncertainErr 表示远端请求已经发出但无法确认最终商品状态，调用方必须禁止自动重试。
		var uncertainErr *itemapp.UncertainRemotePublishError
		if errors.As(publishErr, &uncertainErr) {
			return publishErr
		}
		return publishErr
	}
	if outcome.Result == nil {
		return errors.New("发布商品接口未返回结果")
	}
	if outcome.ResponseCookieErr != nil {
		return &itemapp.PostPublishError{Err: outcome.ResponseCookieErr}
	}
	if ctx.Err() != nil {
		return &itemapp.PostPublishError{Err: ctx.Err()}
	}
	return publisher.localService.Complete(ctx, userID, row, workerToken, outcome.Result)
}

// NewItemBatchRunnerApplication 构造批量发布 worker；调用方必须在组合期提供窄仓储、发布端口和失败分类规则。
func NewItemBatchRunnerApplication(repository itemapp.BatchRepository, publisher itemapp.BatchPublisher, leaseDuration time.Duration, classifyFailure func(error, string) (string, string)) (*itemapp.BatchRunner, error) {
	if repository == nil || publisher == nil || leaseDuration <= 0 || classifyFailure == nil {
		return nil, errors.New("批量发布运行器依赖不完整")
	}
	// options 保存 worker 的租约、平台会话判断与失败状态归类，运行期不再回读 Server。
	options := itemapp.BatchRunOptions{
		LeaseDuration:    leaseDuration,
		IsSessionExpired: IsSessionExpiredError,
		ClassifyFailure:  classifyFailure,
	}
	return itemapp.NewBatchRunner(repository, publisher, options)
}
