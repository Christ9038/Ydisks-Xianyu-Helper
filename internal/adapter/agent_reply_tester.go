package adapter

import (
	"context"
	"errors"
	"time"

	"xianyu-go/internal/agentclient"
	agentreplyapp "xianyu-go/internal/application/agentreply"
)

// agentReplyTester 通过协议客户端向 Agent 发送一次 ping，用于配置页的“测试连接”。
// 它共用与自动回复相同的出站策略，但不执行轮询、不发送任何买家数据，也不写入数据库。
type agentReplyTester struct{}

// NewAgentReplyTester 创建 Agent 连接测试适配器。
func NewAgentReplyTester() agentreplyapp.Tester { return agentReplyTester{} }

// Ping 发送一次 ping 事件并把结果归类；调用方取消 ctx 时返回 ctx 错误，其余失败编码为诊断结果。
func (agentReplyTester) Ping(ctx context.Context, endpoint agentreplyapp.Endpoint) (agentreplyapp.TestResult, error) {
	// started 是请求开始时刻，用于计算诊断耗时。
	started := time.Now()
	// response、err 保存协议响应及调用错误。
	response, err := agentclient.Send(ctx, agentclient.Endpoint{URL: endpoint.URL, Token: endpoint.Token, Timeout: endpoint.Timeout},
		agentclient.Request{Event: agentclient.EventPing, TurnID: agentclient.NewTurnID(), Sequence: 1})
	// result 保存诊断结果；耗时在所有分支都会记录。
	result := agentreplyapp.TestResult{DurationMillis: time.Since(started).Milliseconds()}
	// statusErr 保存非 2xx 错误，用于提取状态码。
	var statusErr *agentclient.StatusError
	switch {
	case err == nil:
		result.Status, result.Handled, result.Working, result.ReplyCount = agentreplyapp.TestStatusOK, response.Handled, response.Working, len(response.Replies)
	case errors.As(err, &statusErr):
		result.Status, result.StatusCode = agentreplyapp.TestStatusHTTPError, statusErr.Code
	case errors.Is(err, agentclient.ErrInvalidResponse):
		result.Status = agentreplyapp.TestStatusInvalidResponse
	case ctx.Err() != nil:
		return agentreplyapp.TestResult{}, ctx.Err()
	default:
		result.Status = agentreplyapp.TestStatusUnreachable
	}
	return result, nil
}
