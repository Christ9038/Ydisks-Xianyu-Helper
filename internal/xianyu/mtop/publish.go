package mtop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/protocol"
)

// UploadMediaAPI 用于本次流程后续判断的UploadMediaAPI
const (
	UploadMediaAPI   = "https://stream-upload.goofish.com/api/upload.api"
	PublishItemAPI   = "https://h5api.m.goofish.com/h5/mtop.idle.pc.idleitem.publish/1.0/"
	RecommendItemAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.kgraph.property.recommend/2.0/"
)

// ErrPublishCategoryUnrecognized 表示闲鱼类目推荐接口调用成功，但没有给出可发布类目。
// 这是发生在最终发布接口之前的确定性结果，调用方可以安全地改用人工指定类目。
// ErrPublishCategoryUnrecognized 用于本次流程后续判断的Err发布分类Unrecognized
var ErrPublishCategoryUnrecognized = errors.New("未能自动识别商品类目，请调整标题或图片后重试")

// PublishErrorCode 用于本次流程后续判断的发布错误Code
type PublishErrorCode string

// PublishErrorUnknown 用于本次流程后续判断的发布错误Unknown
const (
	PublishErrorUnknown                PublishErrorCode = "publish_failed"
	PublishErrorTokenExpired           PublishErrorCode = "auth_expired"
	PublishErrorStockPermissionMissing PublishErrorCode = "stock_permission_missing"
)

// PublishError 用于本次流程后续判断的发布错误
type PublishError struct {
	Code PublishErrorCode
	Ret  []string
	Body string
}

// Error 封装错误业务协调。
func (e *PublishError) Error() string {
	if len(e.Ret) > 0 {
		return strings.Join(sanitizeMTopRet(e.Ret), "; ")
	}
	if e.Body != "" {
		return truncate(sanitizeMTopText(e.Body), 240)
	}
	return string(e.Code)
}

// sanitizeMTopRet 脱敏发布错误中的平台 ret，并保持发布错误的历史分隔格式。
func sanitizeMTopRet(ret []string) []string {
	// values 保存非空且已脱敏的平台错误条目。
	values := make([]string, 0, len(ret))
	// value 表示当前发布响应中的单条平台错误标记。
	for _, value := range ret {
		// item 保存当前平台错误条目的安全文本。
		item := strings.TrimSpace(sanitizeMTopText(value))
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}

// isPublishTokenFailure 判断发布流程是否因 MTOP Token 过期或刷新失败而终止。
func isPublishTokenFailure(err error) bool {
	return IsMTopTokenExpiredErr(err)
}

// PublishImage 用于本次流程后续判断的发布图片
type PublishImage struct {
	Filename    string
	ContentType string
	Data        []byte
}

// PublishCategory 是发布商品时可使用的人工兜底类目。
// CatID、CatName 和 ChannelCatID 必填；部分闲鱼类目没有 TBCatID。
// PublishCategory 用于本次流程后续判断的发布分类
type PublishCategory struct {
	CatID        string `json:"cat_id"`
	CatName      string `json:"cat_name"`
	ChannelCatID string `json:"channel_cat_id,omitempty"`
	TBCatID      string `json:"tb_cat_id,omitempty"`
}

// PublishLocation 是高德附近 POI 映射成的、可直接用于闲鱼发布商品的发货地。
type PublishLocation struct {
	Area       string  `json:"area"`
	City       string  `json:"city"`
	DivisionID string  `json:"division_id"`
	Longitude  float64 `json:"longitude"`
	Latitude   float64 `json:"latitude"`
	POIID      string  `json:"poi_id"`
	POIName    string  `json:"poi_name"`
	Province   string  `json:"province"`
}

// DefaultVirtualPublishCategory 是从闲鱼类目推荐响应中核实的“电子资料”类目。
// 该类目没有 tbCatId，发布时必须保留为空而不是伪造淘宝类目 ID。
// DefaultVirtualPublishCategory 封装DefaultVirtual发布分类业务协调。
func DefaultVirtualPublishCategory() PublishCategory {
	return PublishCategory{
		CatID:        "50023914",
		CatName:      "电子资料",
		ChannelCatID: "202036301",
	}
}

// PublishItemRequest 用于本次流程后续判断的发布商品请求
type PublishItemRequest struct {
	Title              string
	Description        string
	PriceCents         int64
	OriginalPriceCents int64
	Quantity           int
	PostageMode        string
	PostageCents       int64
	// Virtual 表示商品通过系统的虚拟发货流程交付；显式提供 Location 时仍会向闲鱼提交发货地。
	Virtual           bool
	PreferredCategory *PublishCategory
	Location          *PublishLocation
	Images            []PublishImage
	// Specs 是多规格商品的规格维度；为空时按单规格发布。
	Specs []PublishSpec
	// SKUs 是规格组合的逐行价格和库存。
	SKUs []PublishSKU
	// SpecImages 是规格值图片，规格值通过 ImageIndex 引用。
	SpecImages []PublishImage
	// BeforePublish 在图片上传和类目准备完成后、最终商品发布请求发出前执行，可响应批次节流取消。
	BeforePublish func(context.Context) error
}

// PublishItemResult 用于本次流程后续判断的发布商品结果
type PublishItemResult struct {
	ItemID         string
	ItemURL        string
	Title          string
	PriceText      string
	CategoryID     string
	CategoryName   string
	ImageURL       string
	Quantity       int
	RawData        map[string]any
	UpdatedCookies string
}

// uploadedImage 用于本次流程后续判断的uploaded图片
type uploadedImage struct {
	URL    string
	Width  int
	Height int
}

// PublishItem 封装发布商品业务协调。
func (c *ClientImpl) PublishItem(ctx context.Context, cookiesStr string, req PublishItemRequest) (*PublishItemResult, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, errors.New("商品标题不能为空")
	}
	if strings.TrimSpace(req.Description) == "" {
		req.Description = req.Title
	}
	if len(req.Specs) == 0 && req.PriceCents <= 0 {
		return nil, errors.New("商品价格必须大于 0")
	}
	if req.Quantity <= 0 {
		return nil, errors.New("库存数量必须大于 0")
	}
	if len(req.Images) == 0 {
		return nil, errors.New("至少上传 1 张商品图片")
	}
	if len(req.Images) > 9 {
		return nil, errors.New("商品图片最多 9 张")
	}
	// err 表示规格、SKU 和规格图片引用的校验结果。
	if err := validatePublishSKUs(req.Specs, req.SKUs, len(req.SpecImages)); err != nil {
		return nil, err
	}
	if len(req.SKUs) > 0 {
		req.Quantity = publishSKUQuantity(req.SKUs)
	}
	if req.PreferredCategory != nil && !validPublishCategory(*req.PreferredCategory) {
		return nil, errors.New("默认类目必须同时包含类目 ID、类目名称和频道类目 ID")
	}
	// currentCookies 用于本次流程后续判断的currentCookies
	currentCookies := cookiesStr
	if // session 用于本次流程后续判断的会话
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// uploaded 用于本次流程后续判断的uploaded
	uploaded := make([]uploadedImage, 0, len(req.Images))
	// img 表示当前遍历过程中的img
	for _, img := range req.Images {
		// res、updated、err 用于本次流程后续判断的res、updated、err
		res, updated, err := c.uploadPublishImage(ctx, currentCookies, img)
		if err != nil {
			return nil, err
		}
		if updated != "" {
			currentCookies = updated
		}
		uploaded = append(uploaded, res)
	}
	// uploadedSpecImages 保存规格值图片上传后的平台地址，顺序与请求图片索引一致。
	uploadedSpecImages := make([]uploadedImage, 0, len(req.SpecImages))
	// img 表示当前待上传的规格值图片。
	for _, img := range req.SpecImages {
		// res、updated、err 保存当前规格图片的上传结果、Cookie 更新和错误。
		res, updated, err := c.uploadPublishImage(ctx, currentCookies, img)
		if err != nil {
			return nil, err
		}
		if updated != "" {
			currentCookies = updated
		}
		uploadedSpecImages = append(uploadedSpecImages, res)
	}
	// category 用于本次流程后续判断的分类
	var category map[string]any
	// updated 用于本次流程后续判断的updated
	var updated string
	// err 用于本次流程后续判断的err
	var err error
	if req.PreferredCategory != nil {
		category = fallbackPublishCategory(*req.PreferredCategory)
	} else {
		category, updated, err = c.recommendPublishCategory(ctx, currentCookies, req.Title, req.Description, uploaded)
		if updated != "" {
			currentCookies = updated
		}
		if err != nil {
			if errors.Is(err, ErrPublishCategoryUnrecognized) {
				category = fallbackPublishCategory(DefaultVirtualPublishCategory())
			} else {
				return nil, err
			}
		}
	}
	if req.BeforePublish != nil {
		// err 保存发布前闸门等待或取消错误。
		if err := req.BeforePublish(ctx); err != nil {
			return nil, err
		}
	}
	// publishCtx、publishCancel 为最终发布 HTTP 请求创建独立的两分钟网络超时预算，不计入前置节流等待。
	publishCtx, publishCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer publishCancel()
	return c.publishItemOnce(publishCtx, currentCookies, req, uploaded, uploadedSpecImages, category)
}

// uploadPublishImage 封装upload发布图片业务协调。
func (c *ClientImpl) uploadPublishImage(ctx context.Context, cookiesStr string, img PublishImage) (uploadedImage, string, error) {
	// hc 用于本次流程后续判断的hc
	hc := c.httpClientWithTimeout(60 * time.Second)
	if img.ContentType == "" {
		img.ContentType = "application/octet-stream"
	}
	if img.Filename == "" {
		img.Filename = "image"
	}
	// body 用于本次流程后续判断的请求体
	var body bytes.Buffer
	// mw 用于本次流程后续判断的mw
	mw := multipart.NewWriter(&body)
	// header 用于本次流程后续判断的header
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf("form-data; name=\"file\"; filename=\"%s\"", escapeMultipartFilename(filepath.Base(img.Filename))))
	header.Set("Content-Type", img.ContentType)
	// part、err 用于本次流程后续判断的part、err
	part, err := mw.CreatePart(header)
	if err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	if // err 用于本次流程后续判断的err
	_, err := part.Write(img.Data); err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	if // err 用于本次流程后续判断的err
	err := mw.Close(); err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	// u 用于本次流程后续判断的u
	u, _ := url.Parse(UploadMediaAPI)
	// q 用于本次流程后续判断的q
	q := u.Query()
	q.Set("floderId", "0")
	q.Set("appkey", "xy_chat")
	q.Set("_input_charset", "utf-8")
	u.RawQuery = q.Encode()
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	// requestCookies 用于本次流程后续判断的请求Cookies
	_, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", u.String())
	req.Header.Set("content-type", mw.FormDataContentType())
	setBrowserHeaders(req, requestCookies)
	req.Header.Set("accept", "*/*")
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		return uploadedImage{}, cookiesStr, fmt.Errorf("上传商品图片失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 用于本次流程后续判断的updated
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := readMTopBody(resp)
	if err != nil {
		return uploadedImage{}, updated, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return uploadedImage{}, updated, fmt.Errorf("上传商品图片失败: http=%d body=%s", resp.StatusCode, truncate(string(raw), 240))
	}
	// decoded 用于本次流程后续判断的decoded
	var decoded map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(raw, &decoded); err != nil {
		return uploadedImage{}, updated, fmt.Errorf("解析图片上传响应失败: %w (body=%s)", err, truncate(string(raw), 240))
	}
	// obj 用于本次流程后续判断的obj
	obj := mapFromAny(decoded["object"])
	if obj == nil {
		obj = mapFromAny(decoded["data"])
	}
	// imageURL 用于本次流程后续判断的图片URL
	imageURL := mtopString(obj["url"])
	if imageURL == "" {
		imageURL = mtopString(decoded["url"])
	}
	if imageURL == "" {
		return uploadedImage{}, updated, fmt.Errorf("图片上传响应缺少 url: %s", truncate(string(raw), 240))
	}
	// width、height 用于本次流程后续判断的width、height
	width, height := parsePix(mtopString(obj["pix"]))
	if width == 0 || height == 0 {
		if // cfg、err 用于本次流程后续判断的cfg、err
		cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data)); err == nil {
			width, height = cfg.Width, cfg.Height
		}
	}
	if width == 0 {
		width = 800
	}
	if height == 0 {
		height = 800
	}
	return uploadedImage{URL: imageURL, Width: width, Height: height}, updated, nil
}

// recommendPublishCategory 封装recommend发布分类业务协调。
func (c *ClientImpl) recommendPublishCategory(ctx context.Context, cookiesStr, title, desc string, images []uploadedImage) (map[string]any, string, error) {
	// imageInfos 用于本次流程后续判断的图片Infos
	imageInfos := make([]any, 0, len(images))
	// i、img 表示当前遍历过程中的i、img
	for i, img := range images {
		imageInfos = append(imageInfos, publishImagePayload(img, i == 0))
	}
	// data 用于本次流程后续判断的数据
	data := map[string]any{
		"title":        title,
		"lockCpv":      false,
		"multiSKU":     false,
		"publishScene": "mainPublish",
		"scene":        "newPublishChoice",
		"description":  desc,
		"uniqueCode":   strconv.FormatInt(time.Now().UnixMicro(), 10),
	}
	if len(imageInfos) > 0 {
		data["imageInfos"] = imageInfos
	}
	// decoded、updated、err 用于本次流程后续判断的decoded、updated、err
	decoded, updated, err := c.callMTop(ctx, cookiesStr, RecommendItemAPI, "mtop.taobao.idle.kgraph.property.recommend", "2.0", "a21ybx.publish.0.0", "a21ybx.item.sidebar.1.67321598K9Vgx8", "67321598K9Vgx8", data)
	if err != nil {
		if isPublishTokenFailure(err) {
			return nil, updated, &PublishError{Code: PublishErrorTokenExpired, Body: err.Error()}
		}
		return nil, updated, err
	}
	if !hasMTopSuccess(retFromDecoded(decoded)) {
		// classifiedPublishErr 保存发布域能够从无错误码文本中识别出的既有专用错误，例如库存权限不足。
		classifiedPublishErr := classifyPublishError(retFromDecoded(decoded), decoded)
		// publishErr 是发布域专用错误视图；已知专用码优先于通用未知系统分类。
		var publishErr *PublishError
		if errors.As(classifiedPublishErr, &publishErr) && publishErr.Code != PublishErrorUnknown {
			return nil, updated, classifiedPublishErr
		}
		// failure 保存推荐接口的统一平台失败分类；普通库存/权限错误继续由旧发布错误码兼容处理。
		failure := c.mtopResponseFailure("mtop.taobao.idle.kgraph.property.recommend", http.StatusOK, retFromDecoded(decoded), "平台 ret 未包含 SUCCESS")
		// kind、ok 保存推荐接口失败分类及其是否存在。
		if kind, ok := MTopErrorKindOf(failure); ok && kind != MTopErrorBusiness {
			return nil, updated, failure
		}
		return nil, updated, classifiedPublishErr
	}
	// dataMap 用于本次流程后续判断的数据Map
	dataMap := mapFromAny(decoded["data"])
	if dataMap == nil {
		return nil, updated, fmt.Errorf("%w: 类目推荐响应缺少 data", ErrPublishCategoryUnrecognized)
	}
	// cat 用于本次流程后续判断的cat
	cat := mapFromAny(dataMap["categoryPredictResult"])
	if !validPublishCategoryMap(cat) {
		// 部分账号/场景仅在 cardList 的“分类”卡片返回已选中的类目，
		// 不返回 categoryPredictResult。将其转换为统一的发布类目结构。
		if // selected 用于本次流程后续判断的selected
		selected := selectedCategoryFromCards(dataMap); selected != nil {
			cat = selected
			dataMap["categoryPredictResult"] = selected
		}
	}
	if !validPublishCategoryMap(cat) {
		return nil, updated, ErrPublishCategoryUnrecognized
	}
	return dataMap, updated, nil
}

// validPublishCategoryMap 封装有效发布分类Map业务协调。
func validPublishCategoryMap(category map[string]any) bool {
	return category != nil &&
		strings.TrimSpace(mtopString(category["catId"])) != "" &&
		strings.TrimSpace(mtopString(category["catName"])) != "" &&
		strings.TrimSpace(mtopString(category["channelCatId"])) != ""
}

// selectedCategoryFromCards 封装selected分类From卡密列表业务协调。
func selectedCategoryFromCards(data map[string]any) map[string]any {
	// cards 用于本次流程后续判断的卡密列表
	cards, _ := data["cardList"].([]any)
	// rawCard 表示当前遍历过程中的原始卡密
	for _, rawCard := range cards {
		// cardData 用于本次流程后续判断的卡密数据
		cardData := mapFromAny(mapFromAny(rawCard)["cardData"])
		if cardData == nil || mtopString(cardData["propertyId"]) != "-10000" {
			continue
		}
		// values 用于本次流程后续判断的values
		values, _ := cardData["valuesList"].([]any)
		// rawValue 表示当前遍历过程中的原始值
		for _, rawValue := range values {
			// value 用于本次流程后续判断的值
			value := mapFromAny(rawValue)
			if value == nil || !publishLabelSelected(value["isClicked"]) || mtopString(value["catId"]) == "" {
				continue
			}
			return map[string]any{
				"catId":        mtopString(value["catId"]),
				"catName":      mtopString(value["catName"]),
				"channelCatId": mtopString(value["channelCatId"]),
				"tbCatId":      mtopString(value["tbCatId"]),
			}
		}
	}
	return nil
}

// validPublishCategory 封装有效发布分类业务协调。
func validPublishCategory(category PublishCategory) bool {
	return strings.TrimSpace(category.CatID) != "" &&
		strings.TrimSpace(category.CatName) != "" &&
		strings.TrimSpace(category.ChannelCatID) != ""
}

// fallbackPublishCategory 封装fallback发布分类业务协调。
func fallbackPublishCategory(category PublishCategory) map[string]any {
	category = PublishCategory{
		CatID:        strings.TrimSpace(category.CatID),
		CatName:      strings.TrimSpace(category.CatName),
		ChannelCatID: strings.TrimSpace(category.ChannelCatID),
		TBCatID:      strings.TrimSpace(category.TBCatID),
	}
	return map[string]any{
		"categoryPredictResult": map[string]any{
			"catId":        category.CatID,
			"catName":      category.CatName,
			"channelCatId": category.ChannelCatID,
			"tbCatId":      category.TBCatID,
		},
		// 闲鱼将分类作为已选择的标签一并提交；仅传 itemCatDTO 会导致
		// 部分频道类目无法还原完整路径。
		"cardList": []any{map[string]any{
			"cardData": map[string]any{
				"propertyId":   "-10000",
				"propertyName": "分类",
				"valuesList": []any{map[string]any{
					"catName":      category.CatName,
					"channelCatId": category.ChannelCatID,
					"tbCatId":      category.TBCatID,
					"isClicked":    true,
				}},
			},
		}},
	}
}

// validPublishLocation 封装有效发布地址业务协调。
func validPublishLocation(loc PublishLocation) bool {
	return loc.DivisionID != "" && loc.Province != "" && loc.City != "" && loc.Longitude >= -180 && loc.Longitude <= 180 && loc.Latitude >= -90 && loc.Latitude <= 90 && !(loc.Longitude == 0 && loc.Latitude == 0)
}

// publishItemOnce 封装发布商品Once业务协调。
func (c *ClientImpl) publishItemOnce(ctx context.Context, cookiesStr string, req PublishItemRequest, images, specImages []uploadedImage, category map[string]any) (*PublishItemResult, error) {
	// imagePayloads 用于本次流程后续判断的图片Payloads
	imagePayloads := make([]any, 0, len(images))
	// i、img 表示当前遍历过程中的i、img
	for i, img := range images {
		imagePayloads = append(imagePayloads, publishImagePayload(img, i == 0))
	}
	// cat 用于本次流程后续判断的cat
	cat := mapFromAny(category["categoryPredictResult"])
	// data 用于本次流程后续判断的数据
	data := map[string]any{
		"freebies":         false,
		"itemTypeStr":      "b",
		"quantity":         strconv.Itoa(req.Quantity),
		"simpleItem":       "true",
		"imageInfoDOList":  imagePayloads,
		"itemTextDTO":      map[string]any{"desc": req.Description, "title": req.Title, "titleDescSeparate": false},
		"itemLabelExtList": publishLabels(category),
		"itemPriceDTO":     publishPriceDTO(req),
		"userRightsProtocols": []any{map[string]any{
			"enable": false, "serviceCode": "SKILL_PLAY_NO_MIND",
		}},
		"itemPostFeeDTO": postageDTO(req),
		"defaultPrice":   false,
		"itemCatDTO": map[string]any{
			"catId":        mtopString(cat["catId"]),
			"catName":      mtopString(cat["catName"]),
			"channelCatId": mtopString(cat["channelCatId"]),
			"tbCatId":      mtopString(cat["tbCatId"]),
		},
		"uniqueCode":   strconv.FormatInt(time.Now().UnixMicro(), 10),
		"sourceId":     "pcMainPublish",
		"bizcode":      "pcMainPublish",
		"publishScene": "pcMainPublish",
	}
	if len(req.Specs) > 0 {
		data["itemProperties"] = publishPropertiesPayload(req.Specs, specImages)
		data["itemSkuList"] = publishSKUListPayload(req.SKUs)
		// propertyImages 保存规格值图片的官方引用列表。
		propertyImages := publishPropertyImageList(req.Specs, specImages)
		if len(propertyImages) > 0 {
			data["propertyImageList"] = propertyImages
		}
	}
	if req.Location != nil {
		if !validPublishLocation(*req.Location) {
			return nil, errors.New("发货地信息不完整，请重新定位并选择")
		}
		data["itemAddrDTO"] = map[string]any{
			"area": req.Location.Area, "city": req.Location.City, "divisionId": req.Location.DivisionID,
			// 闲鱼网页版发布页使用 latitude,longitude 的顺序。
			"gps":   fmt.Sprintf("%s,%s", strconv.FormatFloat(req.Location.Latitude, 'f', -1, 64), strconv.FormatFloat(req.Location.Longitude, 'f', -1, 64)),
			"poiId": req.Location.POIID, "poiName": req.Location.POIName, "prov": req.Location.Province,
		}
	} else if !req.Virtual {
		return nil, errors.New("发布实物商品前必须选择发货地")
	}
	// decoded、updated、err 用于本次流程后续判断的decoded、updated、err
	decoded, updated, err := c.callMTop(ctx, cookiesStr, PublishItemAPI, "mtop.idle.pc.idleitem.publish", "1.0", "a21ybx.publish.0.0", "a21ybx.home.sidebar.1.46413da6EPl7v5", "46413da6EPl7v5", data)
	if err != nil {
		if isPublishTokenFailure(err) {
			return nil, &PublishError{Code: PublishErrorTokenExpired, Body: err.Error()}
		}
		return nil, err
	}
	// ret 用于本次流程后续判断的ret
	ret := retFromDecoded(decoded)
	if !hasMTopSuccess(ret) {
		// classifiedPublishErr 保存发布域能够从无错误码文本中识别出的既有专用错误，例如库存权限不足。
		classifiedPublishErr := classifyPublishError(ret, decoded)
		// publishErr 是发布域专用错误视图；已知专用码优先于通用未知系统分类。
		var publishErr *PublishError
		if errors.As(classifiedPublishErr, &publishErr) && publishErr.Code != PublishErrorUnknown {
			return nil, classifiedPublishErr
		}
		// failure 保存最终发布接口的统一平台失败分类；库存/权限错误仍保留既有专用码。
		failure := c.mtopResponseFailure("mtop.idle.pc.idleitem.publish", http.StatusOK, ret, "平台 ret 未包含 SUCCESS")
		// kind、ok 保存最终发布接口失败分类及其是否存在。
		if kind, ok := MTopErrorKindOf(failure); ok && kind != MTopErrorBusiness {
			return nil, failure
		}
		return nil, classifiedPublishErr
	}
	// dataMap 用于本次流程后续判断的数据Map
	dataMap := mapFromAny(decoded["data"])
	// itemID 用于本次流程后续判断的商品ID
	itemID := findStringDeep(dataMap, "itemId", "item_id", "id", "itemID")
	if itemID == "" {
		itemID = findStringDeep(decoded, "itemId", "item_id", "itemID")
	}
	// result 用于本次流程后续判断的结果
	result := &PublishItemResult{
		ItemID:         itemID,
		Title:          req.Title,
		PriceText:      publishRepresentativePriceText(req),
		CategoryID:     mtopString(cat["catId"]),
		CategoryName:   mtopString(cat["catName"]),
		ImageURL:       images[0].URL,
		Quantity:       req.Quantity,
		RawData:        dataMap,
		UpdatedCookies: updated,
	}
	if itemID != "" {
		result.ItemURL = "https://www.goofish.com/item?id=" + itemID
	}
	return result, nil
}

// buildMTopQuery 封装buildMTop查询业务协调。
func buildMTopQuery(api, version, t, sign, spmCnt, spmPre, logID string) string {
	// parts 用于本次流程后续判断的parts
	parts := [][2]string{
		{"jsv", "2.7.2"},
		{"appKey", protocol.SignAppKey},
		{"t", t},
		{"sign", sign},
		{"v", version},
		{"type", "originaljson"},
		{"accountSite", "xianyu"},
		{"dataType", "json"},
		{"timeout", "20000"},
		{"api", api},
		{"sessionOption", "AutoLoginOnly"},
		{"spm_cnt", spmCnt},
		{"spm_pre", spmPre},
		{"log_id", logID},
	}
	// b 用于本次流程后续判断的b
	var b strings.Builder
	// i、p 表示当前遍历过程中的i、p
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p[1]))
	}
	return b.String()
}

// setBrowserHeaders 封装set浏览器Headers业务协调。
func setBrowserHeaders(req *http.Request, cookiesStr string) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("origin", "https://www.goofish.com")
	req.Header.Set("referer", "https://www.goofish.com/")
	req.Header.Set("cookie", cookiesStr)
}

// postageDTO 封装postageDTO业务协调。
func postageDTO(req PublishItemRequest) map[string]any {
	// out 用于本次流程后续判断的out
	out := map[string]any{"canFreeShipping": false, "supportFreight": false, "onlyTakeSelf": false}
	switch req.PostageMode {
	case "free", "":
		out["canFreeShipping"] = true
		out["supportFreight"] = true
	case "distance":
		out["supportFreight"] = true
		out["templateId"] = "-100"
	case "fixed":
		out["supportFreight"] = true
		out["postPriceInCent"] = strconv.FormatInt(req.PostageCents, 10)
		out["templateId"] = "0"
	case "none":
		out["templateId"] = "0"
	default:
		out["canFreeShipping"] = true
		out["supportFreight"] = true
	}
	return out
}

// classifyPublishError 封装classify发布错误业务协调。
func classifyPublishError(ret []string, decoded map[string]any) error {
	// bodyBytes 用于本次流程后续判断的请求体Bytes
	bodyBytes, _ := json.Marshal(decoded)
	// body 用于本次流程后续判断的请求体
	body := string(bodyBytes)
	// joined 用于本次流程后续判断的joined
	joined := strings.ToLower(strings.Join(append(ret, body), " "))
	if isTokenExpiredRet(ret) || strings.Contains(joined, "login") || strings.Contains(joined, "session") {
		return &PublishError{Code: PublishErrorTokenExpired, Ret: ret, Body: body}
	}
	// stockTerms 用于本次流程后续判断的stockTerms
	stockTerms := []string{"库存", "数量", "多库存", "多件", "quantity", "stock", "inventory"}
	// permissionTerms 用于本次流程后续判断的permissionTerms
	permissionTerms := []string{"权限", "未开通", "不支持", "没有", "无法", "permission", "forbidden", "not allow", "not support"}
	if containsAny(joined, stockTerms) && containsAny(joined, permissionTerms) {
		return &PublishError{Code: PublishErrorStockPermissionMissing, Ret: ret, Body: body}
	}
	return &PublishError{Code: PublishErrorUnknown, Ret: ret, Body: body}
}

// containsAny 封装containsAny业务协调。
func containsAny(s string, terms []string) bool {
	// term 表示当前遍历过程中的term
	for _, term := range terms {
		if strings.Contains(s, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

// retFromDecoded 封装retFromDecoded业务协调。
func retFromDecoded(decoded map[string]any) []string {
	// raw 用于本次流程后续判断的原始
	raw, _ := decoded["ret"].([]any)
	// out 用于本次流程后续判断的out
	out := make([]string, 0, len(raw))
	// r 表示当前遍历过程中的r
	for _, r := range raw {
		out = append(out, mtopString(r))
	}
	return out
}

// mapFromAny 封装mapFromAny业务协调。
func mapFromAny(v any) map[string]any {
	if // m、ok 用于本次流程后续判断的m、ok
	m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// parsePix 封装parsePix业务协调。
func parsePix(pix string) (int, int) {
	// parts 用于本次流程后续判断的parts
	parts := strings.Split(pix, "x")
	if len(parts) != 2 {
		return 0, 0
	}
	// w 用于本次流程后续判断的w
	w, _ := strconv.Atoi(parts[0])
	// h 用于本次流程后续判断的h
	h, _ := strconv.Atoi(parts[1])
	return w, h
}

// centsText 封装cents文本业务协调。
func centsText(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64)
}

// escapeMultipartFilename 封装escapeMultipartFilename业务协调。
func escapeMultipartFilename(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}
