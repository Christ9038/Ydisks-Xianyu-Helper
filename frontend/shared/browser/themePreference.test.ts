// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import {
	applyResolvedTheme, isThemePreference, nextThemePreference, readThemePreference, resolveTheme, themeStorageKey,
	writeThemePreference,
} from './themePreference';

describe('themePreference', /* 当前测试组验证主题偏好的存储、解析与切换顺序。 */ () => {
	afterEach(/* 每个用例后恢复存储与 DOM 状态，避免用例互相污染。 */ () => {
		window.localStorage.clear();
		delete document.documentElement.dataset.theme;
		vi.restoreAllMocks();
	});

	test('未保存或保存了非法值时回退为跟随系统', /* 当前回调覆盖空值与被篡改的存储值。 */ () => {
		expect(readThemePreference()).toBe('system');
		window.localStorage.setItem(themeStorageKey, 'purple');
		expect(readThemePreference()).toBe('system');
	});

	test('写入后可读回三种合法偏好', /* 当前回调验证持久化往返。 */ () => {
		for (const value /* value 是待往返验证的合法偏好。 */ of ['system', 'light', 'dark'] as const) {
			writeThemePreference(value);
			expect(readThemePreference()).toBe(value);
		}
	});

	test('存储读写抛错时降级而不是崩溃', /* 当前回调模拟加固浏览器禁用存储。 */ () => {
		vi.spyOn(Storage.prototype, 'getItem').mockImplementation(/* 模拟读取被拒绝。 */ () => { throw new Error('denied'); });
		vi.spyOn(Storage.prototype, 'setItem').mockImplementation(/* 模拟写入被拒绝。 */ () => { throw new Error('denied'); });
		expect(readThemePreference()).toBe('system');
		expect(/* 当前回调在存储被拒绝时执行一次写入。 */ () => writeThemePreference('dark')).not.toThrow();
	});

	test('resolveTheme 在 system 下跟随系统，固定偏好忽略系统', /* 当前回调覆盖全部组合。 */ () => {
		expect(resolveTheme('system', true)).toBe('dark');
		expect(resolveTheme('system', false)).toBe('light');
		expect(resolveTheme('light', true)).toBe('light');
		expect(resolveTheme('dark', false)).toBe('dark');
	});

	test('切换顺序为 跟随系统 → 浅色 → 深色 → 跟随系统', /* 当前回调验证循环闭合。 */ () => {
		expect(nextThemePreference('system')).toBe('light');
		expect(nextThemePreference('light')).toBe('dark');
		expect(nextThemePreference('dark')).toBe('system');
	});

	test('isThemePreference 只接受三个枚举值', /* 当前回调覆盖类型守卫。 */ () => {
		expect(isThemePreference('dark')).toBe(true);
		expect(isThemePreference('DARK')).toBe(false);
		expect(isThemePreference(null)).toBe(false);
	});

	test('applyResolvedTheme 写入 html 的 data-theme', /* 当前回调验证样式开关属性。 */ () => {
		applyResolvedTheme('dark');
		expect(document.documentElement.dataset.theme).toBe('dark');
		applyResolvedTheme('light');
		expect(document.documentElement.dataset.theme).toBe('light');
	});
});
