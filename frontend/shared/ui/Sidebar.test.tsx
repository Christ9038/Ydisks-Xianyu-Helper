// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import Sidebar from './Sidebar';

describe('Sidebar', /* 当前测试组验证全局聊天新消息状态在侧边栏中的可见标记。 */ () => {
  test('未查看聊天消息时仅在在线聊天入口显示红点', /* 当前回调验证红点出现与消失不依赖页面标题状态。 */ () => {
    // navigationHandler 是侧边栏导航测试使用的无副作用回调。
    const navigationHandler = vi.fn();
    // logoutHandler 是侧边栏注销测试使用的无副作用回调。
    const logoutHandler = vi.fn();
    // collapseHandler 是侧边栏折叠测试使用的无副作用回调。
    const collapseHandler = vi.fn();
    // buildInfo 是渲染侧边栏所需的最小公开构建标识。
    const buildInfo = { version: 'dev', commit: 'test' };
    // view 保存带有新消息状态的侧边栏渲染控制器。
    const view = render(
      <Sidebar
        activeTab="dashboard"
        collapsed={false}
        onToggleCollapsed={collapseHandler}
        onNavigate={navigationHandler}
        onLogout={logoutHandler}
        buildInfo={buildInfo}
        hasUnreadChatMessage
      />,
    );
    // unreadIndicator 表示挂载在聊天图标右上角的未读提示元素。
    const unreadIndicator = screen.getByLabelText('在线聊天有未读消息');
    expect(unreadIndicator).toBeTruthy();
    expect(unreadIndicator.classList.contains('absolute')).toBe(true);
    expect(unreadIndicator.classList.contains('-right-1')).toBe(true);
    expect(unreadIndicator.classList.contains('-top-1')).toBe(true);
    expect(unreadIndicator.classList.contains('h-3')).toBe(true);
    expect(unreadIndicator.classList.contains('w-3')).toBe(true);
    view.rerender(
      <Sidebar
        activeTab="dashboard"
        collapsed={false}
        onToggleCollapsed={collapseHandler}
        onNavigate={navigationHandler}
        onLogout={logoutHandler}
        buildInfo={buildInfo}
        hasUnreadChatMessage={false}
      />,
    );
    expect(screen.queryByLabelText('在线聊天有未读消息')).toBeNull();
  });
});
