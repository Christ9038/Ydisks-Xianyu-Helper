import React, { useEffect, useState } from 'react';
import { readSidebarCollapsed, writeSidebarCollapsed } from '../../shared/browser/sidebarState';
import { SessionGate } from '../features/session/pages/SessionGate';
import { useSession } from '../providers/SessionProvider';
import AuthenticatedShell, { type DeliveryRuleTarget } from '../shell/AuthenticatedShell';
import { pathByRoute, routeFromLocation, type AppRoute } from './routes';

/** AppRouter 管理认证后的浏览器路由、侧边栏偏好与跨页面规则配置载荷。 */
export const AppRouter: React.FC = () => {
  // isLoggedIn 与 isAdmin 是 Provider 拥有的认证服务端状态，仅用于选择应用壳和授权路由。
  const { isLoggedIn, isAdmin, signOut } = useSession();
  // activeRoute 是当前 URL 对应的短暂导航状态，浏览器前进后退会同步更新。
  const [activeRoute, setActiveRoute] = useState<AppRoute>(routeFromLocation);
  // deliveryRuleTarget 是商品页发起、规则页消费后即清除的短暂跨页面载荷。
  const [deliveryRuleTarget, setDeliveryRuleTarget] = useState<DeliveryRuleTarget | undefined>();
  // sidebarCollapsed 是用户本地偏好，不属于服务端业务数据。
  const [sidebarCollapsed, setSidebarCollapsed] = useState(readSidebarCollapsed);

  /** effect 同步浏览器历史事件，并在路由退回时拒绝非管理员设置页。 */
  useEffect(/* historyEffect 负责订阅并在卸载时移除浏览器历史监听。 */ () => {
    /** handlePopState 读取历史记录对应的路由，不从异步请求恢复旧状态。 */
    const handlePopState = (): void => setActiveRoute(routeFromLocation());
    window.addEventListener('popstate', handlePopState);
    return /* historyCleanup 在路由组件卸载时释放全局历史监听。 */ () => window.removeEventListener('popstate', handlePopState);
  }, []);

  /** effect 在权限变化时将设置页改写为仪表盘，防止地址栏绕过客户端展示限制。 */
  useEffect(/* authorizationEffect 负责将失效的管理员页面安全回退。 */ () => {
    if (isLoggedIn && !isAdmin && activeRoute === 'settings') {
      window.history.replaceState({}, '', pathByRoute.dashboard);
      setActiveRoute('dashboard');
    }
  }, [activeRoute, isAdmin, isLoggedIn]);

  /** navigate 由侧边栏用户操作触发，写入规范 URL 并更新当前路由。 */
  const navigate = (route: AppRoute): void => {
    // permittedRoute 是应用当前权限允许的最终路由。
    const permittedRoute = route === 'settings' && !isAdmin ? 'dashboard' : route;
    // nextPath 是最终路由对应的规范浏览器地址。
    const nextPath = pathByRoute[permittedRoute];
    if (nextPath !== window.location.pathname) window.history.pushState({}, '', nextPath);
    setActiveRoute(permittedRoute);
  };

  /** handleShellNavigation 接收侧边栏的字符串标识，并拒绝不在应用路由表中的遗留值。 */
  const handleShellNavigation = (tab: string): void => {
    // route 是经规范路由表确认后的路由；未知标签一律回退到仪表盘。
    const route = Object.prototype.hasOwnProperty.call(pathByRoute, tab) ? tab as AppRoute : 'dashboard';
    navigate(route);
  };

  /** handleLogout 注销服务端会话；失败仍由 Provider 清理本地认证状态。 */
  const handleLogout = async (): Promise<void> => {
    try {
      await signOut();
    } catch (error /* error 是注销请求失败原因，仅记录通用错误对象而不输出敏感会话内容。 */) {
      console.error('退出登录失败', error);
    }
  };

  /** handleToggleSidebar 由侧边栏按钮触发，并用函数式更新持久化下一次渲染所需偏好。 */
  const handleToggleSidebar = (): void => {
    setSidebarCollapsed(/* preferenceUpdater 基于当前偏好计算并持久化下一次显示状态。 */ previousCollapsed /* previousCollapsed 是变更前的本地侧边栏偏好。 */ => {
      // nextCollapsed 是本次点击后需要写入浏览器存储的偏好值。
      const nextCollapsed = !previousCollapsed;
      writeSidebarCollapsed(nextCollapsed);
      return nextCollapsed;
    });
  };

  /** handleConfigureDelivery 保存商品页选择的目标，并导航到消费该目标的规则页面。 */
  const handleConfigureDelivery = (target: DeliveryRuleTarget): void => {
    setDeliveryRuleTarget(target);
    navigate('rules');
  };

  /** handleDeliveryTargetHandled 在规则页消费一次性联动目标后释放短暂 UI 状态。 */
  const handleDeliveryTargetHandled = (): void => setDeliveryRuleTarget(undefined);

  if (!isLoggedIn) return <SessionGate />;

  return (
    <AuthenticatedShell
      activeTab={activeRoute}
      isAdmin={isAdmin}
      collapsed={sidebarCollapsed}
      deliveryRuleTarget={deliveryRuleTarget}
      onToggleCollapsed={handleToggleSidebar}
      onNavigate={handleShellNavigation}
      onLogout={handleLogout}
      onConfigureDelivery={handleConfigureDelivery}
      onDeliveryTargetHandled={handleDeliveryTargetHandled}
    />
  );
};
