// Command browser-install prepares the Playwright driver and Chromium runtime.
// It intentionally does not change the server's browser launch behavior.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mxschmitt/playwright-go"
)

// main 封装main业务协调。
func main() {
	// driverDir 用于本次流程后续判断的driverDir
	driverDir := flag.String("driver-dir", "", "Playwright driver directory")
	// browserDir 用于本次流程后续判断的浏览器Dir
	browserDir := flag.String("browser-dir", "", "Playwright browser cache directory")
	// withDeps 用于本次流程后续判断的withDeps
	withDeps := flag.Bool("with-deps", false, "also install Linux system dependencies")
	// depsOnly 用于本次流程后续判断的depsOnly
	depsOnly := flag.Bool("deps-only", false, "only install Linux system dependencies; do not download Chromium")
	flag.Parse()
	if *depsOnly && !*withDeps {
		*withDeps = true
	}

	if *driverDir != "" {
		if // err 用于本次流程后续判断的err
		err := os.Setenv("PLAYWRIGHT_DRIVER_PATH", *driverDir); err != nil {
			fatal("设置 driver 目录失败", err)
		}
	}
	if *browserDir != "" {
		if // err 用于本次流程后续判断的err
		err := os.Setenv("PLAYWRIGHT_BROWSERS_PATH", *browserDir); err != nil {
			fatal("设置浏览器目录失败", err)
		}
	}

	// options 用于本次流程后续判断的options
	options := &playwright.RunOptions{
		Browsers: []string{"chromium"},
		Verbose:  true,
	}
	if *driverDir != "" {
		options.DriverDirectory = *driverDir
	}
	// driver、err 用于本次流程后续判断的driver、err
	driver, err := playwright.NewDriver(options)
	if err != nil {
		fatal("创建 Playwright driver 失败", err)
	}
	if // err 用于本次流程后续判断的err
	err := driver.DownloadDriver(); err != nil {
		fatal("下载 Playwright driver 失败", err)
	}

	// args 用于本次流程后续判断的args
	args := []string{"install"}
	if *depsOnly {
		args = []string{"install-deps", "chromium"}
	} else if *withDeps {
		args = append(args, "--with-deps")
		args = append(args, "chromium")
	} else {
		args = append(args, "chromium")
	}
	// cmd 用于本次流程后续判断的cmd
	cmd := driver.Command(args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if // err 用于本次流程后续判断的err
	err := cmd.Run(); err != nil {
		fatal("安装 Chromium 失败", err)
	}
}

// fatal 封装fatal业务协调。
func fatal(message string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	os.Exit(1)
}
