import { afterEach, describe, expect, test, vi } from 'vitest';
import { readSidebarCollapsed, writeSidebarCollapsed } from './sidebarState';

afterEach(() => vi.unstubAllGlobals());

describe('sidebar persistence', () => {
	test('defaults to expanded and persists both states', () => {
		const values = new Map<string, string>();
		vi.stubGlobal('window', { localStorage: {
			getItem: (key: string) => values.get(key) ?? null,
			setItem: (key: string, value: string) => values.set(key, value),
		}});
		expect(readSidebarCollapsed()).toBe(false);
		writeSidebarCollapsed(true);
		expect(readSidebarCollapsed()).toBe(true);
		writeSidebarCollapsed(false);
		expect(readSidebarCollapsed()).toBe(false);
	});

	test('storage failures safely fall back to expanded', () => {
		vi.stubGlobal('window', { localStorage: {
			getItem: () => { throw new Error('blocked'); },
			setItem: () => { throw new Error('blocked'); },
		}});
		expect(readSidebarCollapsed()).toBe(false);
		expect(() => writeSidebarCollapsed(true)).not.toThrow();
	});
});
