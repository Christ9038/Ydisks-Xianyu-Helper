// Package mtopsim 提供带状态的闲鱼 MTOP H5 网关模拟器，仅供测试使用。
//
// 与逐接口返回固定 JSON 的测试替身不同，模拟器在内存中维护账号、商品、订单三类平台事实，
// 并按真实网关的顺序执行校验：Cookie 账号 → 风控 → Session → 签名 Token → 签名 → 业务状态机。
// 被测代码使用真实的 mtop.ClientImpl 发出请求，因此签名、Cookie 轮换、Token 重试、
// 错误分类和业务状态迁移都走生产路径，只有网络传输被替换为进程内的 http.RoundTripper。
//
// 模拟器不访问网络、不读取真实凭证；全部 Cookie、Token 和订单号都是确定性生成的测试数据。
// 生产代码不得导入本包。
package mtopsim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

// 平台返回标记。文本取自项目记录的真实平台响应，模拟器只负责在对应条件下返回它们。
const (
	// RetSuccess 是平台业务成功标记。
	RetSuccess = "SUCCESS::调用成功"
	// RetTokenExpired 是签名 Token 过期标记；平台原文即为拼写错误的 EXOIRED。
	RetTokenExpired = "FAIL_SYS_TOKEN_EXOIRED::令牌过期"
	// RetTokenEmpty 是请求未携带签名 Token 时的标记。
	RetTokenEmpty = "FAIL_SYS_TOKEN_EMPTY::令牌为空"
	// RetSessionExpired 是登录 Session 失效标记，必须重新登录才能恢复。
	RetSessionExpired = "FAIL_SYS_SESSION_EXPIRED::Session过期"
	// RetRiskControl 是触发风控后的标记，任何继续请求都会延长风控。
	RetRiskControl = "RGV587_ERROR::SM::哎哟喂,被挤爆啦,请稍后重试"
	// RetIllegalSign 是签名与请求内容不一致时的标记。
	RetIllegalSign = "FAIL_SYS_ILLEGAL_ACCESS::非法请求"
	// RetAPINotFound 是请求了模拟器未实现接口时的标记，用于暴露未登记的新 MTOP 调用。
	RetAPINotFound = "FAIL_SYS_API_NOT_FOUNDED::请求API不存在"
	// RetSensitiveWord 是确认发货文本命中平台敏感词审核的标记（issue 113）。
	RetSensitiveWord = "HIT_SENSITIVE_WORD::命中敏感字符"
	// RetAlreadyDelivered 是对已发货订单重复确认发货的标记；平台原文不带 FAIL_BIZ 前缀。
	RetAlreadyDelivered = "ORDER_ALREADY_DELIVERY::已发货成功，请刷新页面~"
	// RetGrouponSyncLag 是砍价团购实例尚未同步时的暂时性标记，稍后重试即可成功。
	RetGrouponSyncLag = "GROUPON_INSTANCE_QUERY_ERROR::团购查询失败"
	// RetCannotModifyFee 是订单暂时无法改价的标记。
	RetCannotModifyFee = "FAIL_BIZ_CANNOT_MODIFY_FEE::暂无法修改价格，请稍后重试"
	// RetPolishAgain 是同一商品当天重复擦亮的标记。
	RetPolishAgain = "FAIL_BIZ_IDLEITEM_POLISH_AGAIN::宝贝已经擦亮过了"
	// RetAlreadyRated 是重复评价买家的标记。
	RetAlreadyRated = "FAIL_BIZ_BAD_REQUEST::您已经评价过了"
	// RetRateNotAllowed 是订单尚未完成、不能评价的标记。
	RetRateNotAllowed = "FAIL_BIZ_BAD_REQUEST::当前订单不能评价"
	// RetOrderNotFound 是订单不存在或不属于当前卖家的标记。
	RetOrderNotFound = "FAIL_BIZ_ORDER_NOT_EXIST::订单不存在"
	// RetOrderStatusInvalid 是订单当前状态不允许该操作的标记。
	RetOrderStatusInvalid = "FAIL_BIZ_ORDER_STATUS_ERROR::订单状态不支持该操作"
	// RetGrouponNotFormed 是砍价订单尚未刀成、不能确认发货的标记。
	RetGrouponNotFormed = "FAIL_BIZ_GROUPON_NOT_FORMED::订单尚未成团，暂不能发货"
	// RetGrouponNotPending 是订单不处于待刀成阶段、不能免拼的标记。
	RetGrouponNotPending = "FAIL_BIZ_GROUPON_STATUS_ERROR::当前订单不支持免拼"
	// RetBadRequest 是请求参数缺失或格式错误的标记。
	RetBadRequest = "FAIL_BIZ_PARAM_ERROR::参数错误"
)

// 模拟器实现的 MTOP 接口名，与生产客户端请求路径中的接口名一一对应。
const (
	// APIToken 换取 WebSocket 注册用 accessToken。
	APIToken = "mtop.taobao.idlemessage.pc.login.token"
	// APILoginUser 检查当前登录态。
	APILoginUser = "mtop.taobao.idlemessage.pc.loginuser.get"
	// APIUserNav 读取当前账号资料。
	APIUserNav = "mtop.idle.web.user.page.nav"
	// APIItemList 分页读取在售商品。
	APIItemList = "mtop.idle.web.xyh.item.list"
	// APIOrderDetail 读取订单详情。
	APIOrderDetail = "mtop.idle.web.trade.order.detail"
	// APISoldOrders 分页读取已售订单。
	APISoldOrders = "mtop.taobao.idle.trade.merchant.sold.get"
	// APIConsign 确认虚拟商品发货。
	APIConsign = "mtop.taobao.idle.logistic.consign.dummy"
	// APIFreeShipping 对砍价订单免拼。
	APIFreeShipping = "mtop.idle.groupon.activity.seller.freeshipping"
	// APIAdjustPrice 修改待付款订单价格。
	APIAdjustPrice = "mtop.taobao.idle.trade.user.adjust.price"
	// APIRateList 读取待评价订单。
	APIRateList = "mtop.taobao.idle.merchant.rate.list"
	// APIRateCreate 评价买家。
	APIRateCreate = "mtop.taobao.idle.rate.create"
	// APIPolish 擦亮商品（主接口）。
	APIPolish = "mtop.taobao.idle.item.polish"
	// APIPolishBackup 擦亮商品（备用接口）。
	APIPolishBackup = "mtop.idle.item.polish"
	// APIChatUser 读取会话对方资料。
	APIChatUser = "mtop.taobao.idlemessage.pc.user.query"
	// APIChatItemSearch 在会话中查询商品。
	APIChatItemSearch = "mtop.taobao.idlemessage.pc.tool.item.search"
	// APICategoryRecommend 推荐发布类目。
	APICategoryRecommend = "mtop.taobao.idle.kgraph.property.recommend"
	// APIPublish 发布商品。
	APIPublish = "mtop.idle.pc.idleitem.publish"
	// APIUpload 是图片上传端点的记录名；它不是 MTOP 接口，没有签名校验。
	APIUpload = "upload.api"
)

// uploadHost 是图片上传服务的主机名，与 MTOP 网关不同。
const uploadHost = "stream-upload.goofish.com"

// Fault 描述一次由测试注入的网关异常，用于模拟平台故障而不改动模拟器状态机。
// 注入的异常按先进先出顺序消费，每条只作用于一个请求。
type Fault struct {
	// Ret 非空时以 HTTP 200 返回该平台标记，业务状态机不会执行。
	Ret []string
	// Data 是与 Ret 一起返回的 data 字段，例如风控响应携带的验证地址。
	Data map[string]any
	// HTTPStatus 非零时使用该 HTTP 状态码；为零表示 200。
	HTTPStatus int
	// Body 非空时原样作为响应正文返回，用于模拟被截断或非 JSON 的响应。
	Body string
	// Err 非空时作为网络层错误返回，请求视为未到达平台。
	Err error
	// AfterEffect 为真时先执行真实业务状态机再返回异常，用于模拟“平台已执行但响应丢失”的结果未知场景。
	AfterEffect bool
}

// Call 记录一次到达模拟器的请求，供测试断言调用顺序、次数和请求频率。
type Call struct {
	// API 是请求的接口名。
	API string
	// Version 是请求路径中的接口版本。
	Version string
	// UserID 是请求 Cookie 中的账号标识；缺失时为空。
	UserID string
	// Data 是解码后的请求 data 字段副本。
	Data map[string]any
	// Ret 是模拟器返回的平台标记。
	Ret []string
	// At 是模拟器时钟下请求到达的时刻。
	At time.Time
}

// Gateway 是进程内的闲鱼平台模拟器，实现 http.RoundTripper。
//
// 并发：全部状态由 mu 保护，RoundTrip 与状态读写方法可并发调用。模拟器不启动 goroutine，
// 也不在持锁期间执行任何外部 I/O，因此不需要关闭。
type Gateway struct {
	// mu 保护以下全部字段。
	mu sync.Mutex
	// now 是模拟器时钟，决定 Token 过期、擦亮日期和订单时间。
	now func() time.Time
	// accounts 以账号标识 unb 为键保存平台账号。
	accounts map[string]*account
	// items 以商品 ID 为键保存商品。
	items map[string]*Item
	// itemOrder 保存商品的发布顺序，使分页结果稳定。
	itemOrder []string
	// orders 以订单号为键保存订单。
	orders map[string]*Order
	// orderSeq 保存订单的创建顺序，使已售订单分页结果稳定。
	orderSeq []string
	// faults 以接口名为键保存待消费的注入异常队列。
	faults map[string][]Fault
	// calls 按到达顺序保存全部请求记录。
	calls []Call
	// sequence 是生成 Token、Session、商品号、订单号和图片地址的自增序号。
	sequence int64
	// sensitiveWords 是确认发货文本的敏感词表。
	sensitiveWords []string
	// rotateOnBusinessAPI 为真时业务接口在 Token 过期响应中直接下发新 Token；为假时只有 Token 接口会轮换。
	rotateOnBusinessAPI bool
	// tokenTTL 是新签发签名 Token 的有效期。
	tokenTTL time.Duration
}

// New 创建空的平台模拟器。默认行为与真实平台一致：业务接口在 Token 过期时通过 Set-Cookie 下发新 Token。
func New() *Gateway {
	return &Gateway{
		now:                 time.Now,
		accounts:            map[string]*account{},
		items:               map[string]*Item{},
		orders:              map[string]*Order{},
		faults:              map[string][]Fault{},
		sensitiveWords:      []string{"违禁"},
		rotateOnBusinessAPI: true,
		tokenTTL:            time.Hour,
	}
}

// Client 返回把全部请求发往本模拟器的真实 MTOP 客户端。
// 注入了自定义传输的客户端不经过账号级节流器，因此测试不会因限速而变慢；请求频率通过 Calls 断言。
func (g *Gateway) Client() *mtop.ClientImpl {
	// client 是生产构造函数返回的客户端，只替换网络传输和日志输出。
	client := mtop.NewClient()
	client.HTTPClient = &http.Client{Transport: g}
	client.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return client
}

// SetClock 替换模拟器时钟，用于让 Token 过期或跨天而不真实等待。
func (g *Gateway) SetClock(now func() time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.now = now
}

// SetTokenRotationOnBusinessAPI 控制业务接口是否在 Token 过期响应中下发新 Token。
// 关闭后客户端必须主动调用 Token 接口才能恢复，用于覆盖该恢复路径。
func (g *Gateway) SetTokenRotationOnBusinessAPI(enabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rotateOnBusinessAPI = enabled
}

// SetSensitiveWords 替换确认发货文本的敏感词表。
func (g *Gateway) SetSensitiveWords(words ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sensitiveWords = append([]string(nil), words...)
}

// InjectFault 为指定接口追加注入异常；api 取本包的 API 常量。
func (g *Gateway) InjectFault(api string, faults ...Fault) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.faults[api] = append(g.faults[api], faults...)
}

// Calls 返回全部请求记录的副本，按到达顺序排列。
func (g *Gateway) Calls() []Call {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Call(nil), g.calls...)
}

// CallCount 返回指定接口收到的请求次数；不传接口名时返回全部请求次数。
func (g *Gateway) CallCount(apis ...string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(apis) == 0 {
		return len(g.calls)
	}
	// count 累计命中任一指定接口的请求数。
	count := 0
	// call 是已记录的单次请求。
	for _, call := range g.calls {
		// api 是调用方关心的接口名之一。
		for _, api := range apis {
			if call.API == api {
				count++
			}
		}
	}
	return count
}

// CallSequence 返回请求记录中的接口名序列，便于断言调用顺序。
func (g *Gateway) CallSequence() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	// sequence 按到达顺序保存接口名。
	sequence := make([]string, 0, len(g.calls))
	// call 是已记录的单次请求。
	for _, call := range g.calls {
		sequence = append(sequence, call.API)
	}
	return sequence
}

// ResetCalls 清空请求记录，便于分阶段断言。
func (g *Gateway) ResetCalls() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = nil
}

// nextSequence 返回下一个自增序号；调用方必须持有 mu。
func (g *Gateway) nextSequence() int64 {
	g.sequence++
	return g.sequence
}

// RoundTrip 模拟平台处理一次 HTTP 请求。req 的 Context 已取消时与真实传输一样返回取消错误。
func (g *Gateway) RoundTrip(req *http.Request) (*http.Response, error) {
	// ctxErr 是请求发出前调用方已取消或超时的错误。
	if ctxErr := req.Context().Err(); ctxErr != nil {
		return nil, ctxErr
	}
	// rawBody 是完整请求正文；模拟器内的请求体很小，直接读入内存。
	var rawBody []byte
	if req.Body != nil {
		// readErr 是读取请求正文的错误。
		var readErr error
		rawBody, readErr = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if req.URL.Hostname() == uploadHost {
		return g.handleUpload(req, rawBody)
	}
	return g.handleMTop(req, rawBody)
}

// handleMTop 按真实网关的校验顺序处理一次 MTOP 请求；调用方必须持有 mu。
func (g *Gateway) handleMTop(req *http.Request, rawBody []byte) (*http.Response, error) {
	// api、version 取自请求路径 /h5/<接口名>/<版本>/。
	api, version := apiFromPath(req.URL.Path)
	// cookieHeader 是请求携带的完整 Cookie 头。
	cookieHeader := req.Header.Get("Cookie")
	// cookies 是解析后的 Cookie 键值。
	cookies := protocol.TransCookies(cookieHeader)
	// dataValue 是请求体中参与签名的 data 原文；data 是其解码结果。
	dataValue, data := decodeRequestData(rawBody)
	// call 是本次请求的记录，返回前补全平台标记。
	call := Call{API: api, Version: version, UserID: cookies["unb"], Data: data, At: g.now()}
	// respond 记录请求并构造响应；setCookies 是需要下发的 Set-Cookie 头。
	respond := func(status int, ret []string, payload any, setCookies []string) (*http.Response, error) {
		call.Ret = append([]string(nil), ret...)
		g.calls = append(g.calls, call)
		return jsonResponse(req, status, api, version, ret, payload, setCookies), nil
	}

	// fault、hasFault 是该接口队首的注入异常及其是否存在。
	fault, hasFault := g.popFault(api)
	if hasFault && !fault.AfterEffect {
		return g.respondFault(req, &call, api, version, fault)
	}

	// acct 是 Cookie 中 unb 对应的平台账号；未知账号等同于未登录。
	acct := g.accounts[cookies["unb"]]
	if acct == nil {
		return respond(http.StatusOK, []string{RetSessionExpired}, map[string]any{}, nil)
	}
	if acct.riskLocked {
		return respond(http.StatusOK, []string{RetRiskControl}, map[string]any{"url": "https://www.goofish.com/punish?x5secdata=simulated"}, nil)
	}
	if cookies["cookie2"] != acct.sessionID {
		return respond(http.StatusOK, []string{RetSessionExpired}, map[string]any{}, nil)
	}
	// requestToken 是请求 Cookie 中 _m_h5_tk 的签名 Token 部分。
	requestToken := protocol.SignToken(cookieHeader)
	if requestToken == "" || requestToken != acct.token || !g.now().Before(acct.tokenExpireAt) {
		// ret 区分“未携带 Token”与“Token 已过期”，两者都要求客户端换用新 Token 重签。
		ret := []string{RetTokenExpired}
		if requestToken == "" {
			ret = []string{RetTokenEmpty}
		}
		// setCookies 只在允许轮换的接口上下发新 Token。
		var setCookies []string
		if g.rotateOnBusinessAPI || api == APIToken {
			g.rotateToken(acct)
			setCookies = acct.tokenSetCookies()
		}
		return respond(http.StatusOK, ret, map[string]any{}, setCookies)
	}
	// query 是请求 URL 的查询参数，签名、时间戳和 appKey 都在其中。
	query := req.URL.Query()
	if query.Get("appKey") != protocol.SignAppKey ||
		query.Get("sign") != protocol.GenerateSign(query.Get("t"), requestToken, dataValue) {
		return respond(http.StatusOK, []string{RetIllegalSign}, map[string]any{}, nil)
	}

	// handler 是接口对应的业务状态机；未实现的接口按平台“接口不存在”处理。
	handler := g.handlers()[api]
	if handler == nil {
		return respond(http.StatusOK, []string{RetAPINotFound}, map[string]any{}, nil)
	}
	// ret、payload 是业务状态机给出的平台标记和 data 字段。
	ret, payload := handler(acct, data)
	if hasFault {
		// 业务已执行，但响应被注入的异常替换：调用方只能把结果视为未知。
		return g.respondFault(req, &call, api, version, fault)
	}
	return respond(http.StatusOK, ret, payload, nil)
}

// popFault 取出指定接口队首的注入异常；调用方必须持有 mu。
func (g *Gateway) popFault(api string) (Fault, bool) {
	// queue 是该接口尚未消费的注入异常。
	queue := g.faults[api]
	if len(queue) == 0 {
		return Fault{}, false
	}
	g.faults[api] = queue[1:]
	return queue[0], true
}

// respondFault 按注入异常构造响应或网络错误，并记录本次请求；调用方必须持有 mu。
func (g *Gateway) respondFault(req *http.Request, call *Call, api, version string, fault Fault) (*http.Response, error) {
	call.Ret = append([]string(nil), fault.Ret...)
	g.calls = append(g.calls, *call)
	if fault.Err != nil {
		return nil, fault.Err
	}
	// status 是注入异常使用的 HTTP 状态码，未指定时为 200。
	status := fault.HTTPStatus
	if status == 0 {
		status = http.StatusOK
	}
	if fault.Body != "" {
		return rawResponse(req, status, fault.Body, nil), nil
	}
	// payload 是随异常返回的 data 字段；未指定时为空对象。
	payload := fault.Data
	if payload == nil {
		payload = map[string]any{}
	}
	return jsonResponse(req, status, api, version, fault.Ret, payload, nil), nil
}

// handleUpload 模拟图片上传服务：只校验登录态，不校验 MTOP 签名；调用方必须持有 mu。
func (g *Gateway) handleUpload(req *http.Request, rawBody []byte) (*http.Response, error) {
	// cookies 是上传请求携带的 Cookie 键值。
	cookies := protocol.TransCookies(req.Header.Get("Cookie"))
	// call 是本次上传的记录；Data 只保存正文大小，不保存图片字节。
	call := Call{API: APIUpload, UserID: cookies["unb"], Data: map[string]any{"size": len(rawBody)}, At: g.now()}
	// fault、hasFault 是上传端点队首的注入异常及其是否存在。
	fault, hasFault := g.popFault(APIUpload)
	if hasFault {
		return g.respondFault(req, &call, APIUpload, "", fault)
	}
	// acct 是上传请求对应的平台账号。
	acct := g.accounts[cookies["unb"]]
	if acct == nil || cookies["cookie2"] != acct.sessionID {
		call.Ret = []string{RetSessionExpired}
		g.calls = append(g.calls, call)
		return rawResponse(req, http.StatusUnauthorized, `{"success":false,"message":"not login"}`, nil), nil
	}
	call.Ret = []string{RetSuccess}
	g.calls = append(g.calls, call)
	// imageURL 是确定性生成的图片地址，测试可据此断言发布和确认发货引用了上传结果。
	imageURL := fmt.Sprintf("https://img.alicdn.example/sim/%s/%d.png", acct.UserID, g.nextSequence())
	// body 是上传服务的成功响应；pix 为“宽x高”。
	body, _ := json.Marshal(map[string]any{"success": true, "object": map[string]any{"url": imageURL, "pix": "800x600"}})
	return rawResponse(req, http.StatusOK, string(body), nil), nil
}

// apiFromPath 从 /h5/<接口名>/<版本>/ 形式的路径中取出接口名和版本。
func apiFromPath(path string) (api, version string) {
	// parts 是去掉首尾斜杠后的路径段。
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "h5" {
		return parts[1], parts[2]
	}
	return strings.Trim(path, "/"), ""
}

// decodeRequestData 解析表单正文中的 data 字段，返回参与签名的原文和解码后的对象。
// 数字以 json.Number 保留，避免 19 位订单号在模拟器内部丢失精度。
func decodeRequestData(rawBody []byte) (string, map[string]any) {
	// form、parseErr 是表单解析结果；解析失败时按空 data 处理，由签名校验拒绝请求。
	form, parseErr := url.ParseQuery(string(rawBody))
	if parseErr != nil {
		return "", map[string]any{}
	}
	// dataValue 是 data 字段原文。
	dataValue := form.Get("data")
	// data 是解码后的请求对象。
	data := map[string]any{}
	// decoder 以 json.Number 解码数字。
	decoder := json.NewDecoder(strings.NewReader(dataValue))
	decoder.UseNumber()
	_ = decoder.Decode(&data)
	return dataValue, data
}

// jsonResponse 构造 MTOP 标准信封响应：api、v、ret、data。
func jsonResponse(req *http.Request, status int, api, version string, ret []string, payload any, setCookies []string) *http.Response {
	if payload == nil {
		payload = map[string]any{}
	}
	// body 是序列化后的响应信封。
	body, _ := json.Marshal(map[string]any{"api": api, "v": version, "ret": ret, "data": payload})
	return rawResponse(req, status, string(body), setCookies)
}

// rawResponse 构造带任意正文的 HTTP 响应。
func rawResponse(req *http.Request, status int, body string, setCookies []string) *http.Response {
	// header 是响应头；Set-Cookie 允许多值。
	header := http.Header{"Content-Type": []string{"application/json;charset=UTF-8"}}
	// setCookie 是需要下发的单条 Set-Cookie。
	for _, setCookie := range setCookies {
		header.Add("Set-Cookie", setCookie)
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader([]byte(body))),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
