package browser

// stealthTemplate 是每个浏览器上下文在页面脚本运行前注入的初始化脚本。
// 它只做两类事：一是清除自动化全局标记；二是补齐无头 Chromium 相对有头 Chromium 缺失或自相矛盾的页面对象
// （chrome 对象、通知权限状态、窗口外框尺寸、空 languages）。
// 在用户明确授权下，它还把无头 Chromium 暴露的图形与硬件特征对齐到常见桌面浏览器：图形接口的未遮蔽厂商与渲染器、
// 逻辑核数、常见音视频编解码的 canPlayType 结果，以及空插件列表时的 PDF 插件。
// 它不改写 User-Agent、userAgentData、platform、deviceMemory，也不触碰输入事件与 Canvas、WebRTC；主引擎的 UA 与
// Client Hints 归一化仍完全由 Playwright 上下文和页面级 Emulation 覆盖负责，脚本与其互不干扰。
// 每一段都在 try 内独立执行；除明确授权覆盖的图形厂商、逻辑核数外，其余只在检测到缺失时补齐，Chromium 已有的原生行为保持不变。
const stealthTemplate = `
(() => {
    // 把补丁函数的 toString 伪装成原生函数文本，避免被函数源码探测。
    const nativeNames = new WeakMap();
    try {
        const nativeToString = Function.prototype.toString;
        const patched = new Proxy(nativeToString, {
            apply(target, thisArg, args) {
                if (nativeNames.has(thisArg)) {
                    return 'function ' + nativeNames.get(thisArg) + '() { [native code] }';
                }
                return Reflect.apply(target, thisArg, args);
            },
        });
        nativeNames.set(patched, 'toString');
        Object.defineProperty(Function.prototype, 'toString', {
            configurable: true,
            writable: true,
            value: patched,
        });
    } catch (_) {}
    const markNative = (fn, name) => {
        try { nativeNames.set(fn, name); } catch (_) {}
        return fn;
    };

    // 1. 自动化标记：webdriver 属性与驱动注入的全局变量。
    try {
        Object.defineProperty(Navigator.prototype, 'webdriver', {
            configurable: true,
            get: markNative(() => undefined, 'get webdriver'),
        });
    } catch (_) {}
    for (const key of [
        '__playwright', '__pw_manual', '__PW_inspect', '__pw_original',
        'webdriver', '__webdriver_script_fn', '__webdriver_evaluate',
        '__webdriver_unwrapped', '__driver_evaluate'
    ]) {
        try { delete window[key]; } catch (_) {}
    }

    // 2. window.chrome：无头 Chromium 可能缺少 app、csi、loadTimes、runtime，补齐有头浏览器的对象形状。
    try {
        if (!window.chrome) {
            Object.defineProperty(window, 'chrome', { configurable: true, enumerable: true, writable: true, value: {} });
        }
        const chromeObject = window.chrome;
        if (!chromeObject.app) {
            chromeObject.app = {
                isInstalled: false,
                InstallState: { DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' },
                RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' },
                getDetails: markNative(function getDetails() { return null; }, 'getDetails'),
                getIsInstalled: markNative(function getIsInstalled() { return false; }, 'getIsInstalled'),
                runningState: markNative(function runningState() { return 'cannot_run'; }, 'runningState'),
            };
        }
        if (!chromeObject.csi) {
            const startedAt = Date.now();
            chromeObject.csi = markNative(function csi() {
                return { onloadT: Date.now(), startE: startedAt, pageT: Date.now() - startedAt, tran: 15 };
            }, 'csi');
        }
        if (!chromeObject.loadTimes) {
            const startedAt = Date.now() / 1000;
            chromeObject.loadTimes = markNative(function loadTimes() {
                const now = Date.now() / 1000;
                return {
                    requestTime: startedAt, startLoadTime: startedAt, commitLoadTime: startedAt,
                    finishDocumentLoadTime: now, finishLoadTime: now, firstPaintTime: now,
                    firstPaintAfterLoadTime: 0, navigationType: 'Other', wasFetchedViaSpdy: true,
                    wasNpnNegotiated: true, npnNegotiatedProtocol: 'h2', wasAlternateProtocolAvailable: false,
                    connectionInfo: 'h2',
                };
            }, 'loadTimes');
        }
        if (!chromeObject.runtime) {
            chromeObject.runtime = {
                OnInstalledReason: { CHROME_UPDATE: 'chrome_update', INSTALL: 'install', SHARED_MODULE_UPDATE: 'shared_module_update', UPDATE: 'update' },
                PlatformOs: { ANDROID: 'android', CROS: 'cros', LINUX: 'linux', MAC: 'mac', OPENBSD: 'openbsd', WIN: 'win' },
                connect: markNative(function connect() { throw new TypeError('Error in invocation of runtime.connect(optional string extensionId, optional object connectInfo): chrome.runtime.connect() called from a webpage must specify an Extension ID (string) for its first argument.'); }, 'connect'),
                sendMessage: markNative(function sendMessage() { throw new TypeError('Error in invocation of runtime.sendMessage(optional string extensionId, any message, optional object options, optional function callback): chrome.runtime.sendMessage() called from a webpage must specify an Extension ID (string) for its first argument.'); }, 'sendMessage'),
            };
        }
    } catch (_) {}

    // 3. 通知权限：无头模式下 Notification.permission 为 denied，而 permissions.query 却返回 prompt，二者自相矛盾。
    try {
        if (typeof Notification !== 'undefined' && navigator.permissions && navigator.permissions.query) {
            const originalQuery = navigator.permissions.query.bind(navigator.permissions);
            const patchedQuery = markNative(function query(descriptor) {
                if (descriptor && descriptor.name === 'notifications' && Notification.permission === 'denied') {
                    return originalQuery(descriptor).then((status) => {
                        if (status.state !== 'prompt') return status;
                        return new Proxy(status, {
                            get(target, property) {
                                if (property === 'state') return 'denied';
                                const value = Reflect.get(target, property);
                                return typeof value === 'function' ? value.bind(target) : value;
                            },
                        });
                    });
                }
                return originalQuery(descriptor);
            }, 'query');
            Object.defineProperty(Permissions.prototype, 'query', { configurable: true, writable: true, value: patchedQuery });
        }
    } catch (_) {}

    // 4. 窗口外框尺寸：无头模式下 outerWidth、outerHeight 可能为 0，改为与可视区一致的合理值。
    try {
        if (window.outerWidth === 0 || window.outerHeight === 0) {
            Object.defineProperty(window, 'outerWidth', { configurable: true, get: markNative(() => window.innerWidth, 'get outerWidth') });
            Object.defineProperty(window, 'outerHeight', { configurable: true, get: markNative(() => window.innerHeight + 85, 'get outerHeight') });
        }
    } catch (_) {}

    // 5. languages：仅在为空时补齐为与启动参数 --lang=zh-CN 一致的取值。
    try {
        if (!navigator.languages || navigator.languages.length === 0) {
            Object.defineProperty(Navigator.prototype, 'languages', {
                configurable: true,
                get: markNative(() => Object.freeze(['zh-CN', 'zh']), 'get languages'),
            });
        }
    } catch (_) {}
    // 6. 逻辑核数：对齐常见桌面浏览器的取值。
    try {
        Object.defineProperty(Navigator.prototype, 'hardwareConcurrency', {
            configurable: true,
            get: markNative(() => 4, 'get hardwareConcurrency'),
        });
    } catch (_) {}

    // 7. 图形接口：把无头渲染器暴露的软件渲染厂商与渲染器替换为常见桌面显卡取值。
    try {
        const UNMASKED_VENDOR = 37445;
        const UNMASKED_RENDERER = 37446;
        for (const contextName of ['WebGLRenderingContext', 'WebGL2RenderingContext']) {
            const contextClass = window[contextName];
            if (!contextClass || !contextClass.prototype) continue;
            const original = contextClass.prototype.getParameter;
            const patched = new Proxy(original, {
                apply(target, thisArg, args) {
                    const parameter = (args || [])[0];
                    if (parameter === UNMASKED_VENDOR) return 'Intel Inc.';
                    if (parameter === UNMASKED_RENDERER) return 'Intel Iris OpenGL Engine';
                    return Reflect.apply(target, thisArg, args);
                },
            });
            nativeNames.set(patched, 'getParameter');
            Object.defineProperty(contextClass.prototype, 'getParameter', { configurable: true, writable: true, value: patched });
        }
    } catch (_) {}

    // 8. 编解码：随包 Chromium 缺少专有编解码时 canPlayType 返回空串，对常见 H.264、AAC、MP3 容器格式返回 probably。
    try {
        const originalCanPlayType = HTMLMediaElement.prototype.canPlayType;
        const patchedCanPlayType = new Proxy(originalCanPlayType, {
            apply(target, thisArg, args) {
                const result = Reflect.apply(target, thisArg, args);
                if (result) return result;
                const mime = String((args || [])[0] || '').toLowerCase().replace(/\s+/g, '');
                if (/^video\/mp4;codecs="?avc1/.test(mime) || /^audio\/mp4;codecs="?mp4a\.40/.test(mime)) return 'probably';
                if (mime === 'audio/mpeg' || mime === 'audio/aac' || mime === 'audio/x-m4a' || mime === 'audio/mp4') return 'probably';
                if (mime === 'video/mp4') return 'maybe';
                return result;
            },
        });
        nativeNames.set(patchedCanPlayType, 'canPlayType');
        Object.defineProperty(HTMLMediaElement.prototype, 'canPlayType', { configurable: true, writable: true, value: patchedCanPlayType });
    } catch (_) {}

    // 9. 插件：仅在插件列表为空时补齐 Chrome 内置 PDF 查看器及其 MIME 类型。
    try {
        if (navigator.plugins && navigator.plugins.length === 0) {
            const mimeSpecs = [
                { type: 'application/pdf', suffixes: 'pdf', description: 'Portable Document Format' },
                { type: 'text/pdf', suffixes: 'pdf', description: 'Portable Document Format' },
            ];
            const pluginNames = ['PDF Viewer', 'Chrome PDF Viewer', 'Chromium PDF Viewer', 'Microsoft Edge PDF Viewer', 'WebKit built-in PDF'];
            const mimeTypes = mimeSpecs.map((spec) => {
                const mime = Object.create(MimeType.prototype);
                for (const key of Object.keys(spec)) Object.defineProperty(mime, key, { value: spec[key], enumerable: true });
                return mime;
            });
            const plugins = pluginNames.map((name) => {
                const plugin = Object.create(Plugin.prototype);
                Object.defineProperty(plugin, 'name', { value: name, enumerable: true });
                Object.defineProperty(plugin, 'filename', { value: 'internal-pdf-viewer', enumerable: true });
                Object.defineProperty(plugin, 'description', { value: 'Portable Document Format', enumerable: true });
                Object.defineProperty(plugin, 'length', { value: mimeTypes.length, enumerable: true });
                mimeTypes.forEach((mime, index) => Object.defineProperty(plugin, index, { value: mime, enumerable: true }));
                return plugin;
            });
            const makeArray = (prototype, items) => {
                const array = Object.create(prototype);
                Object.defineProperty(array, 'length', { value: items.length, enumerable: true });
                items.forEach((item, index) => {
                    Object.defineProperty(array, index, { value: item, enumerable: true });
                    Object.defineProperty(array, item.name || item.type, { value: item, enumerable: false });
                });
                Object.defineProperty(array, 'item', { value: markNative(function item(index) { return items[index] || null; }, 'item') });
                Object.defineProperty(array, 'namedItem', { value: markNative(function namedItem(name) { return items.find((entry) => (entry.name || entry.type) === name) || null; }, 'namedItem') });
                return array;
            };
            const pluginArray = makeArray(PluginArray.prototype, plugins);
            const mimeTypeArray = makeArray(MimeTypeArray.prototype, mimeTypes);
            Object.defineProperty(Navigator.prototype, 'plugins', { configurable: true, get: markNative(() => pluginArray, 'get plugins') });
            Object.defineProperty(Navigator.prototype, 'mimeTypes', { configurable: true, get: markNative(() => mimeTypeArray, 'get mimeTypes') });
        }
    } catch (_) {}
})();
`
