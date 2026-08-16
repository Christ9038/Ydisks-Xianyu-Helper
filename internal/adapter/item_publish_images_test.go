package adapter

import (
	"context"
	"testing"
)

// TestLoadBatchPublishImagesConvertsLocalAndRemoteRefs 验证本地与远程图片回调均转换为平台模型。
func TestLoadBatchPublishImagesConvertsLocalAndRemoteRefs(t *testing.T) {
	// localCalls、remoteCalls 记录两类图片读取回调的调用次数。
	localCalls, remoteCalls := 0, 0
	// readLocal 返回本地图片测试数据和稳定文件名。
	readLocal := func(_ string, ref string) ([]byte, string, string, error) {
		localCalls++
		return []byte("local"), "image/png", ref, nil
	}
	// downloadRemote 返回远程图片测试数据。
	downloadRemote := func(_ context.Context, _ string) ([]byte, string, error) {
		remoteCalls++
		return []byte("remote"), "image/jpeg", nil
	}
	// images、loadErr 保存图片转换结果和执行错误。
	images, loadErr := LoadBatchPublishImages(context.Background(), "/tmp/upload", `["local.png","https://example.test/remote.jpg?x=1"]`, readLocal, downloadRemote)
	if loadErr != nil {
		t.Fatalf("加载图片失败: %v", loadErr)
	}
	if len(images) != 2 || localCalls != 1 || remoteCalls != 1 {
		t.Fatalf("图片回调或结果异常: images=%+v local=%d remote=%d", images, localCalls, remoteCalls)
	}
	if images[1].Filename != "remote.jpg" || images[1].ContentType != "image/jpeg" {
		t.Fatalf("远程图片元数据异常: %+v", images[1])
	}
}

// TestLoadBatchPublishImagesRejectsInvalidInput 验证缺失端口、非法 JSON 和空图片列表均明确失败。
func TestLoadBatchPublishImagesRejectsInvalidInput(t *testing.T) {
	// readLocal、downloadRemote 是不应被非法输入调用的测试回调。
	readLocal := func(string, string) ([]byte, string, string, error) { return nil, "", "", nil }
	// downloadRemote 是远程图片测试回调。
	downloadRemote := func(context.Context, string) ([]byte, string, error) { return nil, "", nil }
	// cases 保存不同非法输入及其预期错误断言。
	cases := []struct {
		name       string
		imagesJSON string
		readLocal  ReadPublishImageFile
		wantErr    bool
	}{
		{name: "missing port", imagesJSON: `[]`, wantErr: true},
		{name: "bad json", imagesJSON: "not-json", readLocal: readLocal, wantErr: true},
		{name: "empty", imagesJSON: `[]`, readLocal: readLocal, wantErr: true},
	}
	// testCase 描述当前非法图片输入分支。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// remote 是当前分支可用的远程下载回调，缺失端口分支故意传入 nil。
			remote := downloadRemote
			if testCase.readLocal == nil {
				remote = nil
			}
			// _, loadErr 保存当前非法输入的执行错误。
			_, loadErr := LoadBatchPublishImages(context.Background(), "/tmp/upload", testCase.imagesJSON, testCase.readLocal, remote)
			if (loadErr != nil) != testCase.wantErr {
				t.Fatalf("错误断言不符: %v", loadErr)
			}
		})
	}
}
