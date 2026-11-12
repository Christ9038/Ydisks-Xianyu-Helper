package mtopsim

import (
	"fmt"
	"time"
)

// OrderStatus 是模拟器内部的订单阶段；对外按各接口的协议转换成状态码或中文状态。
type OrderStatus string

const (
	// OrderWaitPay 表示买家已拍下、尚未付款。
	OrderWaitPay OrderStatus = "wait_pay"
	// OrderPendingShip 表示买家已付款、等待卖家发货。
	OrderPendingShip OrderStatus = "pending_ship"
	// OrderShipped 表示卖家已确认发货。
	OrderShipped OrderStatus = "shipped"
	// OrderCompleted 表示买家已确认收货。
	OrderCompleted OrderStatus = "completed"
	// OrderClosed 表示交易已关闭。
	OrderClosed OrderStatus = "closed"
)

// detailCode 返回订单详情接口 utArgs.orderStatus 使用的数字状态码。
func (s OrderStatus) detailCode() string {
	switch s {
	case OrderWaitPay:
		return "1"
	case OrderPendingShip:
		return "2"
	case OrderShipped:
		return "3"
	case OrderCompleted:
		return "4"
	default:
		return "6"
	}
}

// soldLabel 返回已售订单列表接口 commonData.orderStatus 使用的中文状态。
func (s OrderStatus) soldLabel() string {
	switch s {
	case OrderWaitPay:
		return "待付款"
	case OrderPendingShip:
		return "待发货"
	case OrderShipped:
		return "已发货"
	case OrderCompleted:
		return "交易成功"
	default:
		return "交易关闭"
	}
}

// Spec 是订单或商品的一组规格维度，例如“颜色=红色”。
type Spec struct {
	// Name 是规格名称。
	Name string
	// Value 是规格取值。
	Value string
}

// Item 是平台上的一件在售商品。测试通过 Gateway.AddItem 创建，通过 Gateway.Item 读取副本。
type Item struct {
	// ID 是平台商品 ID，纯数字字符串。
	ID string
	// SellerID 是发布该商品的卖家账号标识。
	SellerID string
	// Title 是商品标题。
	Title string
	// Price 是以元为单位的展示价格，例如 "9.90"。
	Price string
	// MultiSpec 表示商品是否为多规格商品。
	MultiSpec bool
	// OnSale 表示商品是否在售；下架商品不出现在商品列表中。
	OnSale bool
	// PolishedOn 是最近一次擦亮的日期（YYYY-MM-DD），用于“一天只能擦亮一次”。
	PolishedOn string
	// PublishData 是通过发布接口创建商品时的请求数据副本，手工添加的商品为空。
	PublishData map[string]any
}

// Order 是平台上的一笔交易订单。测试通过 Gateway 的订单方法推进阶段，通过 Gateway.Order 读取副本。
type Order struct {
	// ID 是平台订单号，19 位纯数字字符串。
	ID string
	// SellerID 是卖家账号标识。
	SellerID string
	// BuyerID 是买家账号标识，纯数字字符串。
	BuyerID string
	// BuyerNick 是买家昵称。
	BuyerNick string
	// ItemID 是成交商品 ID。
	ItemID string
	// ChatID 是买卖双方的会话 ID。
	ChatID string
	// Quantity 是购买件数。
	Quantity int
	// Amount 是以元为单位的成交总金额，例如 "19.80"。
	Amount string
	// Specs 是订单的完整规格组合；无规格订单为空。
	Specs []Spec
	// Status 是订单当前阶段。
	Status OrderStatus
	// Bargain 表示订单是否为二人小刀（砍价）订单。
	Bargain bool
	// BargainFormed 表示砍价订单是否已刀成（自然成团或卖家免拼）；未刀成前不能确认发货。
	BargainFormed bool
	// FreeShipped 表示卖家是否对该订单执行过免拼。
	FreeShipped bool
	// GrouponSyncLag 是免拼接口还需返回多少次“团购实例未同步”才会成功，用于模拟平台同步延迟。
	GrouponSyncLag int
	// AdjustBusy 是改价接口还需返回多少次“暂无法修改价格”才会成功。
	AdjustBusy int
	// ConsignCount 是确认发货接口成功执行的次数，正确的流程中恒为 0 或 1。
	ConsignCount int
	// ConsignText 是确认发货时提交的文本发货凭证。
	ConsignText string
	// ConsignPictures 是确认发货时提交的图片发货凭证地址。
	ConsignPictures []string
	// SellerRated 表示卖家是否已评价买家。
	SellerRated bool
	// SellerFeedback 是卖家评价内容。
	SellerFeedback string
	// BuyerRated 表示买家是否已评价卖家。
	BuyerRated bool
	// CreatedAt 是下单时间。
	CreatedAt time.Time
}

// account 是模拟器内部的平台账号，保存登录态和签名 Token。
type account struct {
	// UserID 是账号标识，对应 Cookie 中的 unb。
	UserID string
	// Nickname 是账号昵称。
	Nickname string
	// sessionID 是当前登录 Session，对应 Cookie 中的 cookie2；重新登录后变化。
	sessionID string
	// token 是当前有效的签名 Token，对应 Cookie _m_h5_tk 下划线前的部分。
	token string
	// tokenExpireAt 是签名 Token 的过期时刻。
	tokenExpireAt time.Time
	// riskLocked 表示账号处于风控状态，全部接口都返回风控标记。
	riskLocked bool
	// accessTokens 是 Token 接口已签发的 accessToken 数量。
	accessTokens int
}

// cookieString 返回该账号当前可用的完整 Cookie。
func (a *account) cookieString() string {
	return fmt.Sprintf("unb=%s; cookie2=%s; _m_h5_tk=%s_%d; _m_h5_tk_enc=enc%s",
		a.UserID, a.sessionID, a.token, a.tokenExpireAt.UnixMilli(), a.token)
}

// tokenSetCookies 返回下发当前签名 Token 的 Set-Cookie 头，作用域与真实平台一致。
func (a *account) tokenSetCookies() []string {
	return []string{
		fmt.Sprintf("_m_h5_tk=%s_%d; Path=/; Domain=goofish.com; Max-Age=604800", a.token, a.tokenExpireAt.UnixMilli()),
		fmt.Sprintf("_m_h5_tk_enc=enc%s; Path=/; Domain=goofish.com; Max-Age=604800", a.token),
	}
}

// rotateToken 为账号签发新的签名 Token；调用方必须持有 mu。
func (g *Gateway) rotateToken(acct *account) {
	acct.token = fmt.Sprintf("%032x", g.nextSequence())
	acct.tokenExpireAt = g.now().Add(g.tokenTTL)
}

// AddAccount 注册一个已登录的平台账号并返回其初始 Cookie。userID 必须是纯数字字符串。
func (g *Gateway) AddAccount(userID, nickname string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	// acct 是新注册的账号；Session 与 Token 立即生效。
	acct := &account{UserID: userID, Nickname: nickname}
	acct.sessionID = fmt.Sprintf("sess%020d", g.nextSequence())
	g.rotateToken(acct)
	g.accounts[userID] = acct
	return acct.cookieString()
}

// Cookie 返回账号当前有效的完整 Cookie；账号不存在时返回空字符串。
func (g *Gateway) Cookie(userID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	// acct、exists 是账号及其是否已注册。
	acct, exists := g.accounts[userID]
	if !exists {
		return ""
	}
	return acct.cookieString()
}

// ExpireToken 让账号当前签名 Token 立即过期；Session 仍然有效，客户端应能自动恢复。
func (g *Gateway) ExpireToken(userID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// acct、exists 是账号及其是否已注册。
	if acct, exists := g.accounts[userID]; exists {
		acct.tokenExpireAt = g.now().Add(-time.Second)
	}
}

// ExpireSession 让账号登录 Session 失效；此前发出的全部 Cookie 都不再可用，只能通过 Relogin 恢复。
func (g *Gateway) ExpireSession(userID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// acct、exists 是账号及其是否已注册。
	if acct, exists := g.accounts[userID]; exists {
		acct.sessionID = fmt.Sprintf("sess%020d", g.nextSequence())
	}
}

// Relogin 模拟账号重新扫码登录，返回带新 Session 和新 Token 的 Cookie。
func (g *Gateway) Relogin(userID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	// acct、exists 是账号及其是否已注册。
	acct, exists := g.accounts[userID]
	if !exists {
		return ""
	}
	acct.sessionID = fmt.Sprintf("sess%020d", g.nextSequence())
	g.rotateToken(acct)
	return acct.cookieString()
}

// SetRiskControl 设置或解除账号的风控状态。风控期间全部接口返回风控标记且不执行业务。
func (g *Gateway) SetRiskControl(userID string, locked bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// acct、exists 是账号及其是否已注册。
	if acct, exists := g.accounts[userID]; exists {
		acct.riskLocked = locked
	}
}

// AddItem 为卖家添加一件在售商品并返回商品 ID。
func (g *Gateway) AddItem(sellerID, title, price string, multiSpec bool) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.addItemLocked(&Item{SellerID: sellerID, Title: title, Price: price, MultiSpec: multiSpec, OnSale: true})
}

// addItemLocked 分配商品 ID 并保存商品；调用方必须持有 mu。
func (g *Gateway) addItemLocked(item *Item) string {
	item.ID = fmt.Sprintf("9%011d", g.nextSequence())
	g.items[item.ID] = item
	g.itemOrder = append(g.itemOrder, item.ID)
	return item.ID
}

// Item 返回商品的副本；exists 表示商品是否存在。
func (g *Gateway) Item(itemID string) (item Item, exists bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// stored、found 是模拟器内保存的商品及其是否存在。
	stored, found := g.items[itemID]
	if !found {
		return Item{}, false
	}
	return *stored, true
}

// Items 返回卖家全部商品的副本，按发布顺序排列。
func (g *Gateway) Items(sellerID string) []Item {
	g.mu.Lock()
	defer g.mu.Unlock()
	// items 保存属于该卖家的商品副本。
	var items []Item
	// itemID 是按发布顺序遍历的商品 ID。
	for _, itemID := range g.itemOrder {
		if g.items[itemID].SellerID == sellerID {
			items = append(items, *g.items[itemID])
		}
	}
	return items
}

// OrderInput 是创建订单所需的买家侧事实。
type OrderInput struct {
	// ItemID 是被购买的商品 ID，必须已通过 AddItem 或发布接口创建。
	ItemID string
	// BuyerID 是买家账号标识，纯数字字符串。
	BuyerID string
	// BuyerNick 是买家昵称。
	BuyerNick string
	// Quantity 是购买件数；小于 1 时按 1 处理。
	Quantity int
	// Amount 是以元为单位的成交总金额。
	Amount string
	// Specs 是买家选择的完整规格组合。
	Specs []Spec
	// Bargain 表示订单是否为二人小刀订单。
	Bargain bool
}

// PlaceOrder 模拟买家拍下商品，返回处于待付款阶段的订单号。
func (g *Gateway) PlaceOrder(input OrderInput) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	// item、exists 是被购买的商品及其是否存在；不存在属于测试编写错误。
	item, exists := g.items[input.ItemID]
	if !exists {
		panic("mtopsim: PlaceOrder 引用了不存在的商品 " + input.ItemID)
	}
	// quantity 是归一化后的购买件数。
	quantity := input.Quantity
	if quantity < 1 {
		quantity = 1
	}
	// sequence 同时用于生成订单号和会话 ID，保证两者稳定且互不相同。
	sequence := g.nextSequence()
	// order 是新创建的待付款订单。订单号为 19 位数字，超过 float64 可精确表示的范围，
	// 用于暴露任何把订单号当作 JSON 数字处理的代码。
	order := &Order{
		ID:        fmt.Sprintf("45678901234%08d", sequence),
		SellerID:  item.SellerID,
		BuyerID:   input.BuyerID,
		BuyerNick: input.BuyerNick,
		ItemID:    input.ItemID,
		ChatID:    fmt.Sprintf("6290%08d", sequence),
		Quantity:  quantity,
		Amount:    input.Amount,
		Specs:     append([]Spec(nil), input.Specs...),
		Status:    OrderWaitPay,
		Bargain:   input.Bargain,
		CreatedAt: g.now(),
	}
	g.orders[order.ID] = order
	g.orderSeq = append(g.orderSeq, order.ID)
	return order.ID
}

// mustOrder 返回订单；订单不存在属于测试编写错误。调用方必须持有 mu。
func (g *Gateway) mustOrder(orderID string) *Order {
	// order、exists 是模拟器内保存的订单及其是否存在。
	order, exists := g.orders[orderID]
	if !exists {
		panic("mtopsim: 引用了不存在的订单 " + orderID)
	}
	return order
}

// PayOrder 模拟买家付款：普通订单进入待发货；砍价订单进入待发货但尚未刀成。
func (g *Gateway) PayOrder(orderID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustOrder(orderID).Status = OrderPendingShip
}

// FormBargain 模拟砍价订单自然刀成（第二位买家参与），之后才允许确认发货。
func (g *Gateway) FormBargain(orderID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustOrder(orderID).BargainFormed = true
}

// ConfirmReceipt 模拟买家确认收货，订单进入交易成功。
func (g *Gateway) ConfirmReceipt(orderID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustOrder(orderID).Status = OrderCompleted
}

// BuyerRate 模拟买家评价卖家。
func (g *Gateway) BuyerRate(orderID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustOrder(orderID).BuyerRated = true
}

// CloseOrder 模拟交易关闭（买家取消或退款成功）。
func (g *Gateway) CloseOrder(orderID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustOrder(orderID).Status = OrderClosed
}

// SetGrouponSyncLag 设置免拼接口在成功前返回“团购实例未同步”的次数。
func (g *Gateway) SetGrouponSyncLag(orderID string, times int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustOrder(orderID).GrouponSyncLag = times
}

// SetAdjustBusy 设置改价接口在成功前返回“暂无法修改价格”的次数。
func (g *Gateway) SetAdjustBusy(orderID string, times int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustOrder(orderID).AdjustBusy = times
}

// Order 返回订单的副本；订单不存在属于测试编写错误。
func (g *Gateway) Order(orderID string) Order {
	g.mu.Lock()
	defer g.mu.Unlock()
	// order 是订单副本，切片字段单独复制以免调用方修改模拟器状态。
	order := *g.mustOrder(orderID)
	order.Specs = append([]Spec(nil), order.Specs...)
	order.ConsignPictures = append([]string(nil), order.ConsignPictures...)
	return order
}
