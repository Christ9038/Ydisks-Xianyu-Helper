// chatAccountTargetStorageKey 是一次性账号→聊天交接的会话存储键；读取后立即清除，避免后续导航复用到旧账号。
const chatAccountTargetStorageKey = 'ydisks.chat.targetAccount.v1';

// requestChatAccount 记录用户希望进入聊天页时优先选中的账号；空标识不写入，存储不可用时保持普通导航行为。
export const requestChatAccount = (accountID: string): void => {
	// normalizedAccountID 保存去除首尾空白后的账号标识，避免把空字符串当作有效目标。
	const normalizedAccountID = accountID.trim();
	if (!normalizedAccountID) return;
	try {
		window.sessionStorage.setItem(chatAccountTargetStorageKey, normalizedAccountID);
	} catch {
		// Storage can be unavailable in hardened browsers; navigation still works with the default account selection.
	}
};

// consumeChatAccountTarget 取出并清除本次账号→聊天交接目标；没有目标或存储不可用时返回空字符串。
export const consumeChatAccountTarget = (): string => {
	try {
		// targetAccountID 保存当前会话存储中的一次性目标账号标识。
		const targetAccountID = window.sessionStorage.getItem(chatAccountTargetStorageKey) || '';
		if (targetAccountID) window.sessionStorage.removeItem(chatAccountTargetStorageKey);
		return targetAccountID;
	} catch {
		return '';
	}
};
