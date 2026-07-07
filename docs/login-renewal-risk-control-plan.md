# 登录续期与风控能力补齐计划

## 目标

参考 `zhinianboke/xianyu-auto-reply` 的实现，把当前项目缺少的登录态续期、MTOP 风控识别、扫码人脸验证能力补齐。改造重点是提升账号长期在线能力和风控状态的可恢复性，同时保持 `xianyu-go` 现有单进程架构清晰、可测试、可回滚。

## 非目标

- 不实现绕过人脸识别。人脸验证必须由用户在手机端完成。
- 不把浏览器截图作为主验证方式。截图仅保留为兜底调试手段。
- 不在第一版引入跨服务架构；续期和验证能力仍在当前 Go 进程内闭环。
- 不默认把已禁用账号自动启用，避免用户明确停用的账号被后台恢复上线。

## 阶段 1：扫码人脸验证 API 链路

修改 `internal/xianyu/qrlogin`：

- 在扫码 `query.do` 返回 `iframeRedirect=true` 时保留响应 Cookie。
- 跟随 `iframeRedirectUrl` 到 `normal_validate.htm`。
- 从 HTML 提取 `htoken` 和 `verify_modes.htm` 下一跳链接。
- 请求 `verify_modes.htm` 并落到 `identity_verify.htm`。
- 从 `identity_verify.htm` 中提取 `new Qrcode({ text: "..." })` 的二维码内容。
- 后端渲染二维码为 `face_qr_url`，返回给前端展示。
- 轮询 `photoVerify/check.do?htoken=...`。
- 当返回 `code=3` 时访问返回的 `ivCheckLogin.htm`，收集最终 Cookie 和 `unb`。

前端修改：

- `verification_required` 状态优先展示 `face_qr_url`。
- 文案改为提示用户用手机闲鱼扫描二维码完成人脸验证。
- `verification_screenshot` 仅作为旧兜底，不再作为主体验。

测试：

- 提取 `htoken`、`verify_modes`、人脸二维码内容的纯函数测试。
- `photoVerify/check.do` 成功后 session 转为 `success` 的 HTTP mock 测试。

## 阶段 2：Cookie 接口续期服务

新增 `internal/xianyu/renew`：

- 实现 `hasLogin.do`、`silentHasLogin.do`、`setLoginSettings.do` 三段续期。
- 每一步合并 `Set-Cookie`，后续请求使用合并后的 Cookie。
- 只有 `setLoginSettings.do` 返回有效 `Set-Cookie` 时，判定长登录续期成功。
- 返回续期方式、更新字段、最终 Cookie、步骤详情。
- 所有响应读取必须有大小上限。

测试：

- 多个 `Set-Cookie` 合并顺序。
- 接口续期成功、失败、部分 Cookie 更新。
- `setLoginSettings.do` 未返回 Cookie 时不误判成功。

## 阶段 3：登录态检查与 MTOP 风控分类

新增或扩展 `internal/xianyu/mtop`：

- 实现 `mtop.taobao.idlemessage.pc.loginuser.get` 登录态检查。
- 识别 `TOKEN_EXOIRED`、`TOKEN_EXPIRED`、`TOKEN_EMPTY`、`SESSION_EXPIRED`。
- 识别 `FAIL_SYS_USER_VALIDATE`、`RGV587`、`punish`、`captcha`、`x5secdata`。
- 对风控错误返回带验证 URL 的结构化错误，避免上层只能靠字符串判断。

测试：

- 各类 ret 的分类。
- 风控 URL 提取。
- 登录态接口返回 Cookie 时正确合并。

## 阶段 4：运行时恢复顺序接入

修改 `internal/engine/account.go`：

当前流程：

`RefreshToken -> WS -> 连续失败 -> PasswordLogin -> 慢重试`

目标流程：

`RefreshToken -> API Renew -> Browser Renew -> PasswordLogin -> 慢重试/人工验证`

规则：

- token 刷新遇到 token/session 问题时先尝试接口续期。
- 接口续期成功后保存 Cookie、清 token 缓存、重新获取 token。
- 接口续期失败但 Cookie 有更新时也保存，避免丢服务端新字段。
- 浏览器续期作为第二层恢复。
- 密码登录作为最后自动恢复手段。
- 风控类错误进入 `verification_required`，降频重试并发通知。

## 阶段 5：重连退避和多账号保护

- 网络错误和认证错误使用不同退避。
- 增加 0-30% 随机抖动，降低多账号同时重连冲击。
- 短连接继续清 token 缓存。
- 频繁短连接时进入明确状态，避免无意义快速重连。

## 验证命令

每阶段至少运行对应包测试。完整收尾运行：

```bash
go test ./...
go vet ./...
make lint
cd frontend && npm test -- --run && npm run build
```

## 实施记录

- 已完成扫码人脸验证 API 链路：后端提取 `htoken`、`verify_modes`、人脸二维码内容并轮询 `photoVerify/check.do`，前端优先展示 `face_qr_url`；截图只保留为兜底。
- 已完成 Cookie 接口续期服务：`hasLogin.do -> silentHasLogin.do -> setLoginSettings.do` 串行合并 `Set-Cookie`，所有响应读取有大小上限。
- 已完成 MTOP 风控分类：token 过期、session 失效、`FAIL_SYS_USER_VALIDATE`、`RGV587`、`punish/captcha/x5secdata` 等状态被结构化识别。
- 已完成运行时恢复顺序：`RefreshToken -> API Renew -> Browser Renew -> PasswordLogin -> 慢重试/人工验证`，Cookie 更新后会清理旧 token 缓存。
- 已完成多账号保护：重连退避加入 0-30% 抖动，降低多账号同时重连造成的风控压力。
