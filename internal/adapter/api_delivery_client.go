package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/netguard"
)

// apiDeliveryResponseLimit 是 API 发货允许读取的最大响应体大小，避免远端响应耗尽服务端内存。
const apiDeliveryResponseLimit int64 = 1 << 20

// apiTestPreviewLimit 限制测试响应返回给浏览器的预览长度，避免把大响应或敏感内容扩散到前端。
const apiTestPreviewLimit = 4096

// apiDeliveryRetryLimit 是开启幂等重试后允许的总请求次数。
const apiDeliveryRetryLimit = 3

// apiDeliveryRetryGaps 是第 2、3 次 API 发货请求前的退避间隔；测试可临时替换它。
var apiDeliveryRetryGaps = []time.Duration{time.Second, 2 * time.Second}

// apiDeliveryClient 实现自动化中心要求的普通 API 卡发货能力。
type apiDeliveryClient struct {
	// store 读取模板变量所需的商品详情，不读取账号凭证明文。
	store *db.Store
	// logger 只记录请求元数据，不记录 URL 查询串、请求模板或响应内容。
	logger *slog.Logger
	// client 是共享的策略 HTTP 客户端；单次超时由请求 Context 控制，避免每个单位创建 Transport。
	client *http.Client
	// clientOnce 保证测试构造的零值客户端只初始化一次共享 Transport。
	clientOnce sync.Once
}

// newAPIDeliveryClient 创建 API 发货适配器。
func newAPIDeliveryClient(store *db.Store, logger *slog.Logger) automation.APICardFetcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &apiDeliveryClient{store: store, logger: logger, client: netguard.ConfiguredHTTPClient(0)}
}

// NewAPICardTester 创建卡密 API 测试适配器；它与自动发货共享出站策略但不会执行重试或写入库存。
func NewAPICardTester(store *db.Store, logger *slog.Logger) cardsapp.APIRequestTester {
	if logger == nil {
		logger = slog.Default()
	}
	return &apiDeliveryClient{store: store, logger: logger, client: netguard.ConfiguredHTTPClient(0)}
}

// Test 发送一次不带真实订单数据的 API 测试请求，并返回状态和响应结构诊断。
func (c *apiDeliveryClient) Test(ctx context.Context, input cardsapp.APIRequestTestInput) (cardsapp.APIRequestTestResult, error) {
	// config、err 分别是规范化后的临时测试配置及其输入校验错误。
	config, err := cardsapp.ParseAPIConfig(input.Config)
	if err != nil {
		return cardsapp.APIRequestTestResult{}, err
	}
	// variables 是测试环境稳定占位符，绝不使用真实订单、买家或账号数据。
	variables := map[string]string{
		"{order_id}": "test-order", "{item_id}": "test-item", "{buyer_id}": "test-buyer",
		"{chat_id}": "test-chat", "{account_id}": "test-account", "{cookie_id}": "test-account",
		"{spec_name}": "", "{spec_value}": "", "{quantity}": "1", "{order_quantity}": "1",
		"{amount}": "0", "{order_amount}": "0", "{item_detail}": "", "{trigger_type}": "test",
		"{timestamp}": time.Now().UTC().Format(time.RFC3339Nano), "{delivery_unit_index}": "1",
		"{delivery_total_count}": "1", "{idempotency_key}": "test-idempotency-key",
	}
	// headers、params 分别是已用测试变量替换的请求头和查询参数。
	headers := replaceAPIJSONMap(config.Headers, variables)
	// params 是已用测试变量替换的查询参数。
	params := replaceAPIJSONMap(config.Params, variables)
	// requestURL、err 分别是待请求 URL 和解析错误；错误文本不回显原始地址。
	requestURL, err := url.Parse(config.URL)
	if err != nil {
		return cardsapp.APIRequestTestResult{}, errors.New("API 地址无效")
	}
	if config.Method == http.MethodGet {
		// query 保存基础地址与模板参数合并后的查询字符串。
		query := requestURL.Query()
		// key、value 分别是查询字段名称和已替换的字段值。
		for key, value := range params {
			query.Set(key, apiJSONScalar(value))
		}
		requestURL.RawQuery = query.Encode()
	}
	// bodyValues 是已替换的正文模板；为空时沿用历史 Params 正文兼容规则。
	bodyValues := replaceAPIJSONMap(config.Body, variables)
	if len(bodyValues) == 0 {
		bodyValues = params
	}
	// body、err 分别是按 Content-Type 编码后的正文和编码错误。
	body, err := encodeAPIDeliveryBody(bodyValues, config.ContentType)
	if err != nil {
		return cardsapp.APIRequestTestResult{}, fmt.Errorf("编码 API 请求参数: %w", err)
	}
	// requestCtx、cancel 限制本次测试请求的最长耗时，并在函数返回时释放计时器。
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(config.Timeout)*time.Second)
	defer cancel()
	// requestBody 仅用于 POST 的可重复读取正文，GET 请求保持为空。
	var requestBody io.Reader
	if config.Method == http.MethodPost {
		requestBody = bytes.NewReader(body)
	}
	// request、err 分别是受上下文控制的远端请求和构造错误。
	request, err := http.NewRequestWithContext(requestCtx, config.Method, requestURL.String(), requestBody)
	if err != nil {
		return cardsapp.APIRequestTestResult{}, errors.New("API 请求地址无效")
	}
	// key、value 分别是请求头名称和经过模板替换的字段值。
	for key, value := range headers {
		request.Header.Set(key, apiJSONScalar(value))
	}
	if config.Method == http.MethodPost && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", config.ContentType)
	}
	// response、err 分别是远端响应和不含请求秘密的网络错误。
	response, err := c.httpClient().Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return cardsapp.APIRequestTestResult{}, ctx.Err()
		}
		return cardsapp.APIRequestTestResult{}, errors.New("API 测试请求失败")
	}
	defer response.Body.Close()
	// rawBody、err 分别是受 1 MiB 限制的响应内容和读取错误。
	rawBody, err := readAPIDeliveryBody(response.Body)
	if err != nil {
		return cardsapp.APIRequestTestResult{Status: "failed", StatusCode: response.StatusCode, ResponseContentType: response.Header.Get("Content-Type")}, err
	}
	// result 是返回给前端的非敏感状态、类型和限长响应预览。
	result := cardsapp.APIRequestTestResult{Status: "success", StatusCode: response.StatusCode, ResponseContentType: response.Header.Get("Content-Type"), ResponsePreview: truncateAPITestPreview(string(rawBody))}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.Status = "failed"
		return result, nil
	}
	// document 只在当前函数内持有 JSON 响应，用于字段名和路径提取。
	var document any
	// decoder 使用 JSON Number 以保持卡密等大整数的原始精度。
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if decoder.Decode(&document) == nil {
		// object、ok 分别是 JSON 顶层对象视图和类型判断结果。
		if object, ok := document.(map[string]any); ok {
			result.ResponseFields = make([]string, 0, len(object))
			// key 是当前 JSON 顶层字段名称，只返回名称不返回全部字段值。
			for key := range object {
				result.ResponseFields = append(result.ResponseFields, key)
			}
			sort.Strings(result.ResponseFields)
		}
		if config.ResponsePath != "" {
			// value、found 分别是响应路径命中的值和是否存在标记。
			if value, found := lookupAPIResponsePath(document, config.ResponsePath); found {
				result.ExtractedValue = truncateAPITestPreview(apiJSONScalar(value))
			}
		} else {
			result.ExtractedValue = truncateAPITestPreview(apiJSONScalar(document))
		}
	}
	return result, nil
}

// truncateAPITestPreview 限制测试结果文本长度并标记被截断的响应。
func truncateAPITestPreview(value string) string {
	if len(value) <= apiTestPreviewLimit {
		return value
	}
	return value[:apiTestPreviewLimit] + "..."
}

// Fetch 根据单个订单单位构造并发送一次 API 请求；重试仍复用同一幂等键。
func (c *apiDeliveryClient) Fetch(ctx context.Context, request automation.APICardRequest) (automation.APICardResult, error) {
	// config 是从专用卡券读取路径得到的完整 API 配置。
	config, err := cardsapp.ParseAPIConfig(request.Config)
	if err != nil {
		return automation.APICardResult{}, err
	}
	// itemDetail 是本地已保存的商品详情；查询不到时保持空字符串，不凭空补造内容。
	itemDetail := ""
	if c.store != nil && c.store.Items != nil && request.ItemID != "" {
		// item、itemErr 保存商品详情查询结果及不存在/数据库错误的区分。
		if item, itemErr := c.store.Items.GetByCookieItem(ctx, request.AccountID, request.ItemID); itemErr == nil {
			itemDetail = item.ItemDetail
		} else if !errors.Is(itemErr, db.ErrNotFound) {
			return automation.APICardResult{}, fmt.Errorf("读取商品详情: %w", itemErr)
		}
	}
	// idempotencyKey 是由触发键、动作、卡券和单位序号稳定生成的请求幂等键。
	idempotencyKey := apiDeliveryIdempotencyKey(request.TriggerKey, request.ActionID, request.CardID, request.UnitIndex)
	// variables 保存可用于请求头和参数值替换的订单上下文。
	variables := map[string]string{
		"{order_id}":             request.OrderID,
		"{item_id}":              request.ItemID,
		"{buyer_id}":             request.BuyerID,
		"{chat_id}":              request.ChatID,
		"{account_id}":           request.AccountID,
		"{cookie_id}":            request.AccountID,
		"{spec_name}":            request.SpecName,
		"{spec_value}":           request.SpecValue,
		"{quantity}":             request.Quantity,
		"{order_quantity}":       request.Quantity,
		"{amount}":               request.Amount,
		"{order_amount}":         request.Amount,
		"{item_detail}":          itemDetail,
		"{trigger_type}":         request.TriggerType,
		"{timestamp}":            time.Now().UTC().Format(time.RFC3339Nano),
		"{delivery_unit_index}":  strconv.Itoa(request.UnitIndex),
		"{delivery_total_count}": strconv.Itoa(request.TotalUnits),
		"{idempotency_key}":      idempotencyKey,
	}
	// headers、params 是递归替换后的请求模板，模板键名保持不变。
	headers := replaceAPIJSONMap(config.Headers, variables)
	// params 保存递归替换后的请求参数模板。
	params := replaceAPIJSONMap(config.Params, variables)
	// requestURL 是不包含动态替换的固定 API 地址；GET 参数随后编码到查询部分。
	requestURL, err := url.Parse(config.URL)
	if err != nil {
		return automation.APICardResult{}, err
	}
	if config.Method == http.MethodGet {
		// query 保存固定 URL 查询参数与 API 模板参数合并后的结果。
		query := requestURL.Query()
		// key、value 分别是不会动态替换的参数名及已经替换的参数值。
		for key, value := range params {
			query.Set(key, apiJSONScalar(value))
		}
		requestURL.RawQuery = query.Encode()
	}
	// bodyValues 保存 POST 正文模板；旧配置没有 body 时继续把 params 作为正文，保持兼容。
	bodyValues := replaceAPIJSONMap(config.Body, variables)
	if len(bodyValues) == 0 {
		bodyValues = params
	}
	// body 保存 POST 请求编码后的正文；GET 请求不把参数复制到请求体。
	body, err := encodeAPIDeliveryBody(bodyValues, config.ContentType)
	if err != nil {
		return automation.APICardResult{}, fmt.Errorf("编码 API 请求参数: %w", err)
	}
	// attempts 是本次请求允许执行的总次数；未开启重试时固定为一次。
	attempts := 1
	if config.RetryEnabled {
		attempts = apiDeliveryRetryLimit
	}
	// attempt 表示当前已执行的请求次数，最多不超过配置允许的总次数。
	for attempt := 1; attempt <= attempts; attempt++ {
		// result、attemptErr 保存本次 HTTP 调用的结果及响应提取错误。
		result, attemptErr := c.doAPIRequest(ctx, config, requestURL.String(), headers, body, attempt)
		if attemptErr == nil {
			return result, nil
		}
		if !shouldRetryAPIDelivery(attemptErr, attempt, attempts) {
			return result, attemptErr
		}
		// retryTimer 在上下文取消时立即终止退避，避免自动化工作线程被无意义占用。
		retryGap := apiDeliveryRetryGaps[attempt-1]
		// retryTimer 控制下一次请求前的有限退避时间。
		retryTimer := time.NewTimer(retryGap)
		select {
		case <-ctx.Done():
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			return automation.APICardResult{Dispatched: result.Dispatched}, ctx.Err()
		case <-retryTimer.C:
		}
	}
	return automation.APICardResult{Dispatched: true}, errors.New("API 发货请求重试次数耗尽")
}

// doAPIRequest 执行一次不含重试的 HTTP 请求，并把状态码映射为可安全分类的结果。
func (c *apiDeliveryClient) doAPIRequest(ctx context.Context, config cardsapp.APIConfig, rawURL string, headers map[string]any, body []byte, attempt int) (automation.APICardResult, error) {
	// requestBody 保存 POST 的可重复读取请求体；GET 请求传入空体。
	var requestBody io.Reader
	if config.Method == http.MethodPost {
		requestBody = bytes.NewReader(body)
	}
	// request、err 保存当前尝试的 HTTP 请求及构造错误。
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(config.Timeout)*time.Second)
	defer cancel()
	// request、err 保存当前尝试的 HTTP 请求及构造错误。
	request, err := http.NewRequestWithContext(requestCtx, config.Method, rawURL, requestBody)
	if err != nil {
		return automation.APICardResult{}, err
	}
	// key、value 分别是请求头名称及递归替换后的请求头值。
	for key, value := range headers {
		request.Header.Set(key, apiJSONScalar(value))
	}
	if config.Method == http.MethodPost && request.Header.Get("Content-Type") == "" {
		// contentType 是用户选择的正文媒体类型；缺省时使用 JSON 兼容旧配置。
		contentType := config.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		request.Header.Set("Content-Type", contentType)
	}
	// response、err 保存远端响应及网络调用错误。
	client := c.httpClient()
	// response、err 保存远端响应及网络调用错误。
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return automation.APICardResult{}, ctx.Err()
		}
		return automation.APICardResult{Dispatched: true}, &apiDeliveryRetryableError{kind: "network", err: err}
	}
	defer response.Body.Close()
	if c.logger != nil {
		c.logger.Info("API 卡发货请求完成", "method", config.Method, "attempt", attempt, "status", response.StatusCode)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// readErr 保存受限读取暂时性错误，不读取响应正文到日志。
		if _, readErr := readAPIDeliveryBody(response.Body); readErr != nil {
			return automation.APICardResult{Dispatched: response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500}, readErr
		}
		// dispatched 表示暂时性状态可能伴随服务端处理；明确的其他 4xx 视为确定拒绝。
		dispatched := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return automation.APICardResult{Dispatched: dispatched}, &apiDeliveryHTTPError{statusCode: response.StatusCode}
	}
	// content、readErr 保存成功响应的提取结果及解析错误。
	content, readErr := readAPIDeliveryResponse(response.Body, response.Header.Get("Content-Type"), config.ResponsePath)
	if readErr != nil {
		return automation.APICardResult{Dispatched: true}, readErr
	}
	if strings.TrimSpace(content) == "" {
		return automation.APICardResult{Dispatched: true}, errors.New("API 响应提取结果为空")
	}
	return automation.APICardResult{Content: content}, nil
}

// encodeAPIDeliveryBody 按用户选择的 Content-Type 编码 API 请求正文。
func encodeAPIDeliveryBody(values map[string]any, contentType string) ([]byte, error) {
	// lowerContentType 保存忽略大小写后的媒体类型，供正文编码分支判断。
	lowerContentType := strings.ToLower(contentType)
	if strings.Contains(lowerContentType, "x-www-form-urlencoded") {
		// formValues 保存表单编码所需的字符串字段。
		formValues := url.Values{}
		// key、value 分别是表单字段名称和待编码的模板值。
		// key、value 分别是纯文本字段名称和字段值。
		for key, value := range values {
			formValues.Set(key, apiJSONScalar(value))
		}
		return []byte(formValues.Encode()), nil
	}
	if strings.Contains(lowerContentType, "text/plain") {
		// lines 保存纯文本键值正文，每行一个字段，便于普通 Web 服务直接读取。
		lines := make([]string, 0, len(values))
		// key、value 分别是 XML 字段名称和字段值。
		for key, value := range values {
			lines = append(lines, key+"="+apiJSONScalar(value))
		}
		sort.Strings(lines)
		return []byte(strings.Join(lines, "\n")), nil
	}
	if strings.Contains(lowerContentType, "application/xml") {
		// xmlParts 保存经过 XML 转义的简单字段元素。
		xmlParts := make([]string, 0, len(values))
		// key、value 分别是 XML 字段名称和待转义字段值。
		for key, value := range values {
			if !isXMLTagName(key) {
				key = "field"
			}
			xmlParts = append(xmlParts, fmt.Sprintf("<%s>%s</%s>", key, xmlEscape(apiJSONScalar(value)), key))
		}
		sort.Strings(xmlParts)
		return []byte("<body>" + strings.Join(xmlParts, "") + "</body>"), nil
	}
	return json.Marshal(values)
}

// isXMLTagName 判断用户填写的键名是否可以作为 XML 标签名使用。
func isXMLTagName(value string) bool {
	if value == "" || (value[0] >= '0' && value[0] <= '9') {
		return false
	}
	// character 表示当前键名中的一个 Unicode 字符。
	for _, character := range value {
		if character != '_' && character != '-' && character != ':' && character != '.' &&
			(character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// xmlEscape 转义 XML 正文中的五类保留字符，避免用户输入破坏请求结构。
func xmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;").Replace(value)
}

// httpClient 返回 API 发货共享的出站客户端；客户端策略会在每次请求时读取运行时开关。
func (c *apiDeliveryClient) httpClient() *http.Client {
	if c == nil {
		return netguard.ConfiguredHTTPClient(0)
	}
	c.clientOnce.Do(func() {
		if c.client == nil {
			c.client = netguard.ConfiguredHTTPClient(0)
		}
	})
	return c.client
}

// apiDeliveryRetryableError 标记可按网络错误策略重试的单次调用失败。
type apiDeliveryRetryableError struct {
	// kind 记录失败类别，不包含请求地址或响应秘密。
	kind string
	// err 保存底层网络错误供日志外的调用链诊断。
	err error
}

// Error 返回不含请求秘密的网络错误文本。
func (e *apiDeliveryRetryableError) Error() string { return "API 发货网络请求失败" }

// Unwrap 返回底层错误供上下文取消和超时判断使用。
func (e *apiDeliveryRetryableError) Unwrap() error { return e.err }

// apiDeliveryHTTPError 保存可用于重试判断的 HTTP 状态码。
type apiDeliveryHTTPError struct {
	// statusCode 是远端返回的 HTTP 状态码。
	statusCode int
}

// Error 返回不包含响应正文的 HTTP 错误。
func (e *apiDeliveryHTTPError) Error() string {
	return fmt.Sprintf("API 发货 HTTP 状态异常: %d", e.statusCode)
}

// shouldRetryAPIDelivery 判断错误是否属于幂等重试允许的网络、超时或暂时性 HTTP 错误。
func shouldRetryAPIDelivery(err error, attempt, limit int) bool {
	if err == nil || attempt >= limit {
		return false
	}
	// httpErr 保存可按状态码判断的 HTTP 失败。
	var httpErr *apiDeliveryHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.statusCode == http.StatusRequestTimeout || httpErr.statusCode == http.StatusTooManyRequests || httpErr.statusCode >= 500
	}
	// networkErr 保存可按幂等策略重试的网络失败。
	var networkErr *apiDeliveryRetryableError
	return errors.As(err, &networkErr) && (networkErr.kind == "network" || errors.Is(networkErr, context.DeadlineExceeded))
}

// readAPIDeliveryResponse 读取受限响应并按路径提取卡密文本。
func readAPIDeliveryResponse(reader io.Reader, contentType, responsePath string) (string, error) {
	// body 保存受大小限制读取的完整响应体。
	body, err := readAPIDeliveryBody(reader)
	if err != nil {
		return "", err
	}
	// trimmed 保存去除空白后的响应内容，用于空值和 JSON 判断。
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", errors.New("API 响应为空")
	}
	// document 保存 JSON 响应的动态结构，仅在当前函数内用于提取文本。
	var document any
	// decoder 保留 JSON 数字的原始文本，避免大数型卡密经过 float64 发生舍入。
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&document) != nil {
		return trimmed, nil
	}
	if responsePath != "" {
		// value、found 保存响应路径命中的节点及命中状态。
		value, found := lookupAPIResponsePath(document, responsePath)
		if !found || value == nil {
			return "", errors.New("API 响应路径不存在或为空")
		}
		return apiJSONScalar(value), nil
	}
	// object、ok 保存可按默认字段提取的 JSON 对象及类型判断。
	if object, ok := document.(map[string]any); ok {
		// key 表示默认响应提取候选字段名。
		for _, key := range []string{"data", "content", "card"} {
			// value、found 保存候选字段的值及是否存在。
			if value, found := object[key]; found && value != nil {
				return apiJSONScalar(value), nil
			}
		}
	}
	return apiJSONScalar(document), nil
}

// readAPIDeliveryBody 读取 API 响应体并拒绝超过 1 MiB 的内容，不返回正文到日志或 HTTP 响应。
func readAPIDeliveryBody(reader io.Reader) ([]byte, error) {
	// limited 保存多读取一个字节以检测超大响应的受限读取器。
	limited := io.LimitReader(reader, apiDeliveryResponseLimit+1)
	// body、err 保存受限读取的响应体和读取错误。
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("读取 API 响应失败: %w", err)
	}
	if int64(len(body)) > apiDeliveryResponseLimit {
		return nil, errors.New("API 响应超过 1 MiB 限制")
	}
	return body, nil
}

// lookupAPIResponsePath 按点号字段和数组下标解析响应路径。
func lookupAPIResponsePath(document any, path string) (any, bool) {
	// current 保存逐段路径解析的当前 JSON 节点。
	current := document
	// segment 表示当前响应路径的字段或数组段。
	for _, segment := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if segment == "" {
			return nil, false
		}
		// name、index、hasIndex、ok 保存当前路径段的字段名、数组下标和解析状态。
		name, index, hasIndex, ok := parseAPIPathSegment(segment)
		if !ok {
			return nil, false
		}
		if name != "" {
			// object、objectOK 保存当前节点的对象视图及类型判断。
			object, objectOK := current.(map[string]any)
			if !objectOK {
				return nil, false
			}
			current, ok = object[name]
			if !ok {
				return nil, false
			}
		}
		if hasIndex {
			// array、arrayOK 保存当前节点的数组视图及类型判断。
			array, arrayOK := current.([]any)
			if !arrayOK || index < 0 || index >= len(array) {
				return nil, false
			}
			current = array[index]
		}
	}
	return current, true
}

// parseAPIPathSegment 把字段名和可选数组下标拆分为结构化路径段。
func parseAPIPathSegment(segment string) (string, int, bool, bool) {
	// bracket 保存数组下标左括号位置。
	bracket := strings.IndexByte(segment, '[')
	if bracket < 0 {
		return segment, 0, false, true
	}
	if !strings.HasSuffix(segment, "]") || bracket == len(segment)-2 {
		return "", 0, false, false
	}
	// index、err 保存括号内数组下标及其解析错误。
	index, err := strconv.Atoi(segment[bracket+1 : len(segment)-1])
	if err != nil || index < 0 {
		return "", 0, false, false
	}
	return segment[:bracket], index, true, true
}

// replaceAPIJSONMap 递归替换模板值，保持 JSON 对象键名原样不变。
func replaceAPIJSONMap(input map[string]any, variables map[string]string) map[string]any {
	// output 保存替换后的新对象，避免修改卡券配置缓存。
	output := make(map[string]any, len(input))
	// key、value 分别是模板字段名和待替换模板值。
	for key, value := range input {
		output[key] = replaceAPIJSONValue(value, variables)
	}
	return output
}

// replaceAPIJSONValue 递归处理对象、数组和字符串模板值。
func replaceAPIJSONValue(value any, variables map[string]string) any {
	// current 保存当前递归节点的动态 JSON 值。
	switch current := value.(type) {
	case string:
		// key、replacement 分别是动态变量和订单上下文替换值。
		for key, replacement := range variables {
			current = strings.ReplaceAll(current, key, replacement)
		}
		return current
	case map[string]any:
		return replaceAPIJSONMap(current, variables)
	case []any:
		// output 保存数组中每个值的递归替换结果。
		output := make([]any, len(current))
		// index、child 分别是数组位置和当前递归节点。
		for index, child := range current {
			output[index] = replaceAPIJSONValue(child, variables)
		}
		return output
	default:
		return value
	}
}

// apiJSONScalar 将响应或查询参数的 JSON 值转换为 API 发货使用的紧凑文本。
func apiJSONScalar(value any) string {
	// current 保存当前需要转换为紧凑文本的动态值。
	switch current := value.(type) {
	case string:
		return current
	case json.Number:
		return current.String()
	case nil:
		return ""
	default:
		// encoded、err 保存复杂响应值的紧凑 JSON 表示及编码错误。
		encoded, err := json.Marshal(current)
		if err != nil {
			return fmt.Sprint(current)
		}
		return string(encoded)
	}
}

// apiDeliveryIdempotencyKey 生成跨进程稳定且不暴露订单内容的幂等键。
func apiDeliveryIdempotencyKey(triggerKey string, actionID, cardID int64, unitIndex int) string {
	// sum 保存幂等键输入的 SHA-256 摘要，不包含可逆订单秘密。
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%d", triggerKey, actionID, cardID, unitIndex)))
	return hex.EncodeToString(sum[:])
}

var _ automation.APICardFetcher = (*apiDeliveryClient)(nil)
