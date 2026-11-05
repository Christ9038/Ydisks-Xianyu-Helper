package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeFile 在临时仓库根 root 下写入相对路径 rel 的文件，内容为 content；失败直接终止测试。
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	// full 是文件的绝对路径。
	full := filepath.Join(root, rel)
	// err 在本函数内依次承接建目录与写文件的失败原因。
	var err error
	if err = os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCheckTokenRule 覆盖令牌规则的通过、缺失、前缀豁免、豁免令牌与无令牌四条路径。
func TestCheckTokenRule(t *testing.T) {
	// root 是临时仓库根。
	root := t.TempDir()
	writeFile(t, root, "src.go", `flag("alpha") flag("beta") flag("PG_USER") flag("skip")`)
	writeFile(t, root, "docs/wiki/a.md", "`-alpha` 和 PG_ 前缀")
	// rule 是基础规则，每个用例在其上调整。
	rule := tokenRule{
		name: "t", sources: []string{"src.go"}, pattern: regexp.MustCompile(`flag\("([^"]+)"\)`),
		docs: []string{"a.md"}, format: "`-%s`",
		prefixOnly: []string{"PG_USER"}, exempt: map[string]bool{"skip": true},
	}
	// got、err 是规则执行结果。
	got, err := checkTokenRule(root, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "beta") {
		t.Fatalf("应只报告缺失的 beta: %v", got)
	}
	rule.pattern = regexp.MustCompile(`nomatch\("([^"]+)"\)`)
	if got, _ = checkTokenRule(root, rule); len(got) == 0 {
		t.Fatal("未提取到令牌必须报违规")
	}
	rule.sources = []string{"missing.go"}
	if got, _ = checkTokenRule(root, rule); len(got) == 0 {
		t.Fatal("源码缺失必须报违规")
	}
	rule.sources = []string{"src.go"}
	rule.docs = []string{"nope.md"}
	rule.pattern = regexp.MustCompile(`flag\("([^"]+)"\)`)
	if _, err = checkTokenRule(root, rule); err == nil {
		t.Fatal("文档缺失必须返回错误")
	}
}

// TestCheckNumberRule 覆盖数值规则：千分位写法通过、数值缺失报错、重复数值只报一次、正则失效与源码缺失。
func TestCheckNumberRule(t *testing.T) {
	// root 是临时仓库根。
	root := t.TempDir()
	writeFile(t, root, "src.go", "const x = 12000, 12000, 7\n")
	writeFile(t, root, "docs/wiki/a.md", "Limit is 12,000 items\nother 99\n")
	// rule 是数值规则，三个捕获组依次是 12000、12000、7。
	rule := numberRule{name: "n", source: "src.go", pattern: regexp.MustCompile(`const x = (\d+), (\d+), (\d+)`),
		docs: []docKeyword{{"a.md", "items"}}}
	// got、err 是执行结果。
	got, err := checkNumberRule(root, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "数值 7") {
		t.Fatalf("应只报告缺失的 7，且 12000 通过千分位匹配、不重复: %v", got)
	}
	rule.pattern = regexp.MustCompile(`nomatch (\d+)`)
	if got, _ = checkNumberRule(root, rule); len(got) != 1 {
		t.Fatalf("正则失效必须报告: %v", got)
	}
	rule.source = "missing.go"
	if _, err = checkNumberRule(root, rule); err == nil {
		t.Fatal("源码缺失必须报错")
	}
}

// TestCheckForbidden 覆盖回归守卫命中与未命中。
func TestCheckForbidden(t *testing.T) {
	// root 是临时仓库根。
	root := t.TempDir()
	writeFile(t, root, "docs/wiki/a.md", "bad phrase here")
	// got、err 是执行结果。
	got, err := checkForbidden(root, []forbiddenRule{{"a.md", "bad phrase", "r"}, {"a.md", "fine", "r"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if _, err = checkForbidden(root, []forbiddenRule{{"x.md", "p", "r"}}); err == nil {
		t.Fatal("文档缺失必须报错")
	}
}

// TestCheckTemplateVariablesAreSupported 覆盖内置、命名空间放行与未知变量。
func TestCheckTemplateVariablesAreSupported(t *testing.T) {
	// root 是临时仓库根。
	root := t.TempDir()
	writeFile(t, root, "docs/wiki/a.md", "{{order_id}} {{cards.x}} {{custom.y}} {{delivery.order_id}} {{library_image:1}} {{buyer_nick}}")
	// got、err 是执行结果。
	got, err := checkTemplateVariablesAreSupported(root, []string{"a.md"}, map[string]bool{"order_id": true})
	if err != nil || len(got) != 1 || !strings.Contains(got[0], "buyer_nick") {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if _, err = checkTemplateVariablesAreSupported(root, []string{"x.md"}, nil); err == nil {
		t.Fatal("文档缺失必须报错")
	}
}

// TestLanguageNeutral 覆盖语言无关判定的各分支。
func TestLanguageNeutral(t *testing.T) {
	// cases 把行内代码映射到期望判定。
	cases := map[string]bool{
		"XIANYU_PORT": true, "中文": false, "word": false, "0": false,
		"<id>": false, `"q"`: false, "http://x/1": false, "a=1": true,
	}
	for span, want := range cases { // span 是被判定的行内代码。
		if got := languageNeutral(span); got != want {
			t.Errorf("%q: got %v want %v", span, got, want)
		}
	}
}

// TestCheckParity 覆盖中英文对齐：一致通过、缺失报告、文档缺失报错。
func TestCheckParity(t *testing.T) {
	// root 是临时仓库根。
	root := t.TempDir()
	writeFile(t, root, "docs/wiki/z.md", "`A_1` `B_2`")
	writeFile(t, root, "docs/wiki/e.md", "`A_1`")
	// got、err 是执行结果。
	got, err := checkParity(root, []zhEnPair{{"z.md", "e.md"}})
	if err != nil || len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "B_2") {
		t.Fatalf("got=%v err=%v", got, err)
	}
	writeFile(t, root, "docs/wiki/e.md", "`A_1` `B_2`")
	if got, _ = checkParity(root, []zhEnPair{{"z.md", "e.md"}}); len(got) != 0 {
		t.Fatalf("一致时不应报告: %v", got)
	}
	if _, err = checkParity(root, []zhEnPair{{"z.md", "none.md"}}); err == nil {
		t.Fatal("文档缺失必须报错")
	}
}

// linuxFixture 写入 Linux 打包夹具；user 是单元里 User/Group 的值。
func linuxFixture(t *testing.T, root, user string) {
	t.Helper()
	writeFile(t, root, "packaging/linux/install.sh", "APP_NAME=\"app\"\n")
	writeFile(t, root, "packaging/linux/ydisks-xianyu-helper.service",
		"User="+user+"\nGroup="+user+"\nWorkingDirectory=/var/lib/app\nEnvironmentFile=/etc/app/config.env\nReadWritePaths=/var/log/app\nExecStart=/opt/app/xianyu-server\n")
}

// TestCheckLinuxPackaging 覆盖一致、用户不一致、缺少 Group、缺少 APP_NAME 与文件缺失。
func TestCheckLinuxPackaging(t *testing.T) {
	// root 是临时仓库根。
	root := t.TempDir()
	// got 是每次检查的违规，逐步复用。
	var got []string
	// err 是每次检查的执行失败原因，逐步复用。
	var err error
	linuxFixture(t, root, "app")
	if got, err = checkLinuxPackaging(root); err != nil || len(got) != 0 {
		t.Fatalf("一致时不应报告: %v %v", got, err)
	}
	linuxFixture(t, root, "other")
	if got, _ = checkLinuxPackaging(root); len(got) != 2 {
		t.Fatalf("User 与 Group 都应报告: %v", got)
	}
	writeFile(t, root, "packaging/linux/ydisks-xianyu-helper.service", "User=app\n")
	if got, _ = checkLinuxPackaging(root); len(got) < 2 {
		t.Fatalf("缺少 Group 与路径应报告: %v", got)
	}
	writeFile(t, root, "packaging/linux/install.sh", "x\n")
	if got, _ = checkLinuxPackaging(root); len(got) != 1 {
		t.Fatalf("缺少 APP_NAME 应报告: %v", got)
	}
	if _, err = checkLinuxPackaging(t.TempDir()); err == nil {
		t.Fatal("文件缺失必须报错")
	}
}

// TestCheckRepository 对真实仓库运行完整检查，保证规则表与当前文档同步。
func TestCheckRepository(t *testing.T) {
	// violations、err 是完整检查结果。
	violations, err := check("../..")
	if err != nil || len(violations) != 0 {
		t.Fatalf("violations=%v err=%v", violations, err)
	}
}
