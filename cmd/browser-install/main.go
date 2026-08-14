// Command browser-install prepares the Playwright driver and Chromium runtime.
// It intentionally does not change the server's browser launch behavior.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mxschmitt/playwright-go"
)

// main 负责main相关处理。
func main() {
	// driverDir 保存driverDir，供当前处理流程使用
	driverDir := flag.String("driver-dir", "", "Playwright driver directory")
	// browserDir 保存浏览器Dir，供当前处理流程使用
	browserDir := flag.String("browser-dir", "", "Playwright browser cache directory")
	// withDeps 保存withDeps，供当前处理流程使用
	withDeps := flag.Bool("with-deps", false, "also install Linux system dependencies")
	// depsOnly 保存depsOnly，供当前处理流程使用
	depsOnly := flag.Bool("deps-only", false, "only install Linux system dependencies; do not download Chromium")
	flag.Parse()
	if *depsOnly && !*withDeps {
		*withDeps = true
	}

	if *driverDir != "" {
		if // err 保存err，供当前处理流程使用
		err := os.Setenv("PLAYWRIGHT_DRIVER_PATH", *driverDir); err != nil {
			fatal("设置 driver 目录失败", err)
		}
	}
	if *browserDir != "" {
		if // err 保存err，供当前处理流程使用
		err := os.Setenv("PLAYWRIGHT_BROWSERS_PATH", *browserDir); err != nil {
			fatal("设置浏览器目录失败", err)
		}
	}

	// options 保存options，供当前处理流程使用
	options := &playwright.RunOptions{
		Browsers: []string{"chromium"},
		Verbose:  true,
	}
	if *driverDir != "" {
		options.DriverDirectory = *driverDir
	}
	// driver、err 保存driver、err，供当前处理流程使用
	driver, err := playwright.NewDriver(options)
	if err != nil {
		fatal("创建 Playwright driver 失败", err)
	}
	if // err 保存err，供当前处理流程使用
	err := driver.DownloadDriver(); err != nil {
		fatal("下载 Playwright driver 失败", err)
	}

	// args 保存args，供当前处理流程使用
	args := []string{"install"}
	if *depsOnly {
		args = []string{"install-deps", "chromium"}
	} else if *withDeps {
		args = append(args, "--with-deps")
		args = append(args, "chromium")
	} else {
		args = append(args, "chromium")
	}
	// cmd 保存cmd，供当前处理流程使用
	cmd := driver.Command(args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if // err 保存err，供当前处理流程使用
	err := cmd.Run(); err != nil {
		fatal("安装 Chromium 失败", err)
	}
}

// fatal 负责fatal相关处理。
func fatal(message string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	os.Exit(1)
}
