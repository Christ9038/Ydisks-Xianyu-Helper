package items

import (
	"archive/zip"
	"bytes"
	"testing"
)

// TestXLSXSharedStringsReadsRichTextAndMissingPart 覆盖 XLSX 共享字符串读取与缺失部件路径。
func TestXLSXSharedStringsReadsRichTextAndMissingPart(t *testing.T) {
	// buffer 保存内存中构造的 XLSX 压缩包。
	var buffer bytes.Buffer
	// writer 保存压缩包写入器。
	writer := zip.NewWriter(&buffer)
	// file、fileErr 保存共享字符串 XML 条目及创建错误。
	file, fileErr := writer.Create("xl/sharedStrings.xml")
	if fileErr != nil {
		t.Fatal(fileErr)
	}
	// writeErr 保存共享字符串 XML 写入错误。
	if _, writeErr := file.Write([]byte(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>普通文本</t></si><si><r><t>富</t></r><r><t>文本</t></r></si></sst>`)); writeErr != nil {
		t.Fatal(writeErr)
	}
	// closeErr 保存压缩包完成错误。
	closeErr := writer.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// archive 保存可供解析器读取的压缩包。
	archive, archiveErr := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if archiveErr != nil {
		t.Fatal(archiveErr)
	}
	// values、valuesErr 保存共享字符串解析结果和错误。
	values, valuesErr := xlsxSharedStrings(archive)
	if valuesErr != nil || len(values) != 2 || values[0] != "普通文本" || values[1] != "富文本" {
		t.Fatalf("shared strings=%v err=%v", values, valuesErr)
	}
	// emptyArchive 保存不含共享字符串部件的压缩包。
	var emptyBuffer bytes.Buffer
	// emptyWriter 保存空压缩包写入器。
	emptyWriter := zip.NewWriter(&emptyBuffer)
	// emptyCloseErr 保存空压缩包完成错误。
	emptyCloseErr := emptyWriter.Close()
	if emptyCloseErr != nil {
		t.Fatal(emptyCloseErr)
	}
	// emptyArchiveErr 保存空压缩包读取错误。
	emptyArchive, emptyArchiveErr := zip.NewReader(bytes.NewReader(emptyBuffer.Bytes()), int64(emptyBuffer.Len()))
	if emptyArchiveErr != nil {
		t.Fatal(emptyArchiveErr)
	}
	// missingValues、missingErr 保存缺失共享字符串部件的返回结果和错误。
	missingValues, missingErr := xlsxSharedStrings(emptyArchive)
	if missingErr != nil || missingValues != nil {
		t.Fatalf("missing shared strings=%v err=%v", missingValues, missingErr)
	}
}
