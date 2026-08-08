const storageKey = 'ydisks.sidebar.v1';

export const readSidebarCollapsed = (): boolean => {
	try {
		return window.localStorage.getItem(storageKey) === 'collapsed';
	} catch {
		return false;
	}
};

export const writeSidebarCollapsed = (collapsed: boolean): void => {
	try {
		window.localStorage.setItem(storageKey, collapsed ? 'collapsed' : 'expanded');
	} catch {
		// Storage can be unavailable in hardened browsers; the in-memory state
		// remains fully functional.
	}
};
