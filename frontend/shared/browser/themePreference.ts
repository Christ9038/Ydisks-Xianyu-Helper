// themeStorageKey 是浏览器本地保存主题偏好的存储键；只保存 system/light/dark 三个枚举值，不含敏感信息。
export const themeStorageKey = 'ydisks.theme.v1';

// ThemePreference 是用户选择的主题偏好：跟随系统、固定浅色或固定深色。
export type ThemePreference = 'system' | 'light' | 'dark';

// ResolvedTheme 是真正应用到页面上的主题，由偏好与系统配色共同决定。
export type ResolvedTheme = 'light' | 'dark';

// themeCycleOrder 是点击切换按钮时的循环顺序：跟随系统 → 浅色 → 深色。
const themeCycleOrder: readonly ThemePreference[] = ['system', 'light', 'dark'];

// isThemePreference 判断任意存储值是否为合法偏好，避免被篡改的本地值污染页面状态。
export const isThemePreference = (value: unknown): value is ThemePreference => value === 'system' || value === 'light' || value === 'dark';

// readThemePreference 读取已保存的主题偏好；存储不可用或值非法时回退为跟随系统。
export const readThemePreference = (): ThemePreference => {
	try {
		// stored 是本地存储中的原始字符串，可能为空或被外部修改。
		const stored = window.localStorage.getItem(themeStorageKey);
		return isThemePreference(stored) ? stored : 'system';
	} catch {
		return 'system';
	}
};

// writeThemePreference 保存主题偏好；加固浏览器可能禁用存储，此时仅内存状态生效。
export const writeThemePreference = (preference: ThemePreference): void => {
	try {
		window.localStorage.setItem(themeStorageKey, preference);
	} catch {
		// 存储不可用时本次会话内的主题切换依然有效，只是不会跨会话记忆。
	}
};

// resolveTheme 把用户偏好和系统是否偏好深色合成为实际主题。
export const resolveTheme = (preference: ThemePreference, systemPrefersDark: boolean): ResolvedTheme => {
	if (preference === 'system') return systemPrefersDark ? 'dark' : 'light';
	return preference;
};

// nextThemePreference 返回循环切换后的下一个偏好。
export const nextThemePreference = (current: ThemePreference): ThemePreference => {
	// index 是当前偏好在循环顺序中的位置。
	const index = themeCycleOrder.indexOf(current);
	return themeCycleOrder[(index + 1) % themeCycleOrder.length];
};

// applyResolvedTheme 把实际主题写到 <html data-theme>，由 index.css 的深色令牌覆盖生效。
export const applyResolvedTheme = (theme: ResolvedTheme): void => {
	document.documentElement.dataset.theme = theme;
};
