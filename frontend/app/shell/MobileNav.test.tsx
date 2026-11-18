// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { MobileNavProps } from './MobileNav';
import MobileNav from './MobileNav';

/** buildInfoFixture 是移动端导航测试使用的公开构建版本，避免依赖真实健康接口。 */
const buildInfoFixture = { version: '1.2.3', commit: 'abc1234' };

/** renderMobileNav 用默认回调渲染底部导航，并允许单个用例覆盖权限、未读和活动页面。 */
const renderMobileNav = (overrides /* overrides 是当前用例需要覆盖的导航属性。 */: Partial<MobileNavProps> = {}) => {
  /** onNavigate 记录用户触发的页面跳转标识。 */
  const onNavigate = vi.fn<(tab: string) => void>();
  /** onLogout 记录用户触发的退出登录动作。 */
  const onLogout = vi.fn<() => void>();
  render(
    <MobileNav
      activeTab="dashboard"
      isAdmin={false}
      buildInfo={buildInfoFixture}
      hasUnreadChatMessage={false}
      onNavigate={onNavigate}
      onLogout={onLogout}
      {...overrides}
    />,
  );
  return { onNavigate, onLogout };
};

/** openMoreSheet 点击“更多”主入口，等待抽屉在 DOM 中出现。 */
const openMoreSheet = (): void => {
  fireEvent.click(screen.getByRole('button', { name: '更多' }));
};

describe('MobileNav 移动端底部导航', /* 当前回调汇总底部主导航和“更多”抽屉的交互契约。 */ () => {
  afterEach(/* 当前回调清理每个用例挂载的 DOM，避免抽屉和按钮状态串扰。 */ () => cleanup());

  test('渲染五个主入口并高亮当前页面', /* 当前回调验证主入口齐全且活动入口使用品牌令牌。 */ () => {
    renderMobileNav({ activeTab: 'chat' });
    for (const label /* label 是当前需要确认存在的主入口中文名称。 */ of ['仪表盘', '聊天', '账号', '订单', '更多']) {
      expect(screen.getByRole('button', { name: label })).toBeTruthy();
    }
    /** chatButton 表示当前活动页面对应的聊天入口。 */
    const chatButton = screen.getByRole('button', { name: '聊天' });
    expect(chatButton.getAttribute('aria-current')).toBe('page');
    expect(chatButton.className).toContain('bg-brand/10');
    expect(chatButton.className).toContain('text-brand');
    expect(screen.getByRole('button', { name: '仪表盘' }).getAttribute('aria-current')).toBeNull();

    cleanup();
    renderMobileNav({ activeTab: 'library' });
    /** moreButton 表示次级页面中应保持高亮的“更多”入口。 */
    const moreButton = screen.getByRole('button', { name: '更多' });
    expect(moreButton.getAttribute('aria-current')).toBe('page');
    expect(screen.queryByRole('dialog')).toBeNull();
  } /* 当前回调断言主入口活动态和“更多”入口的次级页面高亮。 */);

  test('未读聊天消息在聊天入口显示红点', /* 当前回调验证未读状态只在开启时渲染提示。 */ () => {
    renderMobileNav({ hasUnreadChatMessage: true });
    expect(screen.getByRole('status', { name: '在线聊天有未读消息' })).toBeTruthy();

    cleanup();
    renderMobileNav({ hasUnreadChatMessage: false });
    expect(screen.queryByRole('status', { name: '在线聊天有未读消息' })).toBeNull();
  } /* 当前回调断言聊天红点跟随未读状态显示和隐藏。 */);

  test('点击主入口触发导航回调', /* 当前回调验证底部入口直接调用应用壳导航函数。 */ () => {
    // onNavigate 记录本次点击应传出的业务页面标识。
    const { onNavigate } = renderMobileNav({ activeTab: 'dashboard' });
    fireEvent.click(screen.getByRole('button', { name: '订单' }));
    expect(onNavigate).toHaveBeenCalledWith('orders');
    expect(screen.queryByRole('dialog')).toBeNull();
  } /* 当前回调断言订单入口把稳定的业务标识交给应用壳。 */);

  test('更多抽屉展示次级入口并在导航后关闭', /* 当前回调验证抽屉条目和导航后的关闭行为。 */ () => {
    // onNavigate 记录抽屉条目选择后交给应用壳的页面标识。
    const { onNavigate } = renderMobileNav({ activeTab: 'dashboard' });
    openMoreSheet();
    expect(screen.getByRole('dialog')).toBeTruthy();
    for (const label /* label 是抽屉中应出现的常规次级入口中文名称。 */ of ['本地资源库', '卡密库存', '商品列表', '销售统计', '发货模板', '自动化规则', '通知设置']) {
      expect(screen.getByRole('button', { name: label })).toBeTruthy();
    }

    fireEvent.click(screen.getByRole('button', { name: '商品列表' }));
    expect(onNavigate).toHaveBeenCalledWith('items');
    expect(screen.queryByRole('dialog')).toBeNull();
  } /* 当前回调断言抽屉完整列出次级页面且选择后自动收起。 */);

  test('管理员在更多抽屉中看到系统与日志入口', /* 当前回调验证管理员权限增加系统页面入口。 */ () => {
    renderMobileNav({ isAdmin: true });
    openMoreSheet();

    expect(screen.getByRole('button', { name: '系统与AI' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '运行日志' })).toBeTruthy();
  } /* 当前回调断言管理员抽屉包含系统与AI和运行日志入口。 */);

  test('普通用户在更多抽屉中看不到管理员入口', /* 当前回调验证普通用户无法从移动端发现管理员页面。 */ () => {
    renderMobileNav({ isAdmin: false });
    openMoreSheet();

    expect(screen.queryByRole('button', { name: '系统与AI' })).toBeNull();
    expect(screen.queryByRole('button', { name: '运行日志' })).toBeNull();
  } /* 当前回调断言普通用户抽屉不渲染管理员专属入口。 */);

  test('更多抽屉展示构建版本并支持退出登录', /* 当前回调验证版本信息和退出动作。 */ () => {
    // onLogout 记录抽屉退出按钮触发的会话注销次数。
    const { onLogout } = renderMobileNav({ isAdmin: true });
    openMoreSheet();

    expect(screen.getByText('v1.2.3')).toBeTruthy();
    expect(screen.getByText('abc1234')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '退出登录' }));
    expect(onLogout).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).toBeNull();
  } /* 当前回调断言抽屉展示公开版本并先收起再注销会话。 */);

  test('Escape 与遮罩点击都能关闭更多抽屉', /* 当前回调验证键盘和遮罩两种关闭路径。 */ () => {
    renderMobileNav({ isAdmin: false });
    openMoreSheet();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();

    openMoreSheet();
    fireEvent.click(screen.getByTestId('mobile-nav-backdrop'));
    expect(screen.queryByRole('dialog')).toBeNull();
  } /* 当前回调断言 Escape 和遮罩点击都不会触发页面导航。 */);
});
