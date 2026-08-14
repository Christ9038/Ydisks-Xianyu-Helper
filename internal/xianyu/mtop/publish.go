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

// UploadMediaAPI 保存UploadMediaAPI，供当前处理流程使用
const (
	UploadMediaAPI   = "https://stream-upload.goofish.com/api/upload.api"
	PublishItemAPI   = "https://h5api.m.goofish.com/h5/mtop.idle.pc.idleitem.publish/1.0/"
	RecommendItemAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.kgraph.property.recommend/2.0/"
)

// ErrPublishCategoryUnrecognized 表示闲鱼类目推荐接口调用成功，但没有给出可发布类目。
// 这是发生在最终发布接口之前的确定性结果，调用方可以安全地改用人工指定类目。
// ErrPublishCategoryUnrecognized 保存Err发布分类Unrecognized，供当前处理流程使用
var ErrPublishCategoryUnrecognized = errors.New("未能自动识别商品类目，请调整标题或图片后重试")

// PublishErrorCode 保存发布错误Code，供当前处理流程使用
type PublishErrorCode string

// PublishErrorUnknown 保存发布错误Unknown，供当前处理流程使用
const (
	PublishErrorUnknown                PublishErrorCode = "publish_failed"
	PublishErrorTokenExpired           PublishErrorCode = "auth_expired"
	PublishErrorStockPermissionMissing PublishErrorCode = "stock_permission_missing"
)

// PublishError 保存发布错误，供当前处理流程使用
type PublishError struct {
	Code PublishErrorCode
	Ret  []string
	Body string
}

// Error 负责错误相关处理。
func (e *PublishError) Error() string {
	if len(e.Ret) > 0 {
		return strings.Join(e.Ret, "; ")
	}
	if e.Body != "" {
		return truncate(e.Body, 240)
	}
	return string(e.Code)
}

// PublishImage 保存发布图片，供当前处理流程使用
type PublishImage struct {
	Filename    string
	ContentType string
	Data        []byte
}

// PublishCategory 是发布商品时可使用的人工兜底类目。
// CatID、CatName 和 ChannelCatID 必填；部分闲鱼类目没有 TBCatID。
// PublishCategory 保存发布分类，供当前处理流程使用
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
// DefaultVirtualPublishCategory 负责DefaultVirtual发布分类相关处理。
func DefaultVirtualPublishCategory() PublishCategory {
	return PublishCategory{
		CatID:        "50023914",
		CatName:      "电子资料",
		ChannelCatID: "202036301",
	}
}

// PublishItemRequest 保存发布商品请求，供当前处理流程使用
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
}

// PublishItemResult 保存发布商品结果，供当前处理流程使用
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

// uploadedImage 保存uploaded图片，供当前处理流程使用
type uploadedImage struct {
	URL    string
	Width  int
	Height int
}

// PublishItem 负责发布商品相关处理。
func (c *ClientImpl) PublishItem(ctx context.Context, cookiesStr string, req PublishItemRequest) (*PublishItemResult, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, errors.New("商品标题不能为空")
	}
	if strings.TrimSpace(req.Description) == "" {
		req.Description = req.Title
	}
	if req.PriceCents <= 0 {
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
	if req.PreferredCategory != nil && !validPublishCategory(*req.PreferredCategory) {
		return nil, errors.New("默认类目必须同时包含类目 ID、类目名称和频道类目 ID")
	}
	// currentCookies 保存currentCookies，供当前处理流程使用
	currentCookies := cookiesStr
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// uploaded 保存uploaded，供当前处理流程使用
	uploaded := make([]uploadedImage, 0, len(req.Images))
	// img 表示当前遍历过程中的img
	for _, img := range req.Images {
		// res、updated、err 保存res、updated、err，供当前处理流程使用
		res, updated, err := c.uploadPublishImage(ctx, currentCookies, img)
		if err != nil {
			return nil, err
		}
		if updated != "" {
			currentCookies = updated
		}
		uploaded = append(uploaded, res)
	}
	// category 保存分类，供当前处理流程使用
	var category map[string]any
	// updated 保存updated，供当前处理流程使用
	var updated string
	// err 保存err，供当前处理流程使用
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
	return c.publishItemOnce(ctx, currentCookies, req, uploaded, category)
}

// RecommendPublishCategory 根据关键词调用闲鱼推荐接口，返回可直接用于发布的完整类目。
func (c *ClientImpl) RecommendPublishCategory(ctx context.Context, cookiesStr, keyword string) (PublishCategory, string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return PublishCategory{}, cookiesStr, errors.New("类目关键词不能为空")
	}
	// data、updated、err 保存data、updated、err，供当前处理流程使用
	data, updated, err := c.recommendPublishCategory(ctx, cookiesStr, keyword, keyword, nil)
	if err != nil {
		return PublishCategory{}, updated, err
	}
	// cat 保存cat，供当前处理流程使用
	cat := mapFromAny(data["categoryPredictResult"])
	// category 保存分类，供当前处理流程使用
	category := PublishCategory{
		CatID:        strings.TrimSpace(mtopString(cat["catId"])),
		CatName:      strings.TrimSpace(mtopString(cat["catName"])),
		ChannelCatID: strings.TrimSpace(mtopString(cat["channelCatId"])),
		TBCatID:      strings.TrimSpace(mtopString(cat["tbCatId"])),
	}
	if !validPublishCategory(category) {
		return PublishCategory{}, updated, fmt.Errorf("%w: 推荐结果缺少完整类目路径", ErrPublishCategoryUnrecognized)
	}
	return category, updated, nil
}

// uploadPublishImage 负责upload发布图片相关处理。
func (c *ClientImpl) uploadPublishImage(ctx context.Context, cookiesStr string, img PublishImage) (uploadedImage, string, error) {
	// hc 保存hc，供当前处理流程使用
	hc := c.httpClientWithTimeout(60 * time.Second)
	if img.ContentType == "" {
		img.ContentType = "application/octet-stream"
	}
	if img.Filename == "" {
		img.Filename = "image"
	}
	// body 保存请求体，供当前处理流程使用
	var body bytes.Buffer
	// mw 保存mw，供当前处理流程使用
	mw := multipart.NewWriter(&body)
	// header 保存header，供当前处理流程使用
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf("form-data; name=\"file\"; filename=\"%s\"", escapeMultipartFilename(filepath.Base(img.Filename))))
	header.Set("Content-Type", img.ContentType)
	// part、err 保存part、err，供当前处理流程使用
	part, err := mw.CreatePart(header)
	if err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	if // err 保存err，供当前处理流程使用
	_, err := part.Write(img.Data); err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	if // err 保存err，供当前处理流程使用
	err := mw.Close(); err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	// u 保存u，供当前处理流程使用
	u, _ := url.Parse(UploadMediaAPI)
	// q 保存q，供当前处理流程使用
	q := u.Query()
	q.Set("floderId", "0")
	q.Set("appkey", "xy_chat")
	q.Set("_input_charset", "utf-8")
	u.RawQuery = q.Encode()
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	// requestCookies 保存请求Cookies，供当前处理流程使用
	_, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", u.String())
	req.Header.Set("content-type", mw.FormDataContentType())
	setBrowserHeaders(req, requestCookies)
	req.Header.Set("accept", "*/*")
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := hc.Do(req)
	if err != nil {
		return uploadedImage{}, cookiesStr, fmt.Errorf("上传商品图片失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 保存updated，供当前处理流程使用
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readMTopBody(resp)
	if err != nil {
		return uploadedImage{}, updated, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return uploadedImage{}, updated, fmt.Errorf("上传商品图片失败: http=%d body=%s", resp.StatusCode, truncate(string(raw), 240))
	}
	// decoded 保存decoded，供当前处理流程使用
	var decoded map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &decoded); err != nil {
		return uploadedImage{}, updated, fmt.Errorf("解析图片上传响应失败: %w (body=%s)", err, truncate(string(raw), 240))
	}
	// obj 保存obj，供当前处理流程使用
	obj := mapFromAny(decoded["object"])
	if obj == nil {
		obj = mapFromAny(decoded["data"])
	}
	// imageURL 保存图片URL，供当前处理流程使用
	imageURL := mtopString(obj["url"])
	if imageURL == "" {
		imageURL = mtopString(decoded["url"])
	}
	if imageURL == "" {
		return uploadedImage{}, updated, fmt.Errorf("图片上传响应缺少 url: %s", truncate(string(raw), 240))
	}
	// width、height 保存width、height，供当前处理流程使用
	width, height := parsePix(mtopString(obj["pix"]))
	if width == 0 || height == 0 {
		if // cfg、err 保存cfg、err，供当前处理流程使用
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

// recommendPublishCategory 负责recommend发布分类相关处理。
func (c *ClientImpl) recommendPublishCategory(ctx context.Context, cookiesStr, title, desc string, images []uploadedImage) (map[string]any, string, error) {
	// imageInfos 保存图片Infos，供当前处理流程使用
	imageInfos := make([]any, 0, len(images))
	// i、img 表示当前遍历过程中的i、img
	for i, img := range images {
		imageInfos = append(imageInfos, publishImagePayload(img, i == 0))
	}
	// data 保存数据，供当前处理流程使用
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
	// decoded、updated、err 保存decoded、updated、err，供当前处理流程使用
	decoded, updated, err := c.callMTop(ctx, cookiesStr, RecommendItemAPI, "mtop.taobao.idle.kgraph.property.recommend", "2.0", "a21ybx.publish.0.0", "a21ybx.item.sidebar.1.67321598K9Vgx8", "67321598K9Vgx8", data)
	if err != nil {
		return nil, updated, err
	}
	if !hasMTopSuccess(retFromDecoded(decoded)) {
		return nil, updated, classifyPublishError(retFromDecoded(decoded), decoded)
	}
	// dataMap 保存数据Map，供当前处理流程使用
	dataMap := mapFromAny(decoded["data"])
	if dataMap == nil {
		return nil, updated, fmt.Errorf("%w: 类目推荐响应缺少 data", ErrPublishCategoryUnrecognized)
	}
	// cat 保存cat，供当前处理流程使用
	cat := mapFromAny(dataMap["categoryPredictResult"])
	if !validPublishCategoryMap(cat) {
		// 部分账号/场景仅在 cardList 的“分类”卡片返回已选中的类目，
		// 不返回 categoryPredictResult。将其转换为统一的发布类目结构。
		if // selected 保存selected，供当前处理流程使用
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

// validPublishCategoryMap 负责有效发布分类Map相关处理。
func validPublishCategoryMap(category map[string]any) bool {
	return category != nil &&
		strings.TrimSpace(mtopString(category["catId"])) != "" &&
		strings.TrimSpace(mtopString(category["catName"])) != "" &&
		strings.TrimSpace(mtopString(category["channelCatId"])) != ""
}

// selectedCategoryFromCards 负责selected分类From卡密列表相关处理。
func selectedCategoryFromCards(data map[string]any) map[string]any {
	// cards 保存卡密列表，供当前处理流程使用
	cards, _ := data["cardList"].([]any)
	// rawCard 表示当前遍历过程中的原始卡密
	for _, rawCard := range cards {
		// cardData 保存卡密数据，供当前处理流程使用
		cardData := mapFromAny(mapFromAny(rawCard)["cardData"])
		if cardData == nil || mtopString(cardData["propertyId"]) != "-10000" {
			continue
		}
		// values 保存values，供当前处理流程使用
		values, _ := cardData["valuesList"].([]any)
		// rawValue 表示当前遍历过程中的原始值
		for _, rawValue := range values {
			// value 保存值，供当前处理流程使用
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

// validPublishCategory 负责有效发布分类相关处理。
func validPublishCategory(category PublishCategory) bool {
	return strings.TrimSpace(category.CatID) != "" &&
		strings.TrimSpace(category.CatName) != "" &&
		strings.TrimSpace(category.ChannelCatID) != ""
}

// fallbackPublishCategory 负责fallback发布分类相关处理。
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

// validPublishLocation 负责有效发布地址相关处理。
func validPublishLocation(loc PublishLocation) bool {
	return loc.DivisionID != "" && loc.Province != "" && loc.City != "" && loc.Longitude >= -180 && loc.Longitude <= 180 && loc.Latitude >= -90 && loc.Latitude <= 90 && !(loc.Longitude == 0 && loc.Latitude == 0)
}

// publishItemOnce 负责发布商品Once相关处理。
func (c *ClientImpl) publishItemOnce(ctx context.Context, cookiesStr string, req PublishItemRequest, images []uploadedImage, category map[string]any) (*PublishItemResult, error) {
	// imagePayloads 保存图片Payloads，供当前处理流程使用
	imagePayloads := make([]any, 0, len(images))
	// i、img 表示当前遍历过程中的i、img
	for i, img := range images {
		imagePayloads = append(imagePayloads, publishImagePayload(img, i == 0))
	}
	// cat 保存cat，供当前处理流程使用
	cat := mapFromAny(category["categoryPredictResult"])
	// data 保存数据，供当前处理流程使用
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
	// decoded、updated、err 保存decoded、updated、err，供当前处理流程使用
	decoded, updated, err := c.callMTop(ctx, cookiesStr, PublishItemAPI, "mtop.idle.pc.idleitem.publish", "1.0", "a21ybx.publish.0.0", "a21ybx.home.sidebar.1.46413da6EPl7v5", "46413da6EPl7v5", data)
	if err != nil {
		return nil, err
	}
	// ret 保存ret，供当前处理流程使用
	ret := retFromDecoded(decoded)
	if !hasMTopSuccess(ret) {
		return nil, classifyPublishError(ret, decoded)
	}
	// dataMap 保存数据Map，供当前处理流程使用
	dataMap := mapFromAny(decoded["data"])
	// itemID 保存商品ID，供当前处理流程使用
	itemID := findStringDeep(dataMap, "itemId", "item_id", "id", "itemID")
	if itemID == "" {
		itemID = findStringDeep(decoded, "itemId", "item_id", "itemID")
	}
	// result 保存结果，供当前处理流程使用
	result := &PublishItemResult{
		ItemID:         itemID,
		Title:          req.Title,
		PriceText:      centsText(req.PriceCents),
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

// callMTop 负责callMTop相关处理。
func (c *ClientImpl) callMTop(ctx context.Context, cookiesStr, endpoint, api, version, spmCnt, spmPre, logID string, data any) (map[string]any, string, error) {
	// hc 保存hc，供当前处理流程使用
	hc := c.httpClient()
	// rawData 保存原始数据，供当前处理流程使用
	rawData, _ := json.Marshal(data)
	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := string(rawData)
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", endpoint)
	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 保存sign，供当前处理流程使用
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	// query 保存查询，供当前处理流程使用
	query := buildMTopQuery(api, version, t, sign, spmCnt, spmPre, logID)
	// body 保存请求体，供当前处理流程使用
	body := "data=" + url.QueryEscape(dataVal)
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := hc.Do(req)
	if err != nil {
		return nil, cookiesStr, fmt.Errorf("%s 请求失败: %w", api, err)
	}
	defer resp.Body.Close()
	// updated 保存updated，供当前处理流程使用
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, updated, err
	}
	// decoded 保存decoded，供当前处理流程使用
	var decoded map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, updated, fmt.Errorf("解析 %s 响应失败: %w (body=%s)", api, err, truncate(string(raw), 300))
	}
	return decoded, updated, nil
}

// buildMTopQuery 负责buildMTop查询相关处理。
func buildMTopQuery(api, version, t, sign, spmCnt, spmPre, logID string) string {
	// parts 保存parts，供当前处理流程使用
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
	// b 保存b，供当前处理流程使用
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

// setBrowserHeaders 负责set浏览器Headers相关处理。
func setBrowserHeaders(req *http.Request, cookiesStr string) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("origin", "https://www.goofish.com")
	req.Header.Set("referer", "https://www.goofish.com/")
	req.Header.Set("cookie", cookiesStr)
}

// publishImagePayload 负责发布图片请求载荷相关处理。
func publishImagePayload(img uploadedImage, major bool) map[string]any {
	return map[string]any{
		"extraInfo":  map[string]any{"isH": "false", "isT": "false", "raw": "false"},
		"isQrCode":   false,
		"url":        img.URL,
		"heightSize": img.Height,
		"widthSize":  img.Width,
		"major":      major,
		"type":       0,
		"status":     "done",
	}
}

// publishPriceDTO 负责发布PriceDTO相关处理。
func publishPriceDTO(req PublishItemRequest) map[string]any {
	// out 保存out，供当前处理流程使用
	out := map[string]any{"priceInCent": strconv.FormatInt(req.PriceCents, 10)}
	if req.OriginalPriceCents > 0 {
		out["origPriceInCent"] = strconv.FormatInt(req.OriginalPriceCents, 10)
	}
	return out
}

// postageDTO 负责postageDTO相关处理。
func postageDTO(req PublishItemRequest) map[string]any {
	// out 保存out，供当前处理流程使用
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

// publishLabels 负责发布Labels相关处理。
func publishLabels(category map[string]any) []any {
	// cards 保存卡密列表，供当前处理流程使用
	cards, _ := category["cardList"].([]any)
	// out 保存out，供当前处理流程使用
	out := []any{}
	// rawCard 表示当前遍历过程中的原始卡密
	for _, rawCard := range cards {
		// card 保存卡密，供当前处理流程使用
		card := mapFromAny(rawCard)
		// cardData 保存卡密数据，供当前处理流程使用
		cardData := mapFromAny(card["cardData"])
		if cardData == nil {
			continue
		}
		// values 保存values，供当前处理流程使用
		values, _ := cardData["valuesList"].([]any)
		// rawValue 表示当前遍历过程中的原始值
		for _, rawValue := range values {
			// value 保存值，供当前处理流程使用
			value := mapFromAny(rawValue)
			if !publishLabelSelected(value["isClicked"]) {
				continue
			}
			// propertyID 保存propertyID，供当前处理流程使用
			propertyID := mtopString(cardData["propertyId"])
			// propertyName 保存property名称，供当前处理流程使用
			propertyName := mtopString(cardData["propertyName"])
			// channelCatID 保存渠道CatID，供当前处理流程使用
			channelCatID := mtopString(value["channelCatId"])
			// catName 保存cat名称，供当前处理流程使用
			catName := mtopString(value["catName"])
			out = append(out, map[string]any{
				"channelCateName": catName,
				"valueId":         nil,
				"channelCateId":   channelCatID,
				"valueName":       nil,
				"tbCatId":         mtopString(value["tbCatId"]),
				"subPropertyId":   nil,
				"labelType":       "common",
				"subValueId":      nil,
				"labelId":         nil,
				"propertyName":    propertyName,
				"isUserClick":     "1",
				"isUserCancel":    nil,
				"from":            "newPublishChoice",
				"propertyId":      propertyID,
				"labelFrom":       "newPublish",
				"text":            catName,
				"properties":      propertyID + "##" + propertyName + ":" + channelCatID + "##" + catName,
			})
			break
		}
	}
	return out
}

// publishLabelSelected 负责发布LabelSelected相关处理。
func publishLabelSelected(value any) bool {
	if // selected、ok 保存selected、ok，供当前处理流程使用
	selected, ok := value.(bool); ok {
		return selected
	}
	switch strings.ToLower(strings.TrimSpace(mtopString(value))) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// classifyPublishError 负责classify发布错误相关处理。
func classifyPublishError(ret []string, decoded map[string]any) error {
	// bodyBytes 保存请求体Bytes，供当前处理流程使用
	bodyBytes, _ := json.Marshal(decoded)
	// body 保存请求体，供当前处理流程使用
	body := string(bodyBytes)
	// joined 保存joined，供当前处理流程使用
	joined := strings.ToLower(strings.Join(append(ret, body), " "))
	if isTokenExpiredRet(ret) || strings.Contains(joined, "login") || strings.Contains(joined, "session") {
		return &PublishError{Code: PublishErrorTokenExpired, Ret: ret, Body: body}
	}
	// stockTerms 保存stockTerms，供当前处理流程使用
	stockTerms := []string{"库存", "数量", "多库存", "多件", "quantity", "stock", "inventory"}
	// permissionTerms 保存permissionTerms，供当前处理流程使用
	permissionTerms := []string{"权限", "未开通", "不支持", "没有", "无法", "permission", "forbidden", "not allow", "not support"}
	if containsAny(joined, stockTerms) && containsAny(joined, permissionTerms) {
		return &PublishError{Code: PublishErrorStockPermissionMissing, Ret: ret, Body: body}
	}
	return &PublishError{Code: PublishErrorUnknown, Ret: ret, Body: body}
}

// containsAny 负责containsAny相关处理。
func containsAny(s string, terms []string) bool {
	// term 表示当前遍历过程中的term
	for _, term := range terms {
		if strings.Contains(s, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

// retFromDecoded 负责retFromDecoded相关处理。
func retFromDecoded(decoded map[string]any) []string {
	// raw 保存原始，供当前处理流程使用
	raw, _ := decoded["ret"].([]any)
	// out 保存out，供当前处理流程使用
	out := make([]string, 0, len(raw))
	// r 表示当前遍历过程中的r
	for _, r := range raw {
		out = append(out, mtopString(r))
	}
	return out
}

// mapFromAny 负责mapFromAny相关处理。
func mapFromAny(v any) map[string]any {
	if // m、ok 保存m、ok，供当前处理流程使用
	m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// parsePix 负责parsePix相关处理。
func parsePix(pix string) (int, int) {
	// parts 保存parts，供当前处理流程使用
	parts := strings.Split(pix, "x")
	if len(parts) != 2 {
		return 0, 0
	}
	// w 保存w，供当前处理流程使用
	w, _ := strconv.Atoi(parts[0])
	// h 保存h，供当前处理流程使用
	h, _ := strconv.Atoi(parts[1])
	return w, h
}

// findStringDeep 负责findStringDeep相关处理。
func findStringDeep(v any, keys ...string) string {
	// keySet 保存keySet，供当前处理流程使用
	keySet := map[string]struct{}{}
	// k 表示当前遍历过程中的k
	for _, k := range keys {
		keySet[k] = struct{}{}
	}
	// walk 保存walk，供当前处理流程使用
	var walk func(any) string
	walk = func(cur any) string {
		switch // x 保存x，供当前处理流程使用
		x := cur.(type) {
		case map[string]any:
			// k、v 表示当前遍历过程中的k、v
			for k, v := range x {
				if // ok 保存ok，供当前处理流程使用
				_, ok := keySet[k]; ok {
					if // s 保存s，供当前处理流程使用
					s := mtopString(v); s != "" {
						return s
					}
				}
			}
			// v 表示当前遍历过程中的v
			for _, v := range x {
				if // s 保存s，供当前处理流程使用
				s := walk(v); s != "" {
					return s
				}
			}
		case []any:
			// v 表示当前遍历过程中的v
			for _, v := range x {
				if // s 保存s，供当前处理流程使用
				s := walk(v); s != "" {
					return s
				}
			}
		}
		return ""
	}
	return walk(v)
}

// centsText 负责cents文本相关处理。
func centsText(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64)
}

// escapeMultipartFilename 负责escapeMultipartFilename相关处理。
func escapeMultipartFilename(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}
