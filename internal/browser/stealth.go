package browser

// stealthTemplate 是浏览器侧反检测 JS。模板使用 {{占位符}}，由
// stealthScript() 在 Go 侧生成随机值后替换。JS 本身未改动。
const stealthTemplate = `
            // 隐藏webdriver属性
            Object.defineProperty(navigator, 'webdriver', {
                get: () => undefined,
            });

            // 隐藏自动化相关属性
            delete navigator.__proto__.webdriver;
            delete window.navigator.webdriver;
            delete window.navigator.__proto__.webdriver;

            // 模拟真实浏览器环境
            window.chrome = {
                runtime: {},
                loadTimes: function() {},
                csi: function() {},
                app: {}
            };

            // 覆盖plugins - 随机化
            const pluginCount = {{PLUGIN_COUNT}};
            Object.defineProperty(navigator, 'plugins', {
                get: () => Array.from({length: pluginCount}, (_, i) => ({
                    name: 'Plugin' + i,
                    description: 'Plugin ' + i
                })),
            });

            // 覆盖languages
            Object.defineProperty(navigator, 'languages', {
                get: () => ['{{LOCALE}}', 'zh', 'en'],
            });

            // 模拟真实的屏幕信息
            Object.defineProperty(screen, 'availWidth', { get: () => {{VW}} });
            Object.defineProperty(screen, 'availHeight', { get: () => {{VH}} - 40 });
            Object.defineProperty(screen, 'width', { get: () => {{VW}} });
            Object.defineProperty(screen, 'height', { get: () => {{VH}} });

            // 隐藏自动化检测 - 随机化硬件信息
            Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => {{HW_CORES}} });
            Object.defineProperty(navigator, 'deviceMemory', { get: () => {{MEM}} });

            // 模拟真实的时区
            Object.defineProperty(Intl.DateTimeFormat.prototype, 'resolvedOptions', {
                value: function() {
                    return { timeZone: '{{TZ}}' };
                }
            });

            // 隐藏自动化痕迹
            delete window.cdc_adoQpoasnfa76pfcZLmcfl_Array;
            delete window.cdc_adoQpoasnfa76pfcZLmcfl_Promise;
            delete window.cdc_adoQpoasnfa76pfcZLmcfl_Symbol;

            // 模拟有头模式的特征
            Object.defineProperty(navigator, 'maxTouchPoints', { get: () => 0 });
            Object.defineProperty(navigator, 'platform', { get: () => 'Win32' });
            Object.defineProperty(navigator, 'vendor', { get: () => 'Google Inc.' });
            Object.defineProperty(navigator, 'vendorSub', { get: () => '' });
            Object.defineProperty(navigator, 'productSub', { get: () => '20030107' });

            // 模拟真实的连接信息
            Object.defineProperty(navigator, 'connection', {
                get: () => ({
                    effectiveType: "{{EFF_TYPE}}",
                    rtt: {{RTT}},
                    downlink: {{DOWNLINK}}
                })
            });

            // 隐藏无头模式特征
            Object.defineProperty(navigator, 'headless', { get: () => undefined });
            Object.defineProperty(window, 'outerHeight', { get: () => {{VH}} });
            Object.defineProperty(window, 'outerWidth', { get: () => {{VW}} });

            // 模拟真实的媒体设备
            Object.defineProperty(navigator, 'mediaDevices', {
                get: () => ({
                    enumerateDevices: () => Promise.resolve([])
                }),
            });

            // 隐藏自动化检测特征
            Object.defineProperty(navigator, '__webdriver_script_fn', { get: () => undefined });
            Object.defineProperty(navigator, '__webdriver_evaluate', { get: () => undefined });
            Object.defineProperty(navigator, '__webdriver_unwrapped', { get: () => undefined });
            Object.defineProperty(navigator, '__fxdriver_evaluate', { get: () => undefined });
            Object.defineProperty(navigator, '__driver_evaluate', { get: () => undefined });
            Object.defineProperty(navigator, '__webdriver_script_func', { get: () => undefined });

            // 隐藏Playwright特定的对象
            delete window.playwright;
            delete window.__playwright;
            delete window.__pw_manual;
            delete window.__pw_original;

            // 模拟真实的用户代理
            Object.defineProperty(navigator, 'userAgent', {
                get: () => '{{UA}}'
            });

            // 隐藏自动化相关的全局变量
            delete window.webdriver;
            delete window.__webdriver_script_fn;
            delete window.__webdriver_evaluate;
            delete window.__webdriver_unwrapped;
            delete window.__fxdriver_evaluate;
            delete window.__driver_evaluate;
            delete window.__webdriver_script_func;
            delete window._selenium;
            delete window._phantom;
            delete window.callPhantom;
            delete window.phantom;
            delete window.Buffer;
            delete window.emit;
            delete window.spawn;

            // Canvas指纹随机化
            const originalToDataURL = HTMLCanvasElement.prototype.toDataURL;
            HTMLCanvasElement.prototype.toDataURL = function() {
                const context = this.getContext('2d');
                if (context) {
                    const imageData = context.getImageData(0, 0, this.width, this.height);
                    const data = imageData.data;
                    for (let i = 0; i < data.length; i += 4) {
                        if (Math.random() < 0.001) {
                            data[i] = Math.floor(Math.random() * 256);
                        }
                    }
                    context.putImageData(imageData, 0, 0);
                }
                return originalToDataURL.apply(this, arguments);
            };

            // 音频指纹随机化
            const originalGetChannelData = AudioBuffer.prototype.getChannelData;
            AudioBuffer.prototype.getChannelData = function(channel) {
                const data = originalGetChannelData.call(this, channel);
                for (let i = 0; i < data.length; i += 1000) {
                    if (Math.random() < 0.01) {
                        data[i] += Math.random() * 0.0001;
                    }
                }
                return data;
            };

            // WebGL指纹随机化
            const originalGetParameter = WebGLRenderingContext.prototype.getParameter;
            WebGLRenderingContext.prototype.getParameter = function(parameter) {
                if (parameter === 37445) { // UNMASKED_VENDOR_WEBGL
                    return 'Intel Inc.';
                }
                if (parameter === 37446) { // UNMASKED_RENDERER_WEBGL
                    return 'Intel Iris OpenGL Engine';
                }
                return originalGetParameter.call(this, parameter);
            };

            // 模拟真实的鼠标事件
            const originalAddEventListener = EventTarget.prototype.addEventListener;
            EventTarget.prototype.addEventListener = function(type, listener, options) {
                if (type === 'mousedown' || type === 'mouseup' || type === 'mousemove') {
                    const originalListener = listener;
                    listener = function(event) {
                        setTimeout(() => originalListener.call(this, event), Math.random() * 10);
                    };
                }
                return originalAddEventListener.call(this, type, listener, options);
            };

            // 随机化字体检测
            Object.defineProperty(document, 'fonts', {
                get: () => ({
                    ready: Promise.resolve(),
                    check: () => true,
                    load: () => Promise.resolve([])
                })
            });

            // 增强鼠标移动轨迹记录
            let mouseMovements = [];
            let lastMouseTime = Date.now();
            document.addEventListener('mousemove', function(e) {
                const now = Date.now();
                const timeDiff = now - lastMouseTime;
                mouseMovements.push({
                    x: e.clientX,
                    y: e.clientY,
                    time: now,
                    timeDiff: timeDiff
                });
                lastMouseTime = now;
                if (mouseMovements.length > 100) {
                    mouseMovements.shift();
                }
            }, true);

            // 模拟真实的电池API
            if (navigator.getBattery) {
                const originalGetBattery = navigator.getBattery;
                navigator.getBattery = async function() {
                    const battery = await originalGetBattery.call(navigator);
                    Object.defineProperty(battery, 'charging', { get: () => {{BATT_CHARGE}} });
                    Object.defineProperty(battery, 'level', { get: () => {{BATT_LEVEL}} });
                    return battery;
                };
            }

            // 伪装鼠标移动加速度（反检测关键）
            let velocityProfile = [];
            window.addEventListener('mousemove', function(e) {
                const now = performance.now();
                velocityProfile.push({ x: e.clientX, y: e.clientY, t: now });
                if (velocityProfile.length > 50) velocityProfile.shift();
            }, true);

            // 伪装Permission API
            const originalQuery = Permissions.prototype.query;
            Permissions.prototype.query = function(parameters) {
                if (parameters.name === 'notifications') {
                    return Promise.resolve({ state: 'denied' });
                }
                return originalQuery.apply(this, arguments);
            };

            // 伪装Performance API
            const originalNow = Performance.prototype.now;
            Performance.prototype.now = function() {
                return originalNow.call(this) + Math.random() * 0.1;
            };

            // 伪装RTCPeerConnection（WebRTC指纹）
            if (window.RTCPeerConnection) {
                const originalRTC = window.RTCPeerConnection;
                window.RTCPeerConnection = function(...args) {
                    const pc = new originalRTC(...args);
                    const originalCreateOffer = pc.createOffer;
                    pc.createOffer = function(...args) {
                        return originalCreateOffer.apply(this, args).then(offer => {
                            offer.sdp = offer.sdp.replace(/a=fingerprint:.*\r\n/g,
                                'a=fingerprint:sha-256 ' + Array.from({length:64}, ()=>Math.floor(Math.random()*16).toString(16)).join('') + '\r\n');
                            return offer;
                        });
                    };
                    return pc;
                };
            }

            // 🔑 关键优化：隐藏CDP运行时特征
            Object.defineProperty(navigator, 'webdriver', {
                get: () => undefined
            });

            // 🔑 隐藏自动化控制特征
            window.navigator.chrome = {
                runtime: {},
                loadTimes: function() {},
                csi: function() {},
                app: {}
            };

            // 🔑 隐藏Playwright特征
            delete window.__playwright;
            delete window.__pw_manual;
            delete window.__PW_inspect;

            // 🔑 伪装chrome对象（防止检测headless）
            if (!window.chrome) {
                window.chrome = {};
            }
            window.chrome.runtime = {
                id: undefined,
                sendMessage: function() {},
                connect: function() {}
            };

            // 🔑 伪装Permissions API
            const originalQuery2 = window.navigator.permissions.query;
            window.navigator.permissions.query = (parameters) => (
                parameters.name === 'notifications' ?
                    Promise.resolve({ state: Notification.permission }) :
                    originalQuery2(parameters)
            );

            // 🔑 覆盖Function.prototype.toString以隐藏代理
            const oldToString = Function.prototype.toString;
            Function.prototype.toString = function() {
                if (this === navigator.permissions.query) {
                    return 'function query() { [native code] }';
                }
                return oldToString.call(this);
            };
`
