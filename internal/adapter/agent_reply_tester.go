package adapter

import (
	"context"
	"errors"
	"time"

	"xianyu-go/internal/agentclient"
	agentreplyapp "xianyu-go/internal/application/agentreply"
	"xianyu-go/internal/llmclient"
	"xianyu-go/internal/netguard"
)

// agentReplyTester 通过协议客户端向 Agent 发送一次 ping，用于配置页的“测试连接”。
// 它共用与自动回复相同的出站策略，但不执行轮询、不发送任何买家数据，也不写入数据库。
type agentReplyTester struct{}

// NewAgentReplyTester 创建 Agent 连接测试适配器。
func NewAgentReplyTester() agentreplyapp.Tester { return agentReplyTester{} }

// Ping 发送一次 ping 事件并把结果归类；调用方取消 ctx 时返回 ctx 错误，其余失败编码为诊断结果。
func (agentReplyTester) Ping(ctx context.Context, endpoint agentreplyapp.Endpoint) (agentreplyapp.TestResult, error) {
	if endpoint.Protocol == agentreplyapp.ProtocolOpenAI || endpoint.Protocol == agentreplyapp.ProtocolAnthropic {
		return pingModel(ctx, endpoint)
	}
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

// pingModel 向 openai/anthropic 协议的模型端点发送一次最小对话请求（固定文字、16 个 token 上限），只验证地址、密钥与模型的组合可用。
// 请求不含任何买家、商品或订单数据，只在用户点击“测试连接”时发生一次，不重试。
func pingModel(ctx context.Context, endpoint agentreplyapp.Endpoint) (agentreplyapp.TestResult, error) {
	// started 是请求开始时刻，用于计算诊断耗时。
	started := time.Now()
	// result 保存诊断结果；耗时在所有分支都会记录。
	result := agentreplyapp.TestResult{}
	// finish 补全耗时后返回结果。
	finish := func(result agentreplyapp.TestResult) (agentreplyapp.TestResult, error) {
		result.DurationMillis = time.Since(started).Milliseconds()
		return result, nil
	}
	// client、clientErr 是按用户配置地址构造的受限客户端；地址被出站策略拒绝时按未能到达处理。
	client, clientErr := netguard.ConfiguredEndpointHTTPClient(endpoint.URL, endpoint.Timeout)
	if clientErr != nil {
		result.Status = agentreplyapp.TestStatusUnreachable
		return finish(result)
	}
	// completion、err 保存模型响应及调用错误。
	completion, err := llmclient.Complete(ctx, llmclient.Endpoint{
		Protocol: endpoint.Protocol, BaseURL: endpoint.URL, APIKey: endpoint.Token, Model: endpoint.Model, HTTPClient: client,
	}, llmclient.Request{UserText: "ping", MaxTokens: 16})
	// status 是上游返回的 HTTP 状态码，0 表示请求未到达服务端。
	switch status := llmclient.StatusCode(err); {
	case err == nil:
		result.Status, result.Handled = agentreplyapp.TestStatusOK, !completion.Refused
		if completion.Text != "" {
			result.ReplyCount = 1
		}
	case status != 0:
		result.Status, result.StatusCode = agentreplyapp.TestStatusHTTPError, status
	case ctx.Err() != nil:
		return agentreplyapp.TestResult{}, ctx.Err()
	default:
		result.Status = agentreplyapp.TestStatusUnreachable
	}
	return finish(result)
}
