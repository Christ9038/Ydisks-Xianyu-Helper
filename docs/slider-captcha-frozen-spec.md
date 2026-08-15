# 滑块认证冻结规范

> **冻结级别：最高。当前滑块认证逻辑是已验证的生产行为，不得修改。**
>
> 除非用户在当前任务中明确要求修改滑块认证，否则任何开发者或自动化代理都不得编辑、重构、优化、重命名、移动或删除本规范保护的实现和测试，也不得通过修改调用方、浏览器配置或共享工具间接改变其行为。

本文记录 `internal/browser` 中当前滑块认证的完整行为契约。它是维护边界，不是待办方案。后续任务即使涉及登录、Cookie、浏览器自动化、风控恢复或代码清理，也必须保持本文行为逐项不变。

## 1. 保护范围

以下文件为直接保护对象：

- `internal/browser/slider.go`
- `internal/browser/slider_test.go`
- `internal/browser/token_captcha.go`
- `internal/browser/token_captcha_test.go`
- `internal/browser/token_captcha_fallback.go`
- `internal/browser/token_captcha_fallback_integration_test.go`
- `internal/browser/token_captcha_orchestrator_test.go`

下列行为即使位于其他文件中，也属于冻结范围：

- `TokenCaptchaRecover` / `TokenCaptchaRecoverWithEngine` 的调用顺序、参数和返回语义。
- 账号级持久化 Chromium profile 的目录解析、互斥、复用和释放时机。
- 验证链接的获取、过期判断、重新获取时机和重试次数。
- Cookie 注入、旧 `x5sec` 快照、新 `x5sec` 判定、合并和持久化。
- Playwright 主引擎与直接 Chromium/CDP 备用引擎的启用条件和先后顺序。
- 滑块 DOM 选择器及其优先级、同 frame 可见性要求、距离计算、轨迹、时序、成功判定、失败恢复和日志诊断。

禁止以“格式化”“去重”“抽取公共函数”“升级 Playwright”“增强兼容性”“提高成功率”“清理测试”等名义绕过冻结规则。

## 2. 标准 DOM 契约

当前无验证码（NC）页面的标准元素为：

| 角色 | 首选选择器 | 标准尺寸 |
| --- | --- | --- |
| 滑块按钮 | `#nc_1_n1z` | 宽 `42px` |
| 滑轨 | `#nc_1_n1t` | 宽 `300px` |

标准滑动距离固定按可用轨道宽度计算：

```text
distance = track.width - button.width
         = 300px - 42px
         = 258px
```

不可把 `300px` 当作标准拖动距离，不可增加超调补偿。主引擎标准 NC 拖动的最终 X 必须精确落在 `258px`。

查找顺序和约束固定如下：

1. 在主 frame 和所有 iframe 中，优先查找同一 frame 内同时可见的 `#nc_1_n1z` 与 `#nc_1_n1t`。
2. 仅在精确选择器组合未找到时，才按现有顺序使用宽松选择器。
3. 按钮与轨道必须同时可见且位于同一 frame；不可跨 frame 拼接元素。

主引擎按钮选择器顺序：

```text
#nc_1_n1z
.nc_iconfont
.btn_slide
#scratch-captcha-btn
.scratch-captcha-slider .button
```

主引擎轨道选择器顺序：

```text
#nc_1_n1t
.nc_scale
.nc_1_n1t
```

备用引擎必须精确等待可见的 `#nc_1_n1z`。轨道选择器顺序为：

```text
#nc_1_n1t
.nc_scale
#nc_1__scale_text
.nc-lang-cnt
#nc_1_wrapper
.nc_wrapper
```

刮刮乐验证码继续通过 `scratch-captcha`、`scratch-captcha-btn` 或 `scratch-captcha-slider` 标记识别，并只滑标准距离的 `25%–35%`。不得把这条降级兼容逻辑用于标准 NC DOM。

## 3. 主引擎行为

主引擎使用 Playwright 和账号现有的持久化浏览器上下文，最多执行 3 次滑块尝试。

### 3.1 页面准备

- 浏览器总生命周期上限为 `45s`。
- 页面导航等待 `domcontentloaded`，导航超时为 `10s`；导航异常只记录告警并继续检查页面。
- 页面加载后随机等待 `0.3–0.8s`，移动鼠标到 `(640, 360)`，滚动 `200–500px`，并保留现有短暂停顿。
- 页面包含“抱歉，页面访问出现了问题”时，判定验证链接过期。
- 页面包含 `STATUS_BREAKPOINT` 或“崩溃”时，判定验证页崩溃。
- 页面准备完成后、首次滑动前必须先判定页面类型：只有同一 frame 内存在可见的滑块按钮和轨道时才进入滑动逻辑。
- 如果没有可见滑块，并且任一 frame 的可见正文直接显示“验证失败”“安全验证未通过”“请求/加载失败”“系统繁忙”“服务/网络/页面异常”“请稍后重试”或对应英文错误提示，则判定为无可验证滑块的直接错误页，立即停止自动验证。
- 直接错误页不得通过点击错误框、reload 或备用引擎强行转成滑块尝试；只有已经实际拖动过滑块后出现的 `.errloading` 才继续按第 5 节恢复。
- 拖动前必须保存所有域上的旧 `x5sec` 值，供严格成功判定使用。

### 3.2 距离计算

距离计算顺序固定为：

1. 用 DOM `getBoundingClientRect()` 计算 `track.width - button.width`。
2. DOM 计算不可用时，用 Playwright bounding box 计算同一差值。
3. 两者均不可用时，主引擎保留 `220–259px` 的现有降级距离。
4. 仅刮刮乐场景再乘 `0.25–0.35`。

标准 NC 几何信息存在时，必须使用精确差值，不得随机化距离。

### 3.3 拖动轨迹

主引擎轨迹冻结为以下行为：

- 按钮按下前，从按钮左侧附近自然接近，再悬停、对准按钮中心并停顿。
- 按下后使用恰好 6 个高层轨迹点。
- 轨迹分为加速、匀速、减速三段；带小幅 Y 轴抖动。
- X 轴不超调，最后一个点强制等于计算出的精确距离。
- 保留每个点的短随机计划延时，同时按真实墙钟时间补偿，使移动阶段处于 `480–900ms` 目标窗口。
- 顺序固定为 `mouse down → 分段移动 → mouse up → 兼容性 click 事件`。
- 标准 NC 不得添加尾部超调、回拉或额外轨迹点。

拖动完成后保留 `800ms` 等待，再进入严格成功检查。

## 4. 严格成功条件

Token 风控滑块只有同时满足以下两个条件才算成功：

1. 浏览器上下文出现相对拖动前快照全新的、非空的 `x5sec`；旧值不得重复计为成功。
2. 当前页面已经离开 punish/captcha URL。

以下 URL 标记任一存在时，仍视为停留在验证页：

```text
punish
x5step=2
action=captcha
purecaptcha
/captcha
```

`.nc-container` 消失、滑块隐藏、成功图标出现或 frame 断开都不能单独证明 Token 风控成功。严格路径必须以“新 `x5sec` + 离开验证 URL”为准。

严格检查期间，只要出现明确失败控件就立即按失败处理。主引擎拖动后最多等待 `5s` 进行严格确认；完成拖动后还会以 `250ms` 间隔等待新 Cookie，最长 `15s`。成功后收集浏览器上下文当前的 `x5*` Cookie，确保合并的是新签发的 `x5sec`，再合并回原 Cookie 字符串。

非 Token 场景调用通用 `solveSlider` 时，继续保留现有的可见成功标记或 `.nc-container` 消失判定；不得把该宽松判定用于 Token 风控路径。

## 5. 失败识别与恢复

明确失败控件固定为以下前三项：

```text
#nc_1_refresh1
.nc_iconfont.btn_refresh
.errloading
```

点击恢复时按以下优先级查找：

```text
#nc_1_refresh1
.nc_iconfont.btn_refresh
.errloading
[class*='refresh']
```

`.nc-container` 只能在其文本包含下列失败/重试词之一时点击：

```text
重试  刷新  失败  retry  refresh  failed
```

禁止无条件点击初始 `.nc-container`，因为这可能误触发拖动。

恢复状态机固定如下：

1. 优先点击可见的失败/刷新控件。
2. 控件可能延迟出现，首次未找到时按现有逻辑最多短暂等待 `800ms` 并轮询。
3. 点击后最多等待 `4s`，确认按钮和轨道重新出现，且按钮回到轨道起点 `±3px` 内。
4. 点击恢复失败或滑块未归位时，重载原页面。
5. 重载等待 `domcontentloaded`，单次超时最多 `8s`；重载后最多等待 `5s` 确认滑块归位。
6. 主引擎两次尝试之间保留 `1–2s` 随机等待；总尝试次数保持 3 次。

不得用“元素重新出现”替代“元素重新出现且按钮回到原点”的确认。

## 6. 引擎编排

固定执行顺序为：

```text
Playwright 主引擎
  → 链接过期时，关闭当前持久化上下文后重新获取链接并重试主引擎
  → 主引擎发生非链接过期失败时，按配置进入直接 Chromium/CDP 备用引擎
  → 备用引擎仍必须取得新的 x5sec 才能成功
```

约束如下：

- 获取新验证链接的 provider 不得在主引擎持有同账号持久化 profile 锁时调用，避免同账号锁死。
- 过期链接最多触发现有的 2 次链接刷新；最终仍过期时直接失败，不把过期 URL 交给备用引擎重放。
- 主引擎判定为“没有可见滑块的直接错误页”时立即失败，不刷新链接，也不进入直接 Chromium/CDP 备用引擎。
- 进入备用引擎前可再获取一次新链接和更新后的 Cookie。
- `CAPTCHA_REAL_MOUSE=true` 在当前 Go/Docker 实现中只记录物理鼠标不可用，并继续备用逻辑；不得假装已启用物理鼠标。
- `CAPTCHA_DRISSIONPAGE_FALLBACK_ENABLED` 默认为启用。
- 备用引擎成功返回的引擎标识保持为 `drissionpage`，虽然底层实现是直接 Chromium + CDP。

## 7. 备用引擎行为

备用引擎直接启动 Chromium，通过 `DevToolsActivePort` 连接 CDP，不改用 Playwright 的常规 launch 流程。它必须：

- 与主引擎使用同一账号持久化 profile 目录。
- 保持账号级互斥和全局续期槽位控制。
- 启动前清理现有 singleton 文件和 `DevToolsActivePort`，退出后再次清理 singleton 文件。
- 保留 `--remote-debugging-port=0`、`--window-size=1920,1080`、`--lang=zh-CN`、`--disable-blink-features=AutomationControlled` 等现有启动参数。
- headless 时使用 `--headless=new`。
- 直接 Chromium 必须同时携带与当前 Playwright runtime 匹配的兼容启动参数集合（包括后台网络、扩展、Storage 分区、渲染器后台化、SwiftShader 等参数），否则通过 CDP 访问默认上下文的 Cookie Storage 可能无响应；该集合必须随 Playwright runtime 升级同步核对。
- 备用引擎对默认上下文执行 Cookie 清理、注入和读取时，每次操作最多等待 `5s`；超时必须中止当前备用引擎流程并释放浏览器，不得无限阻塞续期协程。
- 默认总超时为 `25s`，可由现有环境变量控制；导航超时为 `15s`。

备用距离规则固定如下：

1. 按钮和轨道 bounding box 都存在时，精确使用 `track.width - button.width`；标准 DOM 必须得到 `258px`。
2. 仅轨道宽度可用时，保留基于轨道宽度的 `70%–90%` 加 `-20–20px` 偏移，并限制在 `200–600px`。
3. 轨道几何信息不可用时，才按 viewport 宽度使用现有随机范围。

备用引擎保留 3 种尝试模式：

| 尝试 | 计划移动时间 | 轨迹点数 | 特征 |
| --- | --- | --- | --- |
| 第 1 次 | `1.5–4s` | `60–150` | 常规 |
| 第 2 次 | `0.9–1.3s` | `30–60` | 急促，按下/释放等待更短 |
| 第 3 次 | `1–2s` | `50–90` | 常规 |

备用轨迹保留现有加速、匀速、减速、犹豫、Y 轴趋势与抖动、偶发轻微超调及回正、清洗和重采样行为。这个行为与主引擎“不得超调”的规则不同，不得相互替换。每次失败后复用第 5 节的恢复流程，最多尝试 3 次；成功仍要求新的 `x5sec` 且已经离开 punish/captcha URL。

## 8. 诊断日志

以下诊断字段属于冻结行为，不得删除或降级：

- 页面 URL 和 frame URL，且必须去掉 query 与 fragment，避免日志泄露验证参数。
- 按钮、轨道 bounding box 和 class。
- 计算出的滑动距离。
- 轨迹点数、计划时长/延时、目标移动时长、真实移动时长和总耗时。
- 最终 `left`、最终 class 或最终 X 偏移。
- 失败时的按钮 style/class、轨道 class、可见重试选择器和重试文本。
- 恢复方式是 `click` 还是 `reload`、所用选择器以及是否重新归位。
- 自动滑块最终失败时，错误文本和 `token 风控滑块处理失败` 日志的 `verification_url` 字段必须包含最后实际使用的完整验证地址（包括 query），供用户复制到本机浏览器手动验证。此字段是仅在最终失败时提供的人工接管信息；常规页面/frame 诊断日志仍必须去掉 query 与 fragment。

页面可能被浏览器扩展注入大量无关 DOM。当前仅把下列注入作为诊断信息记录，不允许它们改变滑块定位、距离或成功判定：

| 工具 | DOM 标记 |
| --- | --- |
| Requestly | `rq-implicit-test-rule-widget` |
| PikPak | `#__PIKPAK_EXTENSION__` |
| DeepL | `deepl-input-controller` |
| 沉浸式翻译 | `#immersive-translate-browser-popup` |

## 9. 必须保留的验证

任何获得用户明确授权的滑块改动，都必须至少保持以下验证通过：

```bash
go test ./internal/browser
go test ./internal/engine ./internal/account ./internal/server
go build ./cmd/server
go vet ./internal/browser ./internal/engine ./internal/account ./internal/server ./cmd/server
```

同时必须保留直接 Chromium 集成场景：

- 主引擎首次滑动成功。
- 主引擎首次失败并隐藏滑块，点击 `.errloading` 恢复后第二次成功。
- 直接 Chromium/CDP 备用引擎使用同一恢复流程后成功。
- 没有可见滑块、只显示直接错误提示的真实 Chromium 页面必须被识别，并且不得进入滑动或备用引擎。
- 标准 `300px` 轨道与 `42px` 按钮得到精确 `258px`。
- 旧 `x5sec` 不得判成功，新 `x5sec` 才能判成功。

不得删除、跳过、弱化或改写断言来让行为变化通过测试。

## 10. 唯一变更流程

### 2026-08-13 当前授权变更

用户明确要求改变“未找到滑块时仍执行失败恢复/备用引擎”和“最终失败日志只保留脱敏页面地址”的既有契约。原因是风控页有时只显示平台错误信息而没有可操作滑块，继续 reload、拖动或切换引擎没有验证意义且会增加风控；而真实滑块自动处理失败时，用户需要完整地址在本机浏览器中接管验证。本规范第 3.1、6、8、9 节已同步纳入新的页面分类、编排、日志和集成验证要求。

只有用户在当前任务中明确点名要求修改滑块认证时，才允许触碰冻结范围。获得授权后也必须在同一个变更中：

1. 说明要改变本文哪一条冻结契约及原因。
2. 同步更新实现、单元测试、真实 Chromium 集成测试和本文。
3. 完成第 9 节验证并报告结果。
4. 不得把一次授权推定为后续任务的持续授权。

没有上述明确授权时，遇到与滑块相关的需求或测试失败，应停止修改滑块代码，保留现场并向用户说明冲突。
