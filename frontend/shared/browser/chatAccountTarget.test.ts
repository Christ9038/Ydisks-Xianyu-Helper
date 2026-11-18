// @vitest-environment jsdom
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { consumeChatAccountTarget,requestChatAccount } from './chatAccountTarget';

// clearChatAccountTargetStorage 清除用例间残留的一次性目标账号，避免后续用例读到前一个用例写入的值。
const clearChatAccountTargetStorage = (): void => {
	window.sessionStorage.clear();
};

describe('账号到聊天的一次性目标交接', /* 当前回调覆盖目标账号写入、消费和存储异常降级边界。 */ () => {
	beforeEach(/* 当前回调在每个用例开始前清空会话存储。 */ () => {
		clearChatAccountTargetStorage();
	});

	afterEach(/* 当前回调恢复存储方法替身并清空会话存储。 */ () => {
		vi.restoreAllMocks();
		clearChatAccountTargetStorage();
	});

	test('写入时去除账号标识首尾空白，消费后返回并清除目标', /* 当前回调验证写入规范化和消费清理。 */ () => {
		requestChatAccount('  account-42  ');
		expect(consumeChatAccountTarget()).toBe('account-42');
		expect(window.sessionStorage.length).toBe(0);
	});

	test('空白账号标识不写入存储', /* 当前回调验证空目标保持普通导航。 */ () => {
		requestChatAccount('   ');
		expect(consumeChatAccountTarget()).toBe('');
		expect(window.sessionStorage.length).toBe(0);
	});

	test('同一目标只允许消费一次', /* 当前回调验证一次性交接不会复用旧账号。 */ () => {
		requestChatAccount('account-9');
		expect(consumeChatAccountTarget()).toBe('account-9');
		expect(consumeChatAccountTarget()).toBe('');
	});

	test('会话存储不可用时写入和消费保持静默降级', /* 当前回调验证受限浏览器中的存储异常不会阻断导航。 */ () => {
		// setItemSpy 是模拟会话存储禁止写入的方法替身。
		const setItemSpy = vi.spyOn(window.Storage.prototype, 'setItem').mockImplementation(/* 写入失败模拟。 */ () => {
			throw new Error('blocked');
		});
		// getItemSpy 是模拟会话存储禁止读取的方法替身。
		const getItemSpy = vi.spyOn(window.Storage.prototype, 'getItem').mockImplementation(/* 读取失败模拟。 */ () => {
			throw new Error('blocked');
		});
		expect(/* 写入异常应被模块内部吞掉。 */ () => requestChatAccount('account-1')).not.toThrow();
		expect(consumeChatAccountTarget()).toBe('');
		expect(setItemSpy).toHaveBeenCalled();
		expect(getItemSpy).toHaveBeenCalled();
	});
});
