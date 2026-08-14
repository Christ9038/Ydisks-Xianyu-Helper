import { afterEach, describe, expect, test, vi } from 'vitest';
import { readSidebarCollapsed, writeSidebarCollapsed } from './sidebarState';

afterEach(/* 当前回调处理用户交互或异步状态变化。 */ () => vi.unstubAllGlobals());

describe('sidebar persistence', /* 当前回调处理用户交互或异步状态变化。 */ () => {
	test('defaults to expanded and persists both states', /* 当前回调处理用户交互或异步状态变化。 */ () => {
		// values 值列表。
		const values = new Map<string, string>();
		vi.stubGlobal('window', { localStorage: {
			getItem: /* 当前回调处理用户交互或异步状态变化。 */ (key: string) => values.get(key) ?? null,
			setItem: /* 当前回调处理用户交互或异步状态变化。 */ (key: string, value: string) => values.set(key, value),
		}});
		expect(readSidebarCollapsed()).toBe(false);
		writeSidebarCollapsed(true);
		expect(readSidebarCollapsed()).toBe(true);
		writeSidebarCollapsed(false);
		expect(readSidebarCollapsed()).toBe(false);
	});

	test('storage failures safely fall back to expanded', /* 当前回调处理用户交互或异步状态变化。 */ () => {
		vi.stubGlobal('window', { localStorage: {
			getItem: /* 当前回调处理用户交互或异步状态变化。 */ () => { throw new Error('blocked'); },
			setItem: /* 当前回调处理用户交互或异步状态变化。 */ () => { throw new Error('blocked'); },
		}});
		expect(readSidebarCollapsed()).toBe(false);
		expect(/* 当前回调处理用户交互或异步状态变化。 */ () => writeSidebarCollapsed(true)).not.toThrow();
	});
});
