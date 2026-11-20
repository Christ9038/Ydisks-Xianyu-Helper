import { useCallback, useEffect, useState } from 'react';
import {
	applyResolvedTheme, nextThemePreference, readThemePreference, resolveTheme, writeThemePreference,
	type ThemePreference,
} from './themePreference';

// darkSchemeQuery 是系统深色配色的媒体查询。
const darkSchemeQuery = '(prefers-color-scheme: dark)';

// UseThemePreferenceResult 描述主题 Hook 暴露给界面的状态与操作。
export interface UseThemePreferenceResult {
	/** preference 是用户当前选择的主题偏好（含跟随系统）。 */ preference: ThemePreference;
	/** cyclePreference 在 跟随系统 → 浅色 → 深色 之间循环切换并持久化。 */ cyclePreference: () => void;
}

// readSystemPrefersDark 读取系统当前是否偏好深色；不支持 matchMedia 的环境视为浅色。
const readSystemPrefersDark = (): boolean => typeof window.matchMedia === 'function' && window.matchMedia(darkSchemeQuery).matches;

// useThemePreference 持有主题偏好并把实际主题同步到 <html>；偏好为 system 时订阅系统配色变化，卸载时解除订阅。
export const useThemePreference = (): UseThemePreferenceResult => {
	// preference 是用户偏好，初始值来自本地存储。
	const [preference, setPreference] = useState<ThemePreference>(readThemePreference);

	useEffect(/* 当前副作用把偏好同步到 <html>，并在 system 时订阅系统配色。 */ () => {
		// 非 system 偏好与系统配色无关，直接应用。
		if (preference !== 'system' || typeof window.matchMedia !== 'function') {
			applyResolvedTheme(resolveTheme(preference, readSystemPrefersDark()));
			return undefined;
		}
		// query 是系统深色配色的媒体查询对象，用于监听实时变化。
		const query = window.matchMedia(darkSchemeQuery);
		// sync 把当前系统配色应用到页面。
		const sync = (): void => applyResolvedTheme(resolveTheme('system', query.matches));
		sync();
		query.addEventListener('change', sync);
		return /* cleanup 解除系统配色监听，避免偏好切换或卸载后继续修改页面。 */ () => query.removeEventListener('change', sync);
	}, [preference]);

	// cyclePreference 是点击事件处理：计算下一个偏好、写入存储并更新状态。
	const cyclePreference = useCallback((): void => {
		setPreference(/* current 是切换前的偏好，下一个值依赖它。 */ current => {
			// next 是循环后的新偏好。
			const next = nextThemePreference(current);
			writeThemePreference(next);
			return next;
		});
	}, []);

	return { preference, cyclePreference };
};
