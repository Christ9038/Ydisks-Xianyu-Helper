// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { themeStorageKey } from './themePreference';
import { useThemePreference } from './useThemePreference';

// FakeQuery 是可手动触发 change 的 matchMedia 查询替身。
interface FakeQuery {
	/** matches 表示系统当前是否偏好深色。 */ matches: boolean;
	/** listeners 是已注册的 change 监听器集合。 */ listeners: Set<() => void>;
}

describe('useThemePreference', /* 当前测试组验证主题 Hook 对存储、系统配色和卸载的处理。 */ () => {
	// query 是当前用例使用的系统配色替身。
	let query: FakeQuery;

	beforeEach(/* 安装 matchMedia 替身并清空存储。 */ () => {
		query = { matches: false, listeners: new Set() };
		window.localStorage.clear();
		// 以替身安装 matchMedia，使测试可手动触发系统配色变化。
		window.matchMedia = vi.fn(/* 返回共享的替身查询。 */ () => ({
			// matches 实时读取替身当前的深色状态。
			get matches() { return query.matches; },
			// addEventListener 登记 change 监听器。
			addEventListener: /* 当前回调登记监听器。 */ (_type: string, listener: () => void) => query.listeners.add(listener),
			// removeEventListener 移除 change 监听器。
			removeEventListener: /* 当前回调移除监听器。 */ (_type: string, listener: () => void) => query.listeners.delete(listener),
		})) as unknown as typeof window.matchMedia;
	});

	afterEach(/* 清理 DOM 属性。 */ () => {
		delete document.documentElement.dataset.theme;
	});

	test('system 偏好跟随系统配色并响应实时变化', /* 当前回调模拟系统由浅变深。 */ () => {
		// result 持有 Hook 的最新返回值。
		const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useThemePreference());
		expect(result.current.preference).toBe('system');
		expect(document.documentElement.dataset.theme).toBe('light');
		act(/* 系统切换到深色并通知监听器。 */ () => { query.matches = true; query.listeners.forEach(/* listener 是已注册监听器。 */ listener => listener()); });
		expect(document.documentElement.dataset.theme).toBe('dark');
	});

	test('点击切换依次进入浅色、深色并写入存储', /* 当前回调验证循环与持久化。 */ () => {
		query.matches = true;
		// result 持有 Hook 的最新返回值。
		const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useThemePreference());
		expect(document.documentElement.dataset.theme).toBe('dark');
		act(/* 切换到浅色。 */ () => result.current.cyclePreference());
		expect(result.current.preference).toBe('light');
		expect(document.documentElement.dataset.theme).toBe('light');
		expect(window.localStorage.getItem(themeStorageKey)).toBe('light');
		expect(query.listeners.size).toBe(0);
		act(/* 切换到深色。 */ () => result.current.cyclePreference());
		expect(document.documentElement.dataset.theme).toBe('dark');
	});

	test('读取已保存的偏好作为初始值', /* 当前回调验证刷新后记忆。 */ () => {
		window.localStorage.setItem(themeStorageKey, 'dark');
		// result 持有 Hook 的最新返回值。
		const { result } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useThemePreference());
		expect(result.current.preference).toBe('dark');
		expect(document.documentElement.dataset.theme).toBe('dark');
	});

	test('卸载时解除系统配色监听', /* 当前回调防止卸载后继续修改页面。 */ () => {
		// unmount 卸载被测 Hook。
		const { unmount } = renderHook(/* 当前回调渲染被测 Hook。 */ () => useThemePreference());
		expect(query.listeners.size).toBe(1);
		unmount();
		expect(query.listeners.size).toBe(0);
	});

	test('不支持 matchMedia 的环境视为浅色', /* 当前回调覆盖降级分支。 */ () => {
		// legacyWindow 是去掉 matchMedia 类型约束的 window 视图，用于模拟旧浏览器；matchMedia 置空覆盖不支持该 API 的环境。
		const legacyWindow = window as { /** matchMedia 是可被置空的媒体查询入口。 */ matchMedia?: unknown };
		Reflect.set(legacyWindow, 'matchMedia', undefined);
		renderHook(/* 当前回调渲染被测 Hook。 */ () => useThemePreference());
		expect(document.documentElement.dataset.theme).toBe('light');
	});
});
