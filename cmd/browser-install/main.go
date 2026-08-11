// Command browser-install prepares the Playwright driver and Chromium runtime.
// It intentionally does not change the server's browser launch behavior.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/playwright-community/playwright-go"
)

func main() {
	driverDir := flag.String("driver-dir", "", "Playwright driver directory")
	browserDir := flag.String("browser-dir", "", "Playwright browser cache directory")
	withDeps := flag.Bool("with-deps", false, "also install Linux system dependencies")
	flag.Parse()

	if *driverDir != "" {
		if err := os.Setenv("PLAYWRIGHT_DRIVER_PATH", *driverDir); err != nil {
			fatal("设置 driver 目录失败", err)
		}
	}
	if *browserDir != "" {
		if err := os.Setenv("PLAYWRIGHT_BROWSERS_PATH", *browserDir); err != nil {
			fatal("设置浏览器目录失败", err)
		}
	}

	options := &playwright.RunOptions{
		Browsers: []string{"chromium"},
		Verbose:  true,
	}
	if *driverDir != "" {
		options.DriverDirectory = *driverDir
	}
	driver, err := playwright.NewDriver(options)
	if err != nil {
		fatal("创建 Playwright driver 失败", err)
	}
	if err := driver.DownloadDriver(); err != nil {
		fatal("下载 Playwright driver 失败", err)
	}

	args := []string{"install"}
	if *withDeps {
		args = append(args, "--with-deps")
	}
	args = append(args, "chromium")
	cmd := driver.Command(args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatal("安装 Chromium 失败", err)
	}
}

func fatal(message string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	os.Exit(1)
}
