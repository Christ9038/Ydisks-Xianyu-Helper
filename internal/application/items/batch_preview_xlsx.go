package items

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"path"
	"strings"
)

// xlsxWorkbook 保存 workbook.xml 中声明的用户可见或隐藏工作表；批量铺货只接受其中恰好一个表。
type xlsxWorkbook struct {
	// Sheets 保存工作簿声明的全部工作表，隐藏表同样必须参与计数以避免误读历史数据。
	Sheets struct {
		// Items 保存每个工作表的关系标识和展示名称。
		Items []xlsxWorkbookSheet `xml:"sheet"`
	} `xml:"sheets"`
}

// xlsxWorkbookSheet 保存 workbook.xml 中单个工作表到关系文件的引用。
type xlsxWorkbookSheet struct {
	// Name 是工作表名称，仅用于结构校验与诊断，不会写入预检结果。
	Name string `xml:"name,attr"`
	// RelationshipID 是 workbook 关系文件中定位实际 XML 分区的标识。
	RelationshipID string `xml:"id,attr"`
}

// xlsxRelationships 保存 workbook.xml.rels 中的关系集合。
type xlsxRelationships struct {
	// Items 保存每一条关系及其目标文件。
	Items []xlsxRelationship `xml:"Relationship"`
}

// xlsxRelationship 保存工作簿关系文件的一条内部或外部关系。
type xlsxRelationship struct {
	// ID 是 workbook.xml 中 sheet 元素引用的唯一关系标识。
	ID string `xml:"Id,attr"`
	// Target 是相对 workbook.xml 所在目录的目标分区路径。
	Target string `xml:"Target,attr"`
	// TargetMode 为 External 时表示目标不在上传压缩包内，不能作为本地工作表读取。
	TargetMode string `xml:"TargetMode,attr"`
}

// xlsxSingleWorksheet 从 workbook 元数据定位唯一工作表，并拒绝包含多个表的上传文件。
func xlsxSingleWorksheet(archive *zip.Reader) (*zip.File, error) {
	// workbookPart 和 err 分别保存 workbook.xml 的受限内容及读取失败原因。
	workbookPart, err := readXLSXPartNamed(archive, "xl/workbook.xml")
	if err != nil {
		return nil, errors.New("XLSX 工作表结构无效")
	}
	// workbook 保存 workbook.xml 反序列化后的工作表声明。
	var workbook xlsxWorkbook
	if xml.Unmarshal(workbookPart, &workbook) != nil || len(workbook.Sheets.Items) == 0 {
		return nil, errors.New("XLSX 工作表结构无效")
	}
	if len(workbook.Sheets.Items) != 1 {
		return nil, errors.New("批量铺货 XLSX 仅支持一个工作表，请删除多余工作表或另存为单工作表 CSV/XLSX 后重试")
	}
	// sheetDeclaration 保存唯一工作表的关系引用。
	sheetDeclaration := workbook.Sheets.Items[0]
	if strings.TrimSpace(sheetDeclaration.Name) == "" || strings.TrimSpace(sheetDeclaration.RelationshipID) == "" {
		return nil, errors.New("XLSX 工作表结构无效")
	}
	// relationshipsPart 和 err 分别保存关系文件内容及读取失败原因。
	relationshipsPart, err := readXLSXPartNamed(archive, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, errors.New("XLSX 工作表结构无效")
	}
	// relationships 保存关系文件反序列化后的目标列表。
	var relationships xlsxRelationships
	if xml.Unmarshal(relationshipsPart, &relationships) != nil {
		return nil, errors.New("XLSX 工作表结构无效")
	}
	// relationship 保存与唯一工作表标识匹配的内部关系。
	var relationship *xlsxRelationship
	// index 表示当前关系在关系集合中的位置。
	for index := range relationships.Items {
		// candidate 保存当前关系的可修改引用，避免循环变量地址失效。
		candidate := &relationships.Items[index]
		if candidate.ID == sheetDeclaration.RelationshipID {
			relationship = candidate
			break
		}
	}
	if relationship == nil || strings.EqualFold(strings.TrimSpace(relationship.TargetMode), "External") {
		return nil, errors.New("XLSX 工作表结构无效")
	}
	// sheetPath 和 valid 分别保存归一化后的内部 XML 路径及安全校验结果。
	sheetPath, valid := xlsxWorkbookTargetPath(relationship.Target)
	if !valid {
		return nil, errors.New("XLSX 工作表结构无效")
	}
	// file 表示压缩包内与 workbook 关系精确匹配的工作表 XML。
	for _, file := range archive.File {
		if file.Name == sheetPath {
			return file, nil
		}
	}
	return nil, errors.New("XLSX 工作表结构无效")
}

// readXLSXPartNamed 在压缩包中读取指定 XML 分区，并继续使用统一的解压大小限制。
func readXLSXPartNamed(archive *zip.Reader, partName string) ([]byte, error) {
	// file 表示当前检查的 ZIP 条目。
	for _, file := range archive.File {
		if file.Name == partName {
			return readXLSXPart(file)
		}
	}
	return nil, errors.New("XLSX 分区不存在")
}

// xlsxWorkbookTargetPath 将 workbook 相对关系目标解析为受限的 xl 内部路径。
func xlsxWorkbookTargetPath(target string) (string, bool) {
	// trimmed 保存移除外围空白后的关系目标。
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", false
	}
	// candidate 保存兼容绝对与相对关系目标后的未清理路径。
	candidate := trimmed
	if strings.HasPrefix(candidate, "/") {
		candidate = strings.TrimPrefix(candidate, "/")
	} else {
		candidate = path.Join("xl", candidate)
	}
	// normalized 保存使用 ZIP 正斜杠规则清理后的内部路径。
	normalized := path.Clean(candidate)
	if !strings.HasPrefix(normalized, "xl/") || !strings.HasSuffix(normalized, ".xml") {
		return "", false
	}
	return normalized, true
}
