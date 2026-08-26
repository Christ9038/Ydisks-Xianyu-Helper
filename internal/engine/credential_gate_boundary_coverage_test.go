package engine

import (
	"context"
	"testing"
)

// TestCredentialRefreshGateCoversNilContextAndLazyInitialization 验证刷新门拒绝空 Context，并能从零值状态惰性初始化。
func TestCredentialRefreshGateCoversNilContextAndLazyInitialization(t *testing.T) {
	// state 是尚未初始化刷新通道的零值凭证状态。
	state := credentialState{}
	// nilContext 表示调用方遗漏生命周期 Context 的非法输入。
	var nilContext context.Context
	// nilRelease、nilErr 保存空 Context 的获取结果。
	nilRelease, nilErr := state.acquireRefreshGate(nilContext)
	if nilRelease != nil || nilErr == nil {
		t.Fatalf("空 Context 应拒绝刷新门：releaseNil=%v err=%v", nilRelease == nil, nilErr)
	}
	// release、acquireErr 保存零值凭证状态惰性初始化后的通道结果。
	release, acquireErr := state.acquireRefreshGate(context.Background())
	if release == nil || acquireErr != nil {
		t.Fatalf("零值凭证状态应初始化刷新门：releaseNil=%v err=%v", release == nil, acquireErr)
	}
	release()
}
