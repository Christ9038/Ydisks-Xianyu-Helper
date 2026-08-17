// storageKey 侧边栏折叠状态存储键。
const storageKey = 'ydisks.sidebar.v1';

// readSidebarCollapsed 读取侧边栏折叠状态。
export const readSidebarCollapsed = (): boolean => {
	try {
		return window.localStorage.getItem(storageKey) === 'collapsed';
	} catch {
		return false;
	}
};

// writeSidebarCollapsed 写入侧边栏折叠状态。
export const writeSidebarCollapsed = (collapsed: boolean): void => {
	try {
		window.localStorage.setItem(storageKey, collapsed ? 'collapsed' : 'expanded');
	} catch {
		// Storage can be unavailable in hardened browsers; the in-memory state
		// remains fully functional.
	}
};
