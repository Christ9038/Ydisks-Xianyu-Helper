package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrRefreshDetailUnsupported 表示当前平台运行时不支持订单详情接口。
var ErrRefreshDetailUnsupported = errors.New("当前 Go MTOP 客户端不支持订单详情接口")

// ErrRefreshCredentialChanged 表示刷新期间账号凭证无法通过一致性复核。
var ErrRefreshCredentialChanged = errors.New("账号凭证已变化，请重试")

// RefreshDetail 是平台返回的订单详情字段。
type RefreshDetail struct {
	// Quantity 是订单购买数量。
	Quantity string
	// SpecName 是商品规格名称。
	SpecName string
	// SpecValue 是商品规格值。
	SpecValue string
	// OrderStatus 是平台返回的订单状态。
	OrderStatus string
	// Amount 是订单实付金额。
	Amount string
	// UpdatedCookies 是非完整 Cookie Jar 流程返回的更新 Cookie。
	UpdatedCookies string
}

// RefreshSoldOrder 是平台订单列表可直接落库的字段。
type RefreshSoldOrder struct {
	// OrderID 是订单业务标识。
	OrderID string
	// ItemID 是关联商品标识。
	ItemID string
	// BuyerID 是买家标识。
	BuyerID string
	// CreatedAt 是平台记录的买家下单时间。
	CreatedAt string
	// OrderStatus 是归一化后的订单状态。
	OrderStatus string
	// Quantity 是购买数量。
	Quantity string
	// Amount 是订单金额。
	Amount string
	// ReceiverName 是收货人姓名。
	ReceiverName string
	// ReceiverPhone 是收货人电话。
	ReceiverPhone string
	// ReceiverAddr 是收货地址。
	ReceiverAddr string
	// ReceiverCity 是收货城市。
	ReceiverCity string
	// IsBargain 表示是否为砍价订单。
	IsBargain bool
}

// RefreshDetailFetchResult 是一次订单详情请求及其 Cookie 会话结果。
type RefreshDetailFetchResult struct {
	// Detail 是平台返回的订单详情，可为空。
	Detail *RefreshDetail
	// CookieUpdate 是请求期间观察到的 Cookie 会话变化。
	CookieUpdate RefreshCookieUpdate
}

// RefreshSoldFetchResult 是一次订单列表请求及其 Cookie 会话结果。
type RefreshSoldFetchResult struct {
	// SellerID 是本次已售请求实际会话的非敏感平台身份；为空时禁止跨账号归属修复。
	SellerID string
	// Orders 是平台返回的全部已售订单。
	Orders []RefreshSoldOrder
	// CookieUpdate 是请求期间观察到的 Cookie 会话变化。
	CookieUpdate RefreshCookieUpdate
}

// RefreshCookieUpdate 描述平台请求观察到的 Cookie Jar 更新。
type RefreshCookieUpdate struct {
	// Value 是更新后的平台 Cookie 值。
	Value string
	// MetadataJSON 是更新后的 Cookie 元数据。
	MetadataJSON string
	// Changed 表示请求期间 Cookie 会话是否发生变化。
	Changed bool
	// Handled 表示本次请求是否由完整 Cookie Jar 接管。
	Handled bool
}

// RefreshOrderWrite 描述详情分片批量写入的一条订单记录。
type RefreshOrderWrite struct {
	// OrderID 是待写入订单标识。
	OrderID string
	// Options 是本次详情刷新需要更新的订单字段。
	Options UpsertOptions
}

// RefreshOrderResult 描述订单刷新结果中的单条兼容结果。
type RefreshOrderResult struct {
	// CookieID 是账号刷新结果对应的账号标识。
	CookieID string
	// OrderID 是订单刷新结果对应的订单标识。
	OrderID string
	// Stage 是 discover、detail 或 persist_cookie 阶段。
	Stage string
	// Success 表示当前结果是否成功。
	Success bool
	// Message 是结果提示文本。
	Message string
	// Error 是结果失败详情文本。
	Error string
	// Discovered 是本账号发现的新订单数。
	Discovered int
	// Updated 是本账号订单列表发生变化的数量。
	Updated int
	// SoftDeleted 是本账号被标记删除的订单数。
	SoftDeleted int
	// OldStatus 是详情刷新前的订单状态。
	OldStatus string
	// NewStatus 是详情刷新后的订单状态。
	NewStatus string
}

// RefreshSummary 是批量刷新统计摘要。
type RefreshSummary struct {
	// Restored 是同账号软删除订单恢复数量，不与新增或跨账号修正重复计数。
	Restored int
	// Reassigned 是经过身份核验后纠正历史归属的订单数量。
	Reassigned int
	// Discovered 是发现的新订单数量。
	Discovered int
	// ListUpdated 是订单列表更新数量。
	ListUpdated int
	// SoftDeleted 是标记删除数量。
	SoftDeleted int
	// DetailTotal 是需要补全详情的订单数量。
	DetailTotal int
	// Total 是本次处理订单总数。
	Total int
	// Updated 是状态发生变化的订单数量。
	Updated int
	// NoChange 是状态未发生变化的订单数量。
	NoChange int
	// Failed 是刷新失败数量。
	Failed int
}

// RefreshResult 是批量订单刷新应用结果。
type RefreshResult struct {
	// PartialFailure 表示批量刷新是否存在失败。
	PartialFailure bool
	// Message 是刷新结果说明。
	Message string
	// Summary 是刷新统计摘要。
	Summary RefreshSummary
	// Results 是逐账号或逐订单的结果。
	Results []RefreshOrderResult
}

// SingleRefreshResult 是单订单刷新应用结果。
type SingleRefreshResult struct {
	// Success 表示刷新是否完成。
	Success bool
	// Message 是刷新结果说明。
	Message string
	// Detail 是刷新后的订单详情。
	Detail RefreshDetail
}

// RefreshRepository 定义订单刷新用例所需的最小持久化能力。
type RefreshRepository interface {
	// FindOrderOwnership 读取含软删除行的非敏感归属；userID 限制可见身份，orderID 不存在时返回 ErrNotFound。
	FindOrderOwnership(ctx context.Context, userID int64, orderID string) (RefreshOwnership, error)
	// RecoverSoldOwnership 以已验证旧归属和版本为条件，原子修正账号、合并 options 并记录恢复审计。
	RecoverSoldOwnership(ctx context.Context, userID int64, cookieID string, expected RefreshOwnership, options UpsertOptions) error
	// ExistsOwned 判断账号是否归属于用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
	// ListOwnedIDs 返回用户拥有的账号标识。
	ListOwnedIDs(ctx context.Context, userID int64) ([]string, error)
	// GetOrder 读取订单实体。
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	// FindOrder 读取订单实体并以 exists 区分不存在。
	FindOrder(ctx context.Context, orderID string) (*Order, bool, error)
	// FindOrdersByIDs 只批量读取 cookieID 内的订单实体，防止归属并发变化时读取其他账号的地址。
	FindOrdersByIDs(ctx context.Context, cookieID string, orderIDs []string) (map[string]*Order, error)
	// FindChatIDsByBuyerAndItem 查询账号下与买家和商品同时匹配的聊天会话标识；结果为空或多条时由调用方判定为不可自动关联。
	FindChatIDsByBuyerAndItem(ctx context.Context, cookieID, buyerID, itemID string) ([]string, error)
	// LockCredentials 获取账号凭证互斥锁。
	LockCredentials(cookieID string) func()
	// LoadCookiePlatformDetail 读取平台请求所需的账号视图。
	LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*PlatformRuntimeData, error)
	// UpdateRenewalCookie 保存扁平 Cookie 和元数据。
	UpdateRenewalCookie(ctx context.Context, cookieID, value, metadata string, at int64) error
	// UpsertOrder 写入订单刷新结果。
	UpsertOrder(ctx context.Context, orderID string, options UpsertOptions) error
	// BatchUpsertOrders 在单个事务中批量执行归属校验与版本 CAS；任一订单失败时整批回滚。
	BatchUpsertOrders(ctx context.Context, rows []RefreshOrderWrite) error
	// SoftDeleteMissingOrders 标记远端订单列表中缺失的本地订单。
	SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error)
	// ListOrdersByCookieCursor 使用复合游标读取账号订单。
	ListOrdersByCookieCursor(ctx context.Context, cookieID string, limit int, afterCreatedAt, afterOrderID string) ([]OrderRow, error)
}

// RefreshRuntime 定义订单刷新访问平台和运行时能力的最小 Port。
type RefreshRuntime interface {
	// DetailAvailable 判断平台详情接口是否可用。
	DetailAvailable() bool
	// SoldAvailable 判断平台已售订单接口是否可用。
	SoldAvailable() bool
	// CredentialAvailable 判断账号是否有可用平台凭证。
	CredentialAvailable(detail *PlatformRuntimeData) bool
	// FetchOrderDetail 请求单个订单详情。
	FetchOrderDetail(ctx context.Context, detail *PlatformRuntimeData, orderID string) (RefreshDetailFetchResult, error)
	// FetchSoldOrders 请求账号全部已售订单列表。
	FetchSoldOrders(ctx context.Context, detail *PlatformRuntimeData) (RefreshSoldFetchResult, error)
	// PersistCookieSession 在凭证锁内保存完整 Cookie 会话。
	PersistCookieSession(ctx context.Context, detail *PlatformRuntimeData, update RefreshCookieUpdate) (string, bool, bool, error)
	// UpdateRunningCookie 同步运行时账号 Cookie。
	UpdateRunningCookie(ctx context.Context, cookieID, value string)
	// RecoverExpiredSession 处理平台会话过期。
	RecoverExpiredSession(ctx context.Context, cookieID string, err error) bool
	// IsSessionExpired 判断错误是否表示平台会话过期。
	IsSessionExpired(err error) bool
}

// RefreshService 承载订单单笔和批量刷新的应用编排，构造后不可复制。
// discoveries 的原子操作仅访问内存，不跨数据库或平台 I/O 持锁；提交阶段在凭证锁内复核代次。
// 每次调用拥有独立上下文指针作为代次身份，不创建后台协程，完成时只清理自己的映射。
type RefreshService struct {
	// repository 保存订单刷新所需的持久化 Port。
	repository RefreshRepository
	// runtime 保存订单刷新所需的平台运行时 Port。
	runtime RefreshRuntime
	// detailChunkSize 是批量详情请求的单账号分片大小。
	detailChunkSize int
	// discoveries 按账号保存最新调用的 *context.Context 身份；sync.Map 零值可用，兼容结构体字面量。
	discoveries sync.Map
}

// NewRefreshService 创建订单刷新应用服务。
func NewRefreshService(repository RefreshRepository, runtime RefreshRuntime, detailChunkSize int) *RefreshService {
	if detailChunkSize <= 0 {
		detailChunkSize = 100
	}
	return &RefreshService{repository: repository, runtime: runtime, detailChunkSize: detailChunkSize}
}

// RefreshSingle 刷新单个订单详情并写回本地订单。
func (s *RefreshService) RefreshSingle(ctx context.Context, userID int64, orderID string) (SingleRefreshResult, error) {
	if s == nil || s.repository == nil || s.runtime == nil {
		return SingleRefreshResult{}, errors.New("订单刷新依赖未初始化")
	}
	// order、err 保存订单读取结果及错误。
	order, err := s.repository.GetOrder(ctx, orderID)
	if err != nil {
		return SingleRefreshResult{}, err
	}
	if order == nil {
		return SingleRefreshResult{}, ErrNotFound
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return SingleRefreshResult{}, ErrForbidden
	}
	// owned 保存订单账号归属结果。
	owned, err := s.repository.ExistsOwned(ctx, userID, order.CookieID)
	if err != nil {
		return SingleRefreshResult{}, err
	}
	if !owned {
		return SingleRefreshResult{}, ErrForbidden
	}
	if !s.runtime.DetailAvailable() {
		return SingleRefreshResult{}, ErrRefreshDetailUnsupported
	}
	// cookieID 保存订单所属账号标识。
	cookieID := order.CookieID
	// unlock 保存凭证锁释放函数。
	unlock := s.repository.LockCredentials(cookieID)
	// locked 表示当前函数是否仍持有凭证锁。
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	// latest、err 保存加锁后读取的平台凭证视图及错误。
	latest, err := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
	if err != nil || latest == nil || latest.UserID != userID || !s.runtime.CredentialAvailable(latest) {
		return SingleRefreshResult{}, ErrRefreshCredentialChanged
	}
	// refreshCtx、cancel 限制单订单外部请求最长执行时间。
	refreshCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// unlockBeforeFetch 保存外部请求前释放凭证锁的动作。
	unlock()
	locked = false
	// fetchResult、callErr 保存远端详情结果及错误。
	fetchResult, callErr := s.runtime.FetchOrderDetail(refreshCtx, latest, orderID)
	// unlockAfterFetch 保存外部请求完成后重新获取凭证锁的释放函数。
	unlockAfterFetch := s.repository.LockCredentials(cookieID)
	// latestAfterFetch、reloadErr 保存外部请求后的凭证视图及重读错误。
	latestAfterFetch, reloadErr := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
	// credentialChanged 表示外部请求期间凭证快照是否发生变化。
	credentialChanged := reloadErr != nil || latestAfterFetch == nil || latestAfterFetch.UserID != userID || latestAfterFetch.Value != latest.Value || latestAfterFetch.MetadataJSON != latest.MetadataJSON
	if !credentialChanged {
		// value、valueChanged、handled、persistErr 保存 Cookie 会话提交结果。
		value, valueChanged, handled, persistErr := s.runtime.PersistCookieSession(ctx, latest, fetchResult.CookieUpdate)
		if persistErr != nil {
			callErr = errors.Join(callErr, fmt.Errorf("保存订单详情响应 Cookie Jar: %w", persistErr))
		} else if handled && valueChanged {
			if value != "" {
				s.runtime.UpdateRunningCookie(ctx, cookieID, value)
			}
		} else if !handled && callErr == nil && fetchResult.Detail != nil && fetchResult.Detail.UpdatedCookies != "" && fetchResult.Detail.UpdatedCookies != latest.Value {
			// metadata 保存不含快照的 Cookie 元数据。
			metadata := latest.MetadataJSON
			// saveErr 保存扁平 Cookie 写入错误。
			if saveErr := s.repository.UpdateRenewalCookie(ctx, cookieID, fetchResult.Detail.UpdatedCookies, metadata, time.Now().Unix()); saveErr == nil {
				s.runtime.UpdateRunningCookie(ctx, cookieID, fetchResult.Detail.UpdatedCookies)
			}
		}
	}
	unlockAfterFetch()
	if credentialChanged {
		return SingleRefreshResult{}, ErrRefreshCredentialChanged
	}
	if callErr != nil {
		s.runtime.RecoverExpiredSession(ctx, cookieID, callErr)
		return SingleRefreshResult{}, callErr
	}
	if fetchResult.Detail == nil {
		return SingleRefreshResult{}, errors.New("订单详情接口未返回结果")
	}
	// status 保存规范化后的订单状态。
	status := NormalizeOrderStatus(fetchResult.Detail.OrderStatus)
	if !ValidEditableOrderStatus(status) {
		status = NormalizeOrderStatus(order.OrderStatus)
	}
	// err 保存订单详情写入错误。
	if err := s.repository.UpsertOrder(ctx, orderID, UpsertOptions{CookieID: cookieID, OrderStatus: status, SpecName: fetchResult.Detail.SpecName, SpecValue: fetchResult.Detail.SpecValue, Quantity: fetchResult.Detail.Quantity, Amount: fetchResult.Detail.Amount}); err != nil {
		return SingleRefreshResult{}, err
	}
	return SingleRefreshResult{Success: true, Message: "订单刷新完成", Detail: RefreshDetail{Quantity: fetchResult.Detail.Quantity, SpecName: fetchResult.Detail.SpecName, SpecValue: fetchResult.Detail.SpecValue, OrderStatus: NormalizeOrderStatus(fetchResult.Detail.OrderStatus), Amount: fetchResult.Detail.Amount}}, nil
}

// Refresh 执行当前用户订单的发现、缺失清理和详情补全。
func (s *RefreshService) Refresh(ctx context.Context, userID int64, cookieID, status string) (RefreshResult, error) {
	if s == nil || s.repository == nil || s.runtime == nil {
		return RefreshResult{}, errors.New("订单刷新依赖未初始化")
	}
	// cookieIDs、err 保存用户账号列表及错误。
	cookieIDs, err := s.repository.ListOwnedIDs(ctx, userID)
	if err != nil {
		return RefreshResult{}, err
	}
	if cookieID != "" {
		// owned、ownedErr 保存筛选账号归属结果及错误。
		owned, ownedErr := s.repository.ExistsOwned(ctx, userID, cookieID)
		if ownedErr != nil {
			return RefreshResult{}, ownedErr
		}
		if !owned {
			return RefreshResult{}, ErrForbidden
		}
		cookieIDs = []string{cookieID}
	}
	// summary 保存批量刷新统计。
	summary := RefreshSummary{}
	// results 保存逐账号或逐订单结果。
	results := make([]RefreshOrderResult, 0)
	// newOrderIDs 保存发现的新订单标识。
	newOrderIDs := make(map[string]struct{})
	// sessionExpiredAccounts 保存会话过期账号标识。
	sessionExpiredAccounts := make(map[string]struct{})
	if s.runtime.SoldAvailable() {
		// currentCookieID 是当前执行订单发现的账号标识。
		for _, currentCookieID := range cookieIDs {
			// discovered、updated、discoveryResult、discoveryErr 保存账号发现结果。
			discovered, updated, discoveryResult, discoveryErr := s.discoverAccount(ctx, userID, currentCookieID)
			summary.Discovered += discovered
			summary.ListUpdated += updated
			summary.Restored += discoveryResult.Restored
			summary.Reassigned += discoveryResult.Reassigned
			results = append(results, discoveryResult.Results...)
			// orderID 是本次发现的新订单标识。
			for orderID := range discoveryResult.NewOrderIDs {
				newOrderIDs[orderID] = struct{}{}
			}
			if discoveryResult.SessionExpired {
				sessionExpiredAccounts[currentCookieID] = struct{}{}
			}
			// result 保存当前账号的订单发现结果。
			result := RefreshOrderResult{CookieID: currentCookieID, Stage: "discover", Success: discoveryErr == nil, Discovered: discovered, Updated: updated}
			if discoveryErr != nil {
				// failedOrders 统计逐单业务失败；账号级错误在无逐单失败时只计一次。
				failedOrders := 0
				// item 是本账号恢复结果，成功恢复不能计入失败数量。
				for _, item := range discoveryResult.Results {
					if !item.Success {
						failedOrders++
					}
				}
				if failedOrders == 0 {
					failedOrders = 1
				}
				summary.Failed += failedOrders
				result.Error = discoveryErr.Error()
			}
			if discoveryResult.SoftDeleted >= 0 && discoveryErr == nil {
				result.SoftDeleted = discoveryResult.SoftDeleted
				summary.SoftDeleted += discoveryResult.SoftDeleted
			}
			results = append(results, result)
		}
	} else {
		summary.Failed++
		results = append(results, RefreshOrderResult{Stage: "discover", Message: "当前 MTop 客户端不支持订单列表发现"})
	}
	// ordersByCookie 保存待补全详情的订单目标。
	ordersByCookie := make(map[string][]refreshTarget)
	// currentCookieID 是当前读取详情目标的账号标识。
	for _, currentCookieID := range cookieIDs {
		// blocked 表示账号是否因会话过期而跳过详情。
		if _, blocked := sessionExpiredAccounts[currentCookieID]; blocked {
			continue
		}
		// afterCreatedAt、afterOrderID 保存当前账号订单扫描游标。
		afterCreatedAt, afterOrderID := "", ""
		for {
			// rows、rowErr 保存当前游标页订单及错误。
			rows, rowErr := s.repository.ListOrdersByCookieCursor(ctx, currentCookieID, 500, afterCreatedAt, afterOrderID)
			if rowErr != nil {
				summary.Failed++
				results = append(results, RefreshOrderResult{CookieID: currentCookieID, Stage: "detail", Error: "读取待同步订单失败"})
				break
			}
			// row 是当前游标页的本地订单行。
			for _, row := range rows {
				// currentStatus 保存当前订单归一化状态。
				currentStatus := NormalizeOrderStatus(row.OrderStatus)
				if status != "" && status != "all" && currentStatus != status {
					continue
				}
				// isNewOrder 表示订单是否刚由发现阶段导入。
				_, isNewOrder := newOrderIDs[row.OrderID]
				if !isNewOrder && isStableRefreshStatus(currentStatus) && strings.TrimSpace(row.Amount) != "" {
					continue
				}
				ordersByCookie[currentCookieID] = append(ordersByCookie[currentCookieID], refreshTarget{OrderID: row.OrderID, CurrentStatus: currentStatus})
			}
			if len(rows) < 500 {
				break
			}
			// lastRow 保存当前游标页最后一条订单。
			lastRow := rows[len(rows)-1]
			if lastRow.CreatedAt == afterCreatedAt && lastRow.OrderID == afterOrderID {
				break
			}
			afterCreatedAt, afterOrderID = lastRow.CreatedAt, lastRow.OrderID
		}
	}
	// total 保存需要补全详情的订单总数。
	total := 0
	// targets 是当前账号的待补全详情目标。
	for _, targets := range ordersByCookie {
		total += len(targets)
	}
	summary.DetailTotal, summary.Total = total, total
	if !s.runtime.DetailAvailable() {
		// message 保存详情接口不可用时返回的说明。
		message := "订单列表同步完成"
		if summary.Discovered > 0 {
			message = fmt.Sprintf("订单列表同步完成，发现并导入 %d 个新订单", summary.Discovered)
		}
		if total > 0 {
			message += fmt.Sprintf("；当前 Go MTOP 客户端不支持详情接口，已跳过 %d 个订单", total)
		}
		return finishOrderRefresh(summary, results, message), nil
	}
	if total == 0 {
		return finishOrderRefresh(summary, results, fmt.Sprintf("订单列表同步完成，发现 %d 个新订单；没有需要补全详情的订单", summary.Discovered)), nil
	}
	// currentCookieID、targets 保存当前账号及其详情目标。
	for currentCookieID, targets := range ordersByCookie {
		// accountExpired 表示当前账号是否因会话过期而停止处理。
		accountExpired := false
		// chunk 是当前账号的详情请求分片。
		for _, chunk := range splitRefreshTargets(targets, s.detailChunkSize) {
			// updated、noChange、failed、chunkResults、expired 保存分片处理统计和结果。
			updated, noChange, failed, chunkResults, expired := s.refreshDetailChunk(ctx, userID, currentCookieID, chunk)
			summary.Updated += updated
			summary.NoChange += noChange
			summary.Failed += failed
			results = append(results, chunkResults...)
			if expired {
				accountExpired = true
				break
			}
		}
		if accountExpired {
			continue
		}
	}
	return finishOrderRefresh(summary, results, fmt.Sprintf("订单同步完成，发现 %d 个新订单", summary.Discovered)), nil
}

// refreshDiscoveryResult 保存单账号发现阶段的内部结果。
type refreshDiscoveryResult struct {
	// Restored 和 Reassigned 分别统计同账号恢复与历史归属修正，避免冒充新订单。
	Restored, Reassigned int
	// Results 保存逐单恢复和无法修复的明细，不包含其他管理用户的账号信息。
	Results []RefreshOrderResult
	// NewOrderIDs 保存本次发现的新订单标识。
	NewOrderIDs map[string]struct{}
	// SoftDeleted 保存本次发现阶段标记删除的订单数量。
	SoftDeleted int
	// SessionExpired 表示本账号平台会话已过期。
	SessionExpired bool
}

// discoverAccount 以 ctx 控制 userID 所有的 cookieID 列表发现；返回新增、更新、恢复明细及失败原因。
// 凭证锁保护请求前快照和请求后复核至数据库提交，外部列表请求及运行时更新均在锁外；SQL 事务由仓储拥有。
// 每次调用登记账号发现代次并在所有返回路径清理，ctx 传递给平台；取消或被新请求取代的结果不得提交。
func (s *RefreshService) discoverAccount(ctx context.Context, userID int64, cookieID string) (int, int, refreshDiscoveryResult, error) {
	// emptyResult 保存发现失败时的默认结果。
	emptyResult := refreshDiscoveryResult{NewOrderIDs: make(map[string]struct{})}
	if ctx.Err() != nil {
		return 0, 0, emptyResult, ctx.Err()
	}
	// generation 登记本次独立代次，成功、失败和取消返回时都只清理自身映射。
	generation := s.beginDiscovery(ctx, cookieID)
	defer s.finishDiscovery(cookieID, generation)
	// unlock 保存账号凭证锁释放函数。
	unlock := s.repository.LockCredentials(cookieID)
	if ctx.Err() != nil {
		unlock()
		return 0, 0, emptyResult, ctx.Err()
	}
	// latest、err 保存最新账号平台视图及错误。
	latest, err := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
	if err != nil || latest == nil || latest.UserID != userID || !s.runtime.CredentialAvailable(latest) {
		unlock()
		if err == nil {
			err = errors.New("账号凭证已变化")
		}
		return 0, 0, emptyResult, err
	}
	unlock()
	// fetchResult、discoveryErr 保存锁外订单发现结果及错误。
	fetchResult, discoveryErr := s.runtime.FetchSoldOrders(ctx, latest)
	if discoveryErr == nil {
		// 联系人补缓存可能访问平台，必须在凭证锁外完成；下方重新复核凭证及发现代次后才可写订单。
		discoveryErr = s.prepareSoldChats(ctx, cookieID, fetchResult.Orders)
	}
	unlock = s.repository.LockCredentials(cookieID)
	if ctx.Err() != nil {
		unlock()
		return 0, 0, emptyResult, ctx.Err()
	}
	if !s.discoveryCurrent(cookieID, generation) {
		unlock()
		return 0, 0, emptyResult, errors.New("该账号已有更新的订单同步，本次旧结果未写入")
	}
	// latestAfterFetch、reloadErr 保存发现完成后的凭证视图及重读错误。
	latestAfterFetch, reloadErr := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
	// credentialChanged 表示发现期间凭证快照是否发生变化。
	credentialChanged := reloadErr != nil || latestAfterFetch == nil || latestAfterFetch.UserID != userID || latestAfterFetch.Value != latest.Value || latestAfterFetch.MetadataJSON != latest.MetadataJSON
	if credentialChanged {
		unlock()
		return 0, 0, emptyResult, ErrRefreshCredentialChanged
	}
	// persistErr 保存已通过代次和凭证复核的响应 Cookie 写入错误；旧会话错误不得影响当前凭证。
	_, _, _, persistErr := s.runtime.PersistCookieSession(ctx, latest, fetchResult.CookieUpdate)
	if persistErr != nil {
		discoveryErr = errors.Join(discoveryErr, fmt.Errorf("保存订单列表响应 Cookie Jar: %w", persistErr))
	}
	// result 保存当前账号发现阶段的汇总结果。
	result := emptyResult
	if discoveryErr != nil {
		unlock()
	}
	if s.runtime.IsSessionExpired(discoveryErr) {
		result.SessionExpired = true
		s.runtime.RecoverExpiredSession(ctx, cookieID, discoveryErr)
	}
	if discoveryErr != nil {
		return 0, 0, result, discoveryErr
	}
	// discovered、updated、result、remoteOrderIDs、writeErr 保存已提交统计、逐单明细、完整远端集合和写入失败。
	discovered, updated, result, remoteOrderIDs, writeErr := s.persistSoldSnapshot(ctx, userID, cookieID, fetchResult.SellerID, fetchResult.Orders)
	if writeErr == nil {
		// deleted、deleteErr 保存完整快照成功处理后的缺失清理结果，有冲突或写入失败时不清理。
		deleted, deleteErr := s.repository.SoftDeleteMissingOrders(ctx, cookieID, remoteOrderIDs)
		result.SoftDeleted = deleted
		if deleteErr != nil {
			writeErr = fmt.Errorf("标记缺失订单失败: %w", deleteErr)
		}
	}
	unlock()
	if fetchResult.CookieUpdate.Changed && fetchResult.CookieUpdate.Value != "" {
		s.runtime.UpdateRunningCookie(ctx, cookieID, fetchResult.CookieUpdate.Value)
	}
	return discovered, updated, result, writeErr
}

// refreshDetailChunk 刷新一个账号详情分片并提交单事务结果。
func (s *RefreshService) refreshDetailChunk(ctx context.Context, userID int64, cookieID string, targets []refreshTarget) (int, int, int, []RefreshOrderResult, bool) {
	// results 保存当前详情分片结果。
	results := make([]RefreshOrderResult, 0, len(targets))
	// unlock 保存账号凭证锁释放函数。
	unlock := s.repository.LockCredentials(cookieID)
	// latest、err 保存详情请求前的平台视图及错误。
	latest, err := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
	if err != nil || latest == nil || latest.UserID != userID || !s.runtime.CredentialAvailable(latest) {
		unlock()
		// target 是凭证失效时需要返回失败的详情目标。
		for _, target := range targets {
			results = append(results, RefreshOrderResult{CookieID: cookieID, OrderID: target.OrderID, Success: false, Message: "账号凭证已变化"})
		}
		return 0, 0, len(targets), results, false
	}
	unlock()
	// detailCtx、cancel 限制本次详情分片外部请求时间。
	detailCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	// pendingWrites 保存成功获取、等待事务写入的订单详情。
	pendingWrites := make([]refreshWrite, 0, len(targets))
	// lastUpdate 保存详情分片最后一次平台响应的 Cookie 更新，即使该次详情请求失败也要尝试提交会话。
	lastUpdate := RefreshCookieUpdate{}
	// sessionErr 保存平台会话过期错误。
	var sessionErr error
	// target 是当前详情请求目标。
	for _, target := range targets {
		// fetchResult、fetchErr 保存当前订单详情结果及错误。
		fetchResult, fetchErr := s.runtime.FetchOrderDetail(detailCtx, latest, target.OrderID)
		if fetchResult.CookieUpdate.Changed || fetchResult.CookieUpdate.Handled {
			lastUpdate = fetchResult.CookieUpdate
		}
		if fetchErr != nil || fetchResult.Detail == nil {
			// message 保存当前详情失败的可读原因。
			message := "订单详情接口未返回结果"
			if fetchErr != nil {
				message = fetchErr.Error()
			}
			results = append(results, RefreshOrderResult{CookieID: cookieID, OrderID: target.OrderID, Success: false, Message: message})
			if s.runtime.IsSessionExpired(fetchErr) {
				sessionErr = fetchErr
				break
			}
			continue
		}
		// newStatus 保存远端详情归一化后的状态。
		newStatus := NormalizeOrderStatus(fetchResult.Detail.OrderStatus)
		if !ValidEditableOrderStatus(newStatus) {
			newStatus = target.CurrentStatus
		}
		pendingWrites = append(pendingWrites, refreshWrite{OrderID: target.OrderID, CurrentStatus: target.CurrentStatus, NewStatus: newStatus, Options: UpsertOptions{CookieID: cookieID, OrderStatus: newStatus, SpecName: fetchResult.Detail.SpecName, SpecValue: fetchResult.Detail.SpecValue, Quantity: fetchResult.Detail.Quantity, Amount: fetchResult.Detail.Amount}, CookieUpdate: fetchResult.CookieUpdate})
	}
	cancel()
	unlock = s.repository.LockCredentials(cookieID)
	// latestAfterDetails、reloadErr 保存详情返回后的凭证复核，必须先于任何订单写入。
	latestAfterDetails, reloadErr := s.repository.LoadCookiePlatformDetail(ctx, cookieID)
	// credentialChanged 表示返回详情属于旧会话，不能写入当前账号状态。
	credentialChanged := reloadErr != nil || latestAfterDetails == nil || latestAfterDetails.UserID != userID || latestAfterDetails.Value != latest.Value || latestAfterDetails.MetadataJSON != latest.MetadataJSON
	if credentialChanged {
		unlock()
		return 0, 0, len(targets), []RefreshOrderResult{{CookieID: cookieID, Stage: "detail", Error: "订单详情完成后账号凭证无法复核，未写入旧结果"}}, false
	}
	// batchRows 保存详情分片等待写入的订单记录，凭证锁保持至本地提交完成。
	batchRows := make([]RefreshOrderWrite, 0, len(pendingWrites))
	// write 是当前详情分片待批量写入的订单详情。
	for _, write := range pendingWrites {
		batchRows = append(batchRows, RefreshOrderWrite{OrderID: write.OrderID, Options: write.Options})
	}
	// batchWriteErr 保存详情分片事务批量 CAS 的提交错误，任一行失败时整批不计成功。
	batchWriteErr := s.repository.BatchUpsertOrders(ctx, batchRows)
	// updated、noChange、failed 保存详情分片统计。
	updated, noChange, failed := 0, 0, len(results)
	// write 是当前统计对应的订单详情。
	for _, write := range pendingWrites {
		if batchWriteErr != nil {
			failed++
			results = append(results, RefreshOrderResult{CookieID: cookieID, OrderID: write.OrderID, Success: false, Message: "批量更新数据库失败"})
			continue
		}
		// changed 表示订单状态是否发生变化。
		changed := write.NewStatus != "" && write.NewStatus != write.CurrentStatus
		if changed {
			updated++
		} else {
			noChange++
		}
		results = append(results, RefreshOrderResult{CookieID: cookieID, OrderID: write.OrderID, Success: true, OldStatus: write.CurrentStatus, NewStatus: write.NewStatus})
	}
	// valueChanged、persistErr 保存凭证锁内的会话提交状态，运行时更新必须在释放锁后进行。
	_, valueChanged, _, persistErr := s.runtime.PersistCookieSession(ctx, latest, lastUpdate)
	if persistErr != nil {
		failed++
		results = append(results, RefreshOrderResult{CookieID: cookieID, Stage: "persist_cookie", Success: false, Message: persistErr.Error()})
	}
	unlock()
	if persistErr == nil && valueChanged && lastUpdate.Value != "" {
		s.runtime.UpdateRunningCookie(ctx, cookieID, lastUpdate.Value)
	}
	if sessionErr != nil {
		s.runtime.RecoverExpiredSession(ctx, cookieID, sessionErr)
	}
	return updated, noChange, failed, results, sessionErr != nil
}
