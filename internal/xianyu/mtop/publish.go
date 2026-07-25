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

const (
	UploadMediaAPI     = "https://stream-upload.goofish.com/api/upload.api"
	PublishItemAPI     = "https://h5api.m.goofish.com/h5/mtop.idle.pc.idleitem.publish/1.0/"
	RecommendItemAPI   = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.kgraph.property.recommend/2.0/"
	DefaultLocationAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.local.poi.get/1.0/"
)

type PublishErrorCode string

const (
	PublishErrorUnknown                PublishErrorCode = "publish_failed"
	PublishErrorTokenExpired           PublishErrorCode = "auth_expired"
	PublishErrorStockPermissionMissing PublishErrorCode = "stock_permission_missing"
)

type PublishError struct {
	Code PublishErrorCode
	Ret  []string
	Body string
}

func (e *PublishError) Error() string {
	if len(e.Ret) > 0 {
		return strings.Join(e.Ret, "; ")
	}
	if e.Body != "" {
		return truncate(e.Body, 240)
	}
	return string(e.Code)
}

type PublishImage struct {
	Filename    string
	ContentType string
	Data        []byte
}

type PublishItemRequest struct {
	Title              string
	Description        string
	PriceCents         int64
	OriginalPriceCents int64
	Quantity           int
	PostageMode        string
	PostageCents       int64
	// Virtual 表示商品只通过系统的虚拟发货流程交付，不需要实物发货地址。
	Virtual bool
	Images  []PublishImage
}

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

type uploadedImage struct {
	URL    string
	Width  int
	Height int
}

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
	currentCookies := cookiesStr
	if session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	uploaded := make([]uploadedImage, 0, len(req.Images))
	for _, img := range req.Images {
		res, updated, err := c.uploadPublishImage(ctx, currentCookies, img)
		if err != nil {
			return nil, err
		}
		if updated != "" {
			currentCookies = updated
		}
		uploaded = append(uploaded, res)
	}
	category, updated, err := c.recommendPublishCategory(ctx, currentCookies, req.Title, req.Description, uploaded)
	if err != nil {
		return nil, err
	}
	if updated != "" {
		currentCookies = updated
	}
	var location map[string]any
	if !req.Virtual {
		location, updated, err = c.defaultPublishLocation(ctx, currentCookies)
		if err != nil {
			return nil, err
		}
		if updated != "" {
			currentCookies = updated
		}
	}
	return c.publishItemOnce(ctx, currentCookies, req, uploaded, category, location)
}

func (c *ClientImpl) uploadPublishImage(ctx context.Context, cookiesStr string, img PublishImage) (uploadedImage, string, error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	if img.ContentType == "" {
		img.ContentType = "application/octet-stream"
	}
	if img.Filename == "" {
		img.Filename = "image"
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf("form-data; name=\"file\"; filename=\"%s\"", escapeMultipartFilename(filepath.Base(img.Filename))))
	header.Set("Content-Type", img.ContentType)
	part, err := mw.CreatePart(header)
	if err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	if _, err := part.Write(img.Data); err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	if err := mw.Close(); err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	u, _ := url.Parse(UploadMediaAPI)
	q := u.Query()
	q.Set("floderId", "0")
	q.Set("appkey", "xy_chat")
	q.Set("_input_charset", "utf-8")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return uploadedImage{}, cookiesStr, err
	}
	_, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", u.String())
	req.Header.Set("content-type", mw.FormDataContentType())
	setBrowserHeaders(req, requestCookies)
	req.Header.Set("accept", "*/*")
	resp, err := hc.Do(req)
	if err != nil {
		return uploadedImage{}, cookiesStr, fmt.Errorf("上传商品图片失败: %w", err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return uploadedImage{}, updated, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return uploadedImage{}, updated, fmt.Errorf("上传商品图片失败: http=%d body=%s", resp.StatusCode, truncate(string(raw), 240))
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return uploadedImage{}, updated, fmt.Errorf("解析图片上传响应失败: %w (body=%s)", err, truncate(string(raw), 240))
	}
	obj := mapFromAny(decoded["object"])
	if obj == nil {
		obj = mapFromAny(decoded["data"])
	}
	imageURL := mtopString(obj["url"])
	if imageURL == "" {
		imageURL = mtopString(decoded["url"])
	}
	if imageURL == "" {
		return uploadedImage{}, updated, fmt.Errorf("图片上传响应缺少 url: %s", truncate(string(raw), 240))
	}
	width, height := parsePix(mtopString(obj["pix"]))
	if width == 0 || height == 0 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data)); err == nil {
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

func (c *ClientImpl) recommendPublishCategory(ctx context.Context, cookiesStr, title, desc string, images []uploadedImage) (map[string]any, string, error) {
	imageInfos := make([]any, 0, len(images))
	for i, img := range images {
		imageInfos = append(imageInfos, publishImagePayload(img, i == 0))
	}
	data := map[string]any{
		"title":        title,
		"lockCpv":      false,
		"multiSKU":     false,
		"publishScene": "mainPublish",
		"scene":        "newPublishChoice",
		"description":  desc,
		"imageInfos":   imageInfos,
		"uniqueCode":   strconv.FormatInt(time.Now().UnixMicro(), 10),
	}
	decoded, updated, err := c.callMTop(ctx, cookiesStr, RecommendItemAPI, "mtop.taobao.idle.kgraph.property.recommend", "2.0", "a21ybx.publish.0.0", "a21ybx.item.sidebar.1.67321598K9Vgx8", "67321598K9Vgx8", data)
	if err != nil {
		return nil, updated, err
	}
	if !hasMTopSuccess(retFromDecoded(decoded)) {
		return nil, updated, classifyPublishError(retFromDecoded(decoded), decoded)
	}
	dataMap := mapFromAny(decoded["data"])
	if dataMap == nil {
		return nil, updated, fmt.Errorf("类目推荐响应缺少 data")
	}
	cat := mapFromAny(dataMap["categoryPredictResult"])
	if cat == nil || mtopString(cat["catId"]) == "" {
		return nil, updated, fmt.Errorf("未能自动识别商品类目，请调整标题或图片后重试")
	}
	return dataMap, updated, nil
}

func (c *ClientImpl) defaultPublishLocation(ctx context.Context, cookiesStr string) (map[string]any, string, error) {
	data := map[string]any{"longitude": 118.78248347393424, "latitude": 31.91629189813543}
	decoded, updated, err := c.callMTop(ctx, cookiesStr, DefaultLocationAPI, "mtop.taobao.idle.local.poi.get", "1.0", "a21ybx.publish.0.0", "a21ybx.item.sidebar.1.38262218ame5nr", "38262218ame5nr", data)
	if err != nil {
		return nil, updated, err
	}
	if !hasMTopSuccess(retFromDecoded(decoded)) {
		return nil, updated, classifyPublishError(retFromDecoded(decoded), decoded)
	}
	dataMap := mapFromAny(decoded["data"])
	addresses, _ := dataMap["commonAddresses"].([]any)
	if len(addresses) == 0 {
		return nil, updated, fmt.Errorf("账号缺少默认发货地址/定位信息，无法发布商品")
	}
	loc := mapFromAny(addresses[0])
	if loc == nil {
		return nil, updated, fmt.Errorf("默认地址格式异常，无法发布商品")
	}
	return loc, updated, nil
}

func (c *ClientImpl) publishItemOnce(ctx context.Context, cookiesStr string, req PublishItemRequest, images []uploadedImage, category, location map[string]any) (*PublishItemResult, error) {
	imagePayloads := make([]any, 0, len(images))
	for i, img := range images {
		imagePayloads = append(imagePayloads, publishImagePayload(img, i == 0))
	}
	cat := mapFromAny(category["categoryPredictResult"])
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
	if !req.Virtual {
		data["itemAddrDTO"] = map[string]any{
			"area":       location["area"],
			"city":       location["city"],
			"divisionId": location["divisionId"],
			"gps":        fmt.Sprintf("%s,%s", mtopString(location["longitude"]), mtopString(location["latitude"])),
			"poiId":      location["poiId"],
			"poiName":    location["poi"],
			"prov":       location["prov"],
		}
	}
	decoded, updated, err := c.callMTop(ctx, cookiesStr, PublishItemAPI, "mtop.idle.pc.idleitem.publish", "1.0", "a21ybx.publish.0.0", "a21ybx.home.sidebar.1.46413da6EPl7v5", "46413da6EPl7v5", data)
	if err != nil {
		return nil, err
	}
	ret := retFromDecoded(decoded)
	if !hasMTopSuccess(ret) {
		return nil, classifyPublishError(ret, decoded)
	}
	dataMap := mapFromAny(decoded["data"])
	itemID := findStringDeep(dataMap, "itemId", "item_id", "id", "itemID")
	if itemID == "" {
		itemID = findStringDeep(decoded, "itemId", "item_id", "itemID")
	}
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

func (c *ClientImpl) callMTop(ctx context.Context, cookiesStr, endpoint, api, version, spmCnt, spmPre, logID string, data any) (map[string]any, string, error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	rawData, _ := json.Marshal(data)
	dataVal := string(rawData)
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", endpoint)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	query := buildMTopQuery(api, version, t, sign, spmCnt, spmPre, logID)
	body := "data=" + url.QueryEscape(dataVal)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, cookiesStr, fmt.Errorf("%s 请求失败: %w", api, err)
	}
	defer resp.Body.Close()
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, updated, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, updated, fmt.Errorf("解析 %s 响应失败: %w (body=%s)", api, err, truncate(string(raw), 300))
	}
	return decoded, updated, nil
}

func buildMTopQuery(api, version, t, sign, spmCnt, spmPre, logID string) string {
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
	var b strings.Builder
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

func setBrowserHeaders(req *http.Request, cookiesStr string) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("origin", "https://www.goofish.com")
	req.Header.Set("referer", "https://www.goofish.com/")
	req.Header.Set("cookie", cookiesStr)
}

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

func publishPriceDTO(req PublishItemRequest) map[string]any {
	out := map[string]any{"priceInCent": strconv.FormatInt(req.PriceCents, 10)}
	if req.OriginalPriceCents > 0 {
		out["origPriceInCent"] = strconv.FormatInt(req.OriginalPriceCents, 10)
	}
	return out
}

func postageDTO(req PublishItemRequest) map[string]any {
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

func publishLabels(category map[string]any) []any {
	cards, _ := category["cardList"].([]any)
	out := []any{}
	for _, rawCard := range cards {
		card := mapFromAny(rawCard)
		cardData := mapFromAny(card["cardData"])
		if cardData == nil {
			continue
		}
		values, _ := cardData["valuesList"].([]any)
		for _, rawValue := range values {
			value := mapFromAny(rawValue)
			clicked, _ := value["isClicked"].(bool)
			if !clicked {
				continue
			}
			propertyID := mtopString(cardData["propertyId"])
			propertyName := mtopString(cardData["propertyName"])
			channelCatID := mtopString(value["channelCatId"])
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

func classifyPublishError(ret []string, decoded map[string]any) error {
	bodyBytes, _ := json.Marshal(decoded)
	body := string(bodyBytes)
	joined := strings.ToLower(strings.Join(append(ret, body), " "))
	if isTokenExpiredRet(ret) || strings.Contains(joined, "login") || strings.Contains(joined, "session") {
		return &PublishError{Code: PublishErrorTokenExpired, Ret: ret, Body: body}
	}
	stockTerms := []string{"库存", "数量", "多库存", "多件", "quantity", "stock", "inventory"}
	permissionTerms := []string{"权限", "未开通", "不支持", "没有", "无法", "permission", "forbidden", "not allow", "not support"}
	if containsAny(joined, stockTerms) && containsAny(joined, permissionTerms) {
		return &PublishError{Code: PublishErrorStockPermissionMissing, Ret: ret, Body: body}
	}
	return &PublishError{Code: PublishErrorUnknown, Ret: ret, Body: body}
}

func containsAny(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func retFromDecoded(decoded map[string]any) []string {
	raw, _ := decoded["ret"].([]any)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		out = append(out, mtopString(r))
	}
	return out
}

func mapFromAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func parsePix(pix string) (int, int) {
	parts := strings.Split(pix, "x")
	if len(parts) != 2 {
		return 0, 0
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	return w, h
}

func findStringDeep(v any, keys ...string) string {
	keySet := map[string]struct{}{}
	for _, k := range keys {
		keySet[k] = struct{}{}
	}
	var walk func(any) string
	walk = func(cur any) string {
		switch x := cur.(type) {
		case map[string]any:
			for k, v := range x {
				if _, ok := keySet[k]; ok {
					if s := mtopString(v); s != "" {
						return s
					}
				}
			}
			for _, v := range x {
				if s := walk(v); s != "" {
					return s
				}
			}
		case []any:
			for _, v := range x {
				if s := walk(v); s != "" {
					return s
				}
			}
		}
		return ""
	}
	return walk(v)
}

func centsText(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64)
}

func escapeMultipartFilename(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}
