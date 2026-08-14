package browser

// Keep Chromium's native fingerprint intact. The launch arguments already
// disable AutomationControlled; this only removes well-known automation
// globals and never patches input events or hardware properties.
// stealthTemplate 保存stealthTemplate，供当前处理流程使用
const stealthTemplate = `
(() => {
    try {
        Object.defineProperty(Navigator.prototype, 'webdriver', {
            configurable: true,
            get: () => undefined,
        });
    } catch (_) {}
    for (const key of [
        '__playwright', '__pw_manual', '__PW_inspect', '__pw_original',
        'webdriver', '__webdriver_script_fn', '__webdriver_evaluate',
        '__webdriver_unwrapped', '__driver_evaluate'
    ]) {
        try { delete window[key]; } catch (_) {}
    }
})();
`
