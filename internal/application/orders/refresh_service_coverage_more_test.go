package orders

import (
	"context"
	"errors"
	"testing"
)

// assertRefreshSingleError 校验单订单刷新返回指定错误。
func assertRefreshSingleError(t *testing.T, service *RefreshService, want error) {
	t.Helper()
	// result、err 保存本次单订单刷新结果及错误。
	result, err := service.RefreshSingle(context.Background(), 7, "order-1")
	if !errors.Is(err, want) {
		t.Fatalf("单订单刷新错误异常: result=%+v got=%v want=%v", result, err, want)
	}
}

// TestRefreshSingleCoversGuardsAndPersistenceErrors 覆盖单订单刷新授权、凭证和写入错误分支。
func TestRefreshSingleCoversGuardsAndPersistenceErrors(t *testing.T) {
	// baseRepository 保存可复用的订单与账号视图。
	baseRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: &Order{OrderID: "order-1", CookieID: "cookie-1", OrderStatus: "processing"}, detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}}
	// runtime 保存可用详情接口运行时。
	runtime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "not-editable"}}}
	// getErr 保存订单读取错误。
	getErr := errors.New("读取订单失败")
	assertRefreshSingleError(t, &RefreshService{repository: &refreshRepositoryFake{getErr: getErr}, runtime: runtime}, getErr)
	assertRefreshSingleError(t, &RefreshService{repository: &refreshRepositoryFake{order: nil}, runtime: runtime}, ErrNotFound)
	assertRefreshSingleError(t, &RefreshService{repository: &refreshRepositoryFake{order: &Order{OrderID: "order-1"}}, runtime: runtime}, ErrForbidden)
	// ownedErr 保存账号归属查询错误。
	ownedErr := errors.New("归属查询失败")
	assertRefreshSingleError(t, &RefreshService{repository: &refreshRepositoryFake{ownedErr: ownedErr, order: baseRepository.order}, runtime: runtime}, ownedErr)
	assertRefreshSingleError(t, &RefreshService{repository: &refreshRepositoryFake{existsResult: boolPtr(false), order: baseRepository.order}, runtime: runtime}, ErrForbidden)
	// loadErr 保存加锁后凭证读取错误。
	loadErr := errors.New("凭证读取失败")
	assertRefreshSingleError(t, &RefreshService{repository: &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: baseRepository.order, detail: baseRepository.detail, loadErr: loadErr}, runtime: runtime}, ErrRefreshCredentialChanged)

	// fetchErr 保存平台详情请求错误。
	fetchErr := errors.New("详情请求失败")
	// fetchRuntime 保存返回平台错误的运行时。
	fetchRuntime := &refreshRuntimeFake{detailAvailable: true, fetchErr: fetchErr}
	// fetchRepository 保存详情请求所需的账号视图。
	fetchRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: baseRepository.order, detail: baseRepository.detail}
	assertRefreshSingleError(t, &RefreshService{repository: fetchRepository, runtime: fetchRuntime}, fetchErr)
	if !fetchRuntime.recovered {
		t.Fatal("平台详情错误后未执行会话恢复")
	}

	// nilDetailRuntime 保存不返回详情实体的运行时。
	nilDetailRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: nil}}
	// nilDetailResult、nilDetailErr 保存不返回详情实体的刷新结果。
	nilDetailResult, nilDetailErr := (&RefreshService{repository: fetchRepository, runtime: nilDetailRuntime}).RefreshSingle(context.Background(), 7, "order-1")
	if nilDetailErr == nil || nilDetailErr.Error() != "订单详情接口未返回结果" {
		t.Fatalf("空详情结果异常: result=%+v err=%v", nilDetailResult, nilDetailErr)
	}

	// upsertErr 保存订单详情写入错误。
	upsertErr := errors.New("订单写入失败")
	// upsertRepository 保存能够返回写入错误的持久化依赖。
	upsertRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: baseRepository.order, detail: baseRepository.detail, upsertErr: upsertErr}
	// invalidStatusRuntime 保存不可编辑状态，服务应沿用本地状态。
	invalidStatusRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "not-editable"}}}
	// result、err 保存不可编辑状态下的写入错误结果。
	result, err := (&RefreshService{repository: upsertRepository, runtime: invalidStatusRuntime}).RefreshSingle(context.Background(), 7, "order-1")
	if !errors.Is(err, upsertErr) || result.Success {
		t.Fatalf("订单写入错误分支异常: result=%+v err=%v", result, err)
	}
	// emptyService 保存没有任何订单刷新依赖的服务。
	emptyService := &RefreshService{}
	// emptyResult、emptyErr 保存空服务的保护性返回。
	emptyResult, emptyErr := emptyService.RefreshSingle(context.Background(), 7, "order-1")
	if emptyErr == nil || emptyResult.Success {
		t.Fatalf("空服务保护分支异常: result=%+v err=%v", emptyResult, emptyErr)
	}
}

// TestRefreshSingleCoversCookieAndCredentialRaceBranches 覆盖 Cookie 提交、凭证竞态和错误合并分支。
func TestRefreshSingleCoversCookieAndCredentialRaceBranches(t *testing.T) {
	// detail 保存单订单详情和扁平 Cookie 更新。
	detail := &RefreshDetail{OrderStatus: "2", UpdatedCookies: "new-cookie"}
	// repository 保存首尾凭证一致的持久化依赖。
	repository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: &Order{OrderID: "order-1", CookieID: "cookie-1", OrderStatus: "processing"}, loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "old-cookie"}, {UserID: 7, Value: "old-cookie"}}}
	// runtime 保存不接管完整 Cookie Jar 的运行时。
	runtime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: detail}, persistConfigured: true, persistHandled: false}
	// result、err 保存扁平 Cookie 提交成功结果。
	result, err := (&RefreshService{repository: repository, runtime: runtime}).RefreshSingle(context.Background(), 7, "order-1")
	if err != nil || !result.Success || runtime.updatedCookie != "new-cookie" {
		t.Fatalf("扁平 Cookie 更新分支异常: result=%+v err=%v cookie=%q", result, err, runtime.updatedCookie)
	}

	// updateErr 保存扁平 Cookie 持久化错误；该错误不应覆盖成功详情。
	updateErr := errors.New("Cookie 保存失败")
	// updateRepository 保存扁平 Cookie 写入错误依赖。
	updateRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: repository.order, detail: &PlatformRuntimeData{UserID: 7, Value: "old-cookie"}, loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "old-cookie"}, {UserID: 7, Value: "old-cookie"}}, updateCookieErr: updateErr}
	// updateRuntime 保存不接管完整 Cookie Jar 的运行时。
	updateRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: detail}, persistConfigured: true, persistHandled: false}
	result, err = (&RefreshService{repository: updateRepository, runtime: updateRuntime}).RefreshSingle(context.Background(), 7, "order-1")
	if err != nil || !result.Success {
		t.Fatalf("扁平 Cookie 保存错误不应阻断详情成功: result=%+v err=%v", result, err)
	}

	// emptyValueRepository 保存完整 Cookie Jar 提交成功但没有新 Cookie 文本的依赖。
	emptyValueRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: repository.order, detail: repository.detail, loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "old-cookie"}, {UserID: 7, Value: "old-cookie"}}}
	// emptyValueRuntime 保存 handled 且变化并返回新 Cookie 文本的运行时。
	emptyValueRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: detail}, persistConfigured: true, persistHandled: true, persistChanged: true, persistValue: "new-cookie"}
	// emptyValueResult、emptyValueErr 保存完整 Cookie 接管结果。
	emptyValueResult, emptyValueErr := (&RefreshService{repository: emptyValueRepository, runtime: emptyValueRuntime}).RefreshSingle(context.Background(), 7, "order-1")
	if emptyValueErr != nil || !emptyValueResult.Success || emptyValueRuntime.updatedCookie != "new-cookie" {
		t.Fatalf("空 Cookie 值分支异常: result=%+v err=%v cookie=%q", emptyValueResult, emptyValueErr, emptyValueRuntime.updatedCookie)
	}

	// persistErr 保存完整 Cookie Jar 提交错误。
	persistErr := errors.New("Cookie Jar 保存失败")
	// persistRepository 保存完整 Cookie Jar 提交所需账号视图。
	persistRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: repository.order, detail: &PlatformRuntimeData{UserID: 7, Value: "old-cookie"}}
	// persistRuntime 保存完整 Cookie Jar 提交错误运行时。
	persistRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: detail}, persistConfigured: true, persistHandled: true, persistChanged: true, persistValue: "runtime-cookie", persistErr: persistErr}
	assertRefreshSingleError(t, &RefreshService{repository: persistRepository, runtime: persistRuntime}, persistErr)

	// changedRepository 让外部请求前后凭证快照不同。
	changedRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: repository.order, loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "old-cookie"}, {UserID: 7, Value: "new-cookie"}}}
	// changedRuntime 保存可成功返回详情的运行时。
	changedRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: detail}}
	assertRefreshSingleError(t, &RefreshService{repository: changedRepository, runtime: changedRuntime}, ErrRefreshCredentialChanged)
}

// TestRefreshCoversSelectionAndUnsupportedBranches 覆盖批量刷新账号筛选、列表扫描和接口能力分支。
func TestRefreshCoversSelectionAndUnsupportedBranches(t *testing.T) {
	// nilService 保存空批量刷新服务的保护性边界。
	var nilService *RefreshService
	// nilResult、nilErr 保存空服务的批量刷新返回。
	nilResult, nilErr := nilService.Refresh(context.Background(), 7, "", "all")
	if nilErr == nil || nilResult.Summary.Total != 0 {
		t.Fatalf("空批量刷新服务分支异常: result=%+v err=%v", nilResult, nilErr)
	}
	// listErr 保存用户账号列表错误。
	listErr := errors.New("账号列表失败")
	assertRefreshError(t, &RefreshService{repository: &refreshRepositoryFake{listOwnedErr: listErr}, runtime: &refreshRuntimeFake{}}, listErr)
	// ownedErr 保存筛选账号归属错误。
	ownedErr := errors.New("筛选归属失败")
	// ownedService 保存带账号筛选的归属错误服务。
	ownedService := &RefreshService{repository: &refreshRepositoryFake{ownedErr: ownedErr}, runtime: &refreshRuntimeFake{}}
	// ownedResult、ownedRefreshErr 保存筛选归属错误结果。
	ownedResult, ownedRefreshErr := ownedService.Refresh(context.Background(), 7, "cookie-1", "all")
	if !errors.Is(ownedRefreshErr, ownedErr) {
		t.Fatalf("筛选账号归属错误异常: result=%+v err=%v", ownedResult, ownedRefreshErr)
	}
	// forbiddenService 保存不属于当前用户的筛选账号服务。
	forbiddenService := &RefreshService{repository: &refreshRepositoryFake{existsResult: boolPtr(false)}, runtime: &refreshRuntimeFake{}}
	// forbiddenResult、forbiddenErr 保存筛选账号拒绝结果。
	forbiddenResult, forbiddenErr := forbiddenService.Refresh(context.Background(), 7, "cookie-1", "all")
	if !errors.Is(forbiddenErr, ErrForbidden) {
		t.Fatalf("筛选账号拒绝异常: result=%+v err=%v", forbiddenResult, forbiddenErr)
	}

	// repository 保存详情扫描目标和扫描错误。
	repository := &refreshRepositoryFake{rows: []OrderRow{{OrderID: "stable", OrderStatus: "completed", Amount: "1.00"}, {OrderID: "pending", OrderStatus: "processing"}}, rowsErr: errors.New("扫描失败")}
	// runtime 保存不支持详情和订单列表的运行时。
	runtime := &refreshRuntimeFake{}
	// result、err 保存不支持平台能力下的结果。
	result, err := (&RefreshService{repository: repository, runtime: runtime}).Refresh(context.Background(), 7, "", "all")
	// 列表能力缺失与详情扫描失败必须分别报告，不能吞掉第二个错误。
	if err != nil || !result.PartialFailure || result.Summary.Failed != 2 || len(result.Results) != 2 || result.Results[1].Error != "读取待同步订单失败" {
		t.Fatalf("不支持订单列表分支异常: result=%+v err=%v", result, err)
	}

	// filterRepository 保存可筛选的订单扫描结果。
	filterRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, rows: []OrderRow{{OrderID: "other", OrderStatus: "processing"}}}
	// filterRuntime 保存详情可用但订单列表不可用的运行时。
	filterRuntime := &refreshRuntimeFake{detailAvailable: true}
	result, err = (&RefreshService{repository: filterRepository, runtime: filterRuntime}).Refresh(context.Background(), 7, "cookie-1", "completed")
	if err != nil || result.Summary.DetailTotal != 0 {
		t.Fatalf("状态筛选分支异常: result=%+v err=%v", result, err)
	}
}

// TestRefreshCoversDiscoveryErrorAndDetailUnavailableBranches 覆盖订单发现错误、会话过期和详情跳过分支。
func TestRefreshCoversDiscoveryErrorAndDetailUnavailableBranches(t *testing.T) {
	// fetchErr 保存平台订单列表错误。
	fetchErr := errors.New("订单列表请求失败")
	// repository 保存发现所需的有效凭证。
	repository := &refreshRepositoryFake{detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}}
	// runtime 保存普通订单列表错误。
	runtime := &refreshRuntimeFake{soldAvailable: true, fetchErr: fetchErr}
	// result、err 保存普通发现错误结果。
	result, err := (&RefreshService{repository: repository, runtime: runtime}).Refresh(context.Background(), 7, "", "all")
	if err != nil || result.Summary.Failed != 1 || len(result.Results) != 1 {
		t.Fatalf("普通发现错误分支异常: result=%+v err=%v", result, err)
	}

	// expiredRuntime 保存会话过期的订单列表错误。
	expiredRuntime := &refreshRuntimeFake{soldAvailable: true, fetchErr: fetchErr, expired: true}
	result, err = (&RefreshService{repository: repository, runtime: expiredRuntime}).Refresh(context.Background(), 7, "", "all")
	if err != nil || !expiredRuntime.recovered || result.Summary.Failed != 1 {
		t.Fatalf("会话过期发现分支异常: result=%+v err=%v", result, err)
	}

	// noDetailRepository 保存新订单扫描目标。
	noDetailRepository := &refreshRepositoryFake{detail: repository.detail, rows: []OrderRow{{OrderID: "order-1", OrderStatus: "processing"}}, orders: map[string]*Order{}}
	// noDetailRuntime 保存可发现订单但不支持详情的运行时。
	noDetailRuntime := &refreshRuntimeFake{soldAvailable: true, soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order-1", OrderStatus: "processing"}}}}
	result, err = (&RefreshService{repository: noDetailRepository, runtime: noDetailRuntime}).Refresh(context.Background(), 7, "", "all")
	if err != nil || result.Summary.DetailTotal != 1 || result.Message == "" {
		t.Fatalf("详情接口跳过分支异常: result=%+v err=%v", result, err)
	}
}

// TestRefreshCoversDiscoveryPersistenceBranches 覆盖发现阶段凭证竞态、Cookie 更新和缺失清理错误。
func TestRefreshCoversDiscoveryPersistenceBranches(t *testing.T) {
	// baseDetail 保存发现请求使用的账号视图。
	baseDetail := &PlatformRuntimeData{UserID: 7, Value: "cookie"}
	// deleteErr 保存缺失订单清理错误。
	deleteErr := errors.New("缺失订单清理失败")
	// deleteRepository 保存可完成发现写入但清理失败的依赖。
	deleteRepository := &refreshRepositoryFake{detail: baseDetail, deleteErr: deleteErr, orders: map[string]*Order{}}
	// deleteRuntime 保存有效订单列表响应。
	deleteRuntime := &refreshRuntimeFake{soldAvailable: true, soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order-1", OrderStatus: "processing"}}}}
	// result、err 保存缺失清理结果。
	result, err := (&RefreshService{repository: deleteRepository, runtime: deleteRuntime}).Refresh(context.Background(), 7, "", "all")
	if err != nil || result.Summary.Failed != 1 {
		t.Fatalf("缺失订单清理错误分支异常: result=%+v err=%v", result, err)
	}

	// cookieRepository 保存凭证前后一致的账号视图。
	cookieRepository := &refreshRepositoryFake{detail: baseDetail, loadDetails: []*PlatformRuntimeData{baseDetail, baseDetail}, orders: map[string]*Order{}}
	// cookieRuntime 保存带 Cookie 更新的订单列表响应。
	cookieRuntime := &refreshRuntimeFake{soldAvailable: true, soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order-1", OrderStatus: "processing"}}, CookieUpdate: RefreshCookieUpdate{Changed: true, Value: "new-cookie"}}}
	_, err = (&RefreshService{repository: cookieRepository, runtime: cookieRuntime}).Refresh(context.Background(), 7, "", "all")
	if err != nil || cookieRuntime.updatedCookie != "new-cookie" {
		t.Fatalf("发现阶段 Cookie 更新分支异常: err=%v cookie=%q", err, cookieRuntime.updatedCookie)
	}

	// raceRepository 保存外部请求前后不一致的凭证视图。
	raceRepository := &refreshRepositoryFake{detail: baseDetail, loadDetails: []*PlatformRuntimeData{baseDetail, {UserID: 7, Value: "rotated"}}}
	// raceRuntime 保存有效订单列表响应。
	raceRuntime := &refreshRuntimeFake{soldAvailable: true, soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order-1"}}}}
	_, err = (&RefreshService{repository: raceRepository, runtime: raceRuntime}).Refresh(context.Background(), 7, "", "all")
	if err != nil {
		t.Fatalf("发现凭证竞态不应抛出批量顶层错误: %v", err)
	}

	// findErr 保存批量读取本地订单错误。
	findErr := errors.New("批量读取失败")
	// findRepository 保存返回批量读取错误的依赖。
	findRepository := &refreshRepositoryFake{detail: baseDetail, batchFindErr: findErr}
	// findRuntime 保存有效订单列表响应。
	findRuntime := &refreshRuntimeFake{soldAvailable: true, soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order-1"}}}}
	_, err = (&RefreshService{repository: findRepository, runtime: findRuntime}).Refresh(context.Background(), 7, "", "all")
	if err != nil {
		t.Fatalf("发现批量读取错误不应抛出批量顶层错误: %v", err)
	}

	// persistErrorRepository 保存发现阶段 Cookie Jar 提交错误的仓储。
	persistErrorRepository := &refreshRepositoryFake{detail: baseDetail, loadDetails: []*PlatformRuntimeData{baseDetail, baseDetail}}
	// persistError 保存发现响应 Cookie Jar 写入错误。
	persistError := errors.New("发现 Cookie 提交失败")
	// persistErrorRuntime 保存带显式提交错误的订单列表运行时。
	persistErrorRuntime := &refreshRuntimeFake{soldAvailable: true, soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order-1"}}}, persistConfigured: true, persistErr: persistError}
	_, err = (&RefreshService{repository: persistErrorRepository, runtime: persistErrorRuntime}).Refresh(context.Background(), 7, "", "all")
	if err != nil {
		t.Fatalf("发现 Cookie 提交错误不应升级为顶层错误: %v", err)
	}

	// invalidCredentialService 保存初始凭证缺失场景的服务。
	invalidCredentialService := &RefreshService{repository: &refreshRepositoryFake{}, runtime: &refreshRuntimeFake{soldAvailable: true}}
	// invalidCredentialResult、invalidCredentialErr 保存初始凭证校验结果。
	invalidCredentialResult, invalidCredentialErr := invalidCredentialService.Refresh(context.Background(), 7, "", "all")
	if invalidCredentialErr != nil || invalidCredentialResult.Summary.Failed != 1 {
		t.Fatalf("初始凭证缺失分支异常: result=%+v err=%v", invalidCredentialResult, invalidCredentialErr)
	}
}

// TestRefreshCoversCursorSkipAndSessionBranches 覆盖订单游标重复、稳定订单跳过和会话过期后的详情跳过分支。
func TestRefreshCoversCursorSkipAndSessionBranches(t *testing.T) {
	// rows 保存恰好一页的稳定订单，第二次读取会命中重复游标保护。
	rows := make([]OrderRow, 500)
	// index 是当前初始化订单行的下标。
	for index := range rows {
		rows[index] = OrderRow{OrderID: "stable", OrderStatus: "completed", Amount: "1.00"}
	}
	// cursorRepository 保存游标重复和稳定订单跳过场景的仓储。
	cursorRepository := &refreshRepositoryFake{rows: rows}
	// cursorRuntime 保存不请求详情的运行时。
	cursorRuntime := &refreshRuntimeFake{}
	// cursorResult、cursorErr 保存游标扫描结果。
	cursorResult, cursorErr := (&RefreshService{repository: cursorRepository, runtime: cursorRuntime}).Refresh(context.Background(), 7, "", "all")
	if cursorErr != nil || cursorResult.Summary.DetailTotal != 0 {
		t.Fatalf("游标重复或稳定订单跳过异常: result=%+v err=%v", cursorResult, cursorErr)
	}

	// expiredRepository 保存会话过期后仍有本地详情目标的仓储。
	expiredRepository := &refreshRepositoryFake{detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}, rows: []OrderRow{{OrderID: "order-1", OrderStatus: "processing"}}}
	// expiredRuntime 保存发现阶段会话过期的运行时。
	expiredRuntime := &refreshRuntimeFake{soldAvailable: true, soldResult: RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "remote-1"}}}, fetchErr: errors.New("会话过期"), expired: true, detailAvailable: true}
	// expiredResult、expiredErr 保存会话过期后的批量结果。
	expiredResult, expiredErr := (&RefreshService{repository: expiredRepository, runtime: expiredRuntime}).Refresh(context.Background(), 7, "", "all")
	if expiredErr != nil || expiredResult.Summary.DetailTotal != 0 || !expiredRuntime.recovered {
		t.Fatalf("会话过期详情跳过异常: result=%+v err=%v", expiredResult, expiredErr)
	}

	// detailExpiredRepository 保存详情阶段会话过期的本地目标。
	detailExpiredRepository := &refreshRepositoryFake{detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}, rows: []OrderRow{{OrderID: "order-1", OrderStatus: "processing"}}}
	// detailExpiredRuntime 保存详情请求会话过期的运行时。
	detailExpiredRuntime := &refreshRuntimeFake{detailAvailable: true, detailErrors: []error{errors.New("详情会话过期")}, expired: true}
	// detailExpiredResult、detailExpiredErr 保存详情阶段过期结果。
	detailExpiredResult, detailExpiredErr := (&RefreshService{repository: detailExpiredRepository, runtime: detailExpiredRuntime}).Refresh(context.Background(), 7, "", "all")
	if detailExpiredErr != nil || !detailExpiredResult.PartialFailure || !detailExpiredRuntime.recovered {
		t.Fatalf("详情会话过期分支异常: result=%+v err=%v", detailExpiredResult, detailExpiredErr)
	}
}

// TestRefreshCoversSoldOrderNormalizationBranches 覆盖远端订单空标识、去重、砍价标记和空结果分支。
func TestRefreshCoversSoldOrderNormalizationBranches(t *testing.T) {
	// service 保存订单发现写入所需的最小服务。
	service := &RefreshService{repository: &refreshRepositoryFake{orders: map[string]*Order{}}, runtime: &refreshRuntimeFake{}}
	// discovered、updated、newIDs、remoteIDs、err 保存空远端订单结果。
	discovered, updated, newIDs, remoteIDs, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{{OrderID: " "}})
	if err != nil || discovered != 0 || updated != 0 || len(newIDs) != 0 || len(remoteIDs) != 0 {
		t.Fatalf("空远端订单分支异常: discovered=%d updated=%d new=%v remote=%v err=%v", discovered, updated, newIDs, remoteIDs, err)
	}
	// remoteOrders 保存包含重复标识和砍价标记的远端订单。
	remoteOrders := []RefreshSoldOrder{{OrderID: "order-1", IsBargain: true}, {OrderID: "order-1", IsBargain: false}}
	// discovered、updated、newIDs、remoteIDs、err 保存规范化远端订单结果。
	discovered, updated, newIDs, remoteIDs, err = service.persistSoldOrders(context.Background(), "cookie-1", remoteOrders)
	if err != nil || discovered != 1 || updated != 0 || len(newIDs) != 1 || len(remoteIDs) != 1 {
		t.Fatalf("远端订单去重或砍价分支异常: discovered=%d updated=%d new=%v remote=%v err=%v", discovered, updated, newIDs, remoteIDs, err)
	}
}

// TestRefreshDetailChunkCoversFailureAndCommitBranches 覆盖详情分片的请求失败、批量写入和凭证提交分支。
func TestRefreshDetailChunkCoversFailureAndCommitBranches(t *testing.T) {
	// target 保存详情分片目标。
	target := []refreshTarget{{OrderID: "order-1", CurrentStatus: "processing"}, {OrderID: "order-2", CurrentStatus: "processing"}}
	// missingRepository 保存无有效凭证的依赖。
	missingRepository := &refreshRepositoryFake{}
	// missingRuntime 保存详情能力运行时。
	missingRuntime := &refreshRuntimeFake{detailAvailable: true}
	// failed、results、expired 保存凭证缺失分片统计与结果。
	_, _, failed, results, expired := (&RefreshService{repository: missingRepository, runtime: missingRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", target)
	if failed != len(target) || len(results) != len(target) || expired {
		t.Fatalf("详情分片凭证失败分支异常: failed=%d results=%d expired=%v", failed, len(results), expired)
	}

	// writeErr 保存详情批量写入错误。
	writeErr := errors.New("详情批量写入失败")
	// writeRepository 保存有效凭证和批量写入错误。
	writeRepository := &refreshRepositoryFake{detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}, batchUpsertErr: writeErr}
	// writeRuntime 保存一条成功详情和一条空详情。
	writeRuntime := &refreshRuntimeFake{detailResults: []RefreshDetailFetchResult{{Detail: &RefreshDetail{OrderStatus: "2"}}, {}}, detailAvailable: true}
	_, _, failed, results, expired = (&RefreshService{repository: writeRepository, runtime: writeRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", target)
	// 一笔详情为空和一笔数据库失败都必须计入失败，不能把空详情漏计。
	if failed != 2 || len(results) != 2 || expired {
		t.Fatalf("详情批量写入错误分支异常: failed=%d results=%d expired=%v", failed, len(results), expired)
	}

	// expiredErr 保存会话过期详情错误。
	expiredErr := errors.New("详情会话过期")
	// expiredRepository 保存有效凭证依赖。
	expiredRepository := &refreshRepositoryFake{detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}}
	// expiredRuntime 保存会话过期错误运行时。
	expiredRuntime := &refreshRuntimeFake{detailAvailable: true, detailErrors: []error{expiredErr}, expired: true}
	// failed、results、expired 保存会话过期分片统计与结果。
	_, _, failed, results, expired = (&RefreshService{repository: expiredRepository, runtime: expiredRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", target)
	// 会话过期也是实际失败，必须使整轮同步展示未完成。
	if failed != 1 || len(results) != 1 || !expired || !expiredRuntime.recovered {
		t.Fatalf("详情会话过期分支异常: failed=%d results=%d expired=%v recovered=%v", failed, len(results), expired, expiredRuntime.recovered)
	}

	// reloadRepository 让详情请求后凭证复核失败。
	reloadRepository := &refreshRepositoryFake{loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "cookie"}, {UserID: 7, Value: "rotated"}}}
	// reloadRuntime 保存成功详情运行时。
	reloadRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "2"}}}
	_, _, failed, results, expired = (&RefreshService{repository: reloadRepository, runtime: reloadRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", target[:1])
	// 凭证在请求期间变化时必须在写订单之前拒绝旧结果。
	if failed != 1 || len(results) != 1 || expired || reloadRepository.batchUpsertCount != 0 || reloadRepository.upsertCount != 0 {
		t.Fatalf("详情完成后凭证复核分支异常: failed=%d results=%d expired=%v", failed, len(results), expired)
	}

	// persistRepository 保存完整 Cookie Jar 提交所需凭证。
	persistRepository := &refreshRepositoryFake{loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "cookie"}, {UserID: 7, Value: "cookie"}}}
	// persistErr 保存详情 Cookie Jar 提交错误。
	persistErr := errors.New("详情 Cookie 提交失败")
	// persistRuntime 保存成功详情和 Cookie Jar 错误。
	persistRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "processing"}, CookieUpdate: RefreshCookieUpdate{Changed: true, Value: "new-cookie"}}, persistConfigured: true, persistHandled: true, persistChanged: true, persistValue: "new-cookie", persistErr: persistErr}
	_, _, failed, results, expired = (&RefreshService{repository: persistRepository, runtime: persistRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", target[:1])
	if failed == 0 || len(results) < 2 || expired {
		t.Fatalf("详情 Cookie 提交错误分支异常: failed=%d results=%d expired=%v", failed, len(results), expired)
	}

	// invalidStatusRepository 保存详情状态归一化使用的账号视图。
	invalidStatusRepository := &refreshRepositoryFake{loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "cookie"}, {UserID: 7, Value: "cookie"}}}
	// invalidStatusRuntime 保存不可编辑远端状态。
	invalidStatusRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "not-editable"}}}
	// invalidStatusUpdated、invalidStatusNoChange、invalidStatusFailed、invalidStatusResults、invalidStatusExpired 保存状态回退结果。
	invalidStatusUpdated, invalidStatusNoChange, invalidStatusFailed, invalidStatusResults, invalidStatusExpired := (&RefreshService{repository: invalidStatusRepository, runtime: invalidStatusRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", []refreshTarget{{OrderID: "order-1", CurrentStatus: "processing"}})
	if invalidStatusUpdated != 0 || invalidStatusNoChange != 1 || invalidStatusFailed != 0 || len(invalidStatusResults) != 1 || invalidStatusExpired {
		t.Fatalf("详情状态回退分支异常: updated=%d noChange=%d failed=%d results=%d expired=%v", invalidStatusUpdated, invalidStatusNoChange, invalidStatusFailed, len(invalidStatusResults), invalidStatusExpired)
	}

	// reloadErrorRepository 保存详情完成后凭证读取错误。
	reloadErrorRepository := &refreshRepositoryFake{detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}, loadErrors: []error{nil, errors.New("详情后凭证读取失败")}}
	// reloadErrorRuntime 保存能够完成详情请求的运行时。
	reloadErrorRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "processing"}}}
	// reloadErrorFailed、reloadErrorResults、reloadErrorExpired 保存详情后复核错误结果。
	_, _, reloadErrorFailed, reloadErrorResults, reloadErrorExpired := (&RefreshService{repository: reloadErrorRepository, runtime: reloadErrorRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", []refreshTarget{{OrderID: "order-1", CurrentStatus: "processing"}})
	// 复核失败应只有失败结果，不能先写入再同时报告成功和失败。
	if reloadErrorFailed != 1 || len(reloadErrorResults) != 1 || reloadErrorExpired || reloadErrorRepository.batchUpsertCount != 0 {
		t.Fatalf("详情后凭证读取错误分支异常: failed=%d results=%d expired=%v", reloadErrorFailed, len(reloadErrorResults), reloadErrorExpired)
	}

	// successRepository 保存成功详情提交所需凭证。
	successRepository := &refreshRepositoryFake{loadDetails: []*PlatformRuntimeData{{UserID: 7, Value: "cookie"}, {UserID: 7, Value: "cookie"}}}
	// successRuntime 保存状态变化和 Cookie 更新。
	successRuntime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "3"}, CookieUpdate: RefreshCookieUpdate{Changed: true, Value: "new-cookie"}}, persistConfigured: true, persistHandled: true, persistChanged: true, persistValue: "new-cookie"}
	// updated、noChange、failed、results、expired 保存成功分片统计和结果。
	updated, noChange, failed, results, expired := (&RefreshService{repository: successRepository, runtime: successRuntime}).refreshDetailChunk(context.Background(), 7, "cookie-1", []refreshTarget{{OrderID: "order-1", CurrentStatus: "pending_ship"}})
	if updated != 1 || noChange != 0 || failed != 0 || len(results) != 1 || expired || successRuntime.updatedCookie != "new-cookie" {
		t.Fatalf("详情成功分支异常: updated=%d noChange=%d failed=%d results=%d expired=%v cookie=%q", updated, noChange, failed, len(results), expired, successRuntime.updatedCookie)
	}
}

// assertRefreshError 校验批量刷新返回指定错误。
func assertRefreshError(t *testing.T, service *RefreshService, want error) {
	t.Helper()
	// result、err 保存批量刷新结果及错误。
	result, err := service.Refresh(context.Background(), 7, "", "all")
	if !errors.Is(err, want) {
		t.Fatalf("批量刷新错误异常: result=%+v got=%v want=%v", result, err, want)
	}
}

// boolPtr 返回测试所需的布尔指针。
func boolPtr(value bool) *bool {
	// result 保存可被用例修改或比较的布尔指针。
	result := value
	return &result
}
