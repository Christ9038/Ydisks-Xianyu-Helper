package orders

import "context"

// beginDiscovery 为 s 的 cookieID 原子登记 ctx 对应的新代次，返回本次调用的独立指针身份。
// 原子操作仅访问内存；即使调用共享 ctx 值，参数地址仍不同，兼容服务零值与结构体字面量。
func (s *RefreshService) beginDiscovery(ctx context.Context, cookieID string) *context.Context {
	// generation 用独立参数地址区分并发调用，不比较上下文内容，也不输出账号或凭证。
	generation := &ctx
	s.discoveries.Store(cookieID, generation)
	return generation
}

// finishDiscovery 在 s 的 cookieID 调用完成或取消返回时只删除 generation 自身，不移除新调用的映射。
// 最新请求结束后映射缺失也会拒绝旧结果，不恢复旧请求资格；不创建后台清理协程。
func (s *RefreshService) finishDiscovery(cookieID string, generation *context.Context) {
	s.discoveries.CompareAndDelete(cookieID, generation)
}

// discoveryCurrent 在凭证锁内核对 s 中 cookieID 的 generation 是否仍最新，不保留内存锁。
// 新请求须先取得同一凭证锁才能读取请求快照，因此不会先提交再被当前旧提交覆盖。
func (s *RefreshService) discoveryCurrent(cookieID string, generation *context.Context) bool {
	// current、exists 是最新代次及映射存在性；映射缺失不能让仍在运行的旧请求重新取得提交资格。
	current, exists := s.discoveries.Load(cookieID)
	return exists && current == generation
}
