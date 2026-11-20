// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import ThemeToggle from './ThemeToggle';

describe('ThemeToggle', /* 当前测试组验证主题切换按钮的文案与点击行为。 */ () => {
	afterEach(/* 每个用例后卸载渲染内容，避免按钮重复。 */ cleanup);

	test('显示当前偏好名称并在点击时触发切换', /* 当前回调验证三种偏好文案与回调。 */ () => {
		// onCycle 是点击回调替身。
		const onCycle = vi.fn();
		// view 用于在偏好变化后重新渲染。
		const view = render(<ThemeToggle preference="system" onCycle={onCycle} />);
		expect(screen.getByText('跟随系统')).toBeTruthy();
		fireEvent.click(screen.getByRole('button'));
		expect(onCycle).toHaveBeenCalledTimes(1);
		view.rerender(<ThemeToggle preference="light" onCycle={onCycle} />);
		expect(screen.getByText('浅色模式')).toBeTruthy();
		view.rerender(<ThemeToggle preference="dark" onCycle={onCycle} />);
		expect(screen.getByRole('button').getAttribute('aria-label')).toContain('深色模式');
	});

	test('compact 模式只显示图标但保留可访问名称', /* 当前回调覆盖侧边栏折叠态。 */ () => {
		render(<ThemeToggle preference="dark" onCycle={vi.fn()} compact />);
		expect(screen.queryByText('深色模式')).toBeNull();
		expect(screen.getByRole('button', { name: /深色模式/ })).toBeTruthy();
	});
});
