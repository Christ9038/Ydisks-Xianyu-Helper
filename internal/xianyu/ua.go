// Package xianyu 的公共常量：UA 统一管理。
//
// 真实浏览器的所有出站请求（HTTP mtop + WS 握手 + /reg 注册消息）UA 必须完全一致，
// 否则版本号自相矛盾会被风控识别为伪造。这里集中定义，三处引用同一常量，杜绝不一致。
//
// 版本号选择原则：用主流稳定版区间，避免太新（显得伪造）或太旧（显得异常）。
// 浏览器版本必须与实际安装的 Chromium 保持一致。
package xianyu

// ChromeVer 统一的 Chrome 版本号，所有 UA 引用此常量。
// 必须与 playwright-go 实际下载的 Chromium 版本对齐。
// playwright-go v0.5700.1 → Playwright v1.57.0 → Chromium 143.0.7499.4
const ChromeVer = "143.0.7499.4"

// ChromeMajor Chrome 主版本号（sec-ch-ua 用），从 ChromeVer 派生。
const ChromeMajor = "143"

// BrowserUA 标准 Chrome 浏览器 UA（HTTP mtop / WS 握手共用）。
const BrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + ChromeVer + " Safari/537.36"

// RegUA WS /reg 注册消息用的 UA：浏览器 UA + 闲鱼网页客户端标识（DingTalk/DingWeb）。
// DingTalk 后缀是闲鱼网页真实客户端标识，必须保留；其 Chrome 版本与 BrowserUA 一致。
const RegUA = BrowserUA + " DingTalk(2.1.5) OS(Windows/10) Browser(Chrome/" + ChromeVer + ") DingWeb/2.1.5 IMPaaS DingWeb/2.1.5"

// SecChUA sec-ch-ua 请求头值，主版本号与 ChromeMajor 一致。
// 真实浏览器 sec-ch-ua 的版本号必须与 UA 的 Chrome 主版本号匹配，否则矛盾。
const SecChUA = `"Not;A=Brand";v="99", "Google Chrome";v="` + ChromeMajor + `", "Chromium";v="` + ChromeMajor + `"`
