import React,{ lazy,Suspense,useEffect,useState } from 'react';
import type { Item } from '../features/items/api';
import Sidebar from '../../shared/ui/Sidebar';
import { useChatTitleNotification } from '../features/chat/titleNotification';
import { getHealth } from '../features/system/api';
import type { BuildInfo } from '../features/system/types';

// DeliveryRuleTarget 描述商品页跳转到自动化规则页时需要携带的目标信息。
export interface DeliveryRuleTarget {
  // cookieId 表示目标闲鱼账号标识。
  cookieId: string;
  // itemId 表示目标商品标识。
  itemId: string;
  // requestId 用于区分连续发起的跳转请求。
  requestId: number;
}

// AuthenticatedShellProps 描述认证后应用壳需要的导航、权限和联动回调。
export interface AuthenticatedShellProps {
  // activeTab 表示当前正在展示的业务页面标识。
  activeTab: string;
  // isAdmin 表示当前会话是否拥有管理员权限。
  isAdmin: boolean;
  // collapsed 表示侧边栏是否处于折叠状态。
  collapsed: boolean;
  // deliveryRuleTarget 表示待传递给规则页的商品发货目标。
  deliveryRuleTarget?: DeliveryRuleTarget;
  // onToggleCollapsed 负责切换侧边栏折叠状态。
  onToggleCollapsed: () => void;
  // onNavigate 负责切换业务页面并同步外部路由。
  onNavigate: (tab: string) => void;
  // onLogout 负责注销当前会话并清理认证状态。
  onLogout: () => void;
  // onConfigureDelivery 负责接收商品页发起的规则配置目标。
  onConfigureDelivery: (target: DeliveryRuleTarget) => void;
  // onDeliveryTargetHandled 负责确认规则页已经消费跳转目标。
  onDeliveryTargetHandled: () => void;
}

// Dashboard 是按需加载的仪表盘页面，避免首屏同步载入图表依赖。
const Dashboard = lazy(/* Dashboard 页面按路由激活时加载。 */ () => import('../features/dashboard/pages/Dashboard'));
// AccountList 是按需加载的账号管理页面，避免未访问时载入账号弹窗和二维码代码。
const AccountList = lazy(/* AccountList 页面按路由激活时加载。 */ () => import('../features/accounts/pages/AccountList'));
// OrderList 是按需加载的订单页面，避免首屏载入订单导入与刷新代码。
const OrderList = lazy(/* OrderList 页面按路由激活时加载。 */ () => import('../features/orders/pages/OrderList'));
// CardList 是按需加载的卡密页面，避免首屏载入卡密批量处理代码。
const CardList = lazy(/* CardList 页面按路由激活时加载。 */ () => import('../features/cards/pages/CardList'));
// ItemList 是按需加载的商品页面，避免首屏载入商品发布编辑器代码。
const ItemList = lazy(/* ItemList 页面按路由激活时加载。 */ () => import('../features/items/pages/ItemList'));
// Settings 是按需加载的系统设置页面，仅在管理员访问时加载。
const Settings = lazy(/* Settings 页面按路由激活时加载。 */ () => import('../features/settings/pages/Settings'));
// Rules 是按需加载的自动化规则页面，避免首屏载入规则编辑器代码。
const Rules = lazy(/* Rules 页面按路由激活时加载。 */ () => import('../features/rules/pages/Rules'));
// DeliveryTemplates 是按需加载的发货模板管理页面。
const DeliveryTemplates = lazy(/* DeliveryTemplates 页面按路由激活时加载。 */ () => import('../features/delivery-templates/pages/DeliveryTemplates'));
// Notifications 是按需加载的通知页面，避免首屏载入通知配置代码。
const Notifications = lazy(/* Notifications 页面按路由激活时加载。 */ () => import('../features/notifications/pages/Notifications'));
// Chat 是按需加载的聊天页面，避免未访问时载入聊天历史和 WebSocket 视图。
const Chat = lazy(/* Chat 页面按路由激活时加载。 */ () => import('../features/chat/pages/Chat'));

// PageLoading 展示路由页面代码加载期间的统一占位状态。
const PageLoading: React.FC = () => (
  <div className="flex min-h-[24rem] items-center justify-center" role="status" aria-label="正在加载页面">
    <div className="h-8 w-8 animate-spin rounded-full border-4 border-brand/20 border-t-brand" />
  </div>
);

// AppContentProps 描述页面组合器接收的路由和跨页面联动状态。
export interface AppContentProps {
  // activeTab 表示当前需要渲染的业务页面标识。
  activeTab: string;
  // isAdmin 表示当前会话是否允许访问管理员页面。
  isAdmin: boolean;
  // deliveryRuleTarget 表示商品页传递给规则页的目标信息。
  deliveryRuleTarget?: DeliveryRuleTarget;
  // onConfigureDelivery 负责接收商品页面发起的规则配置目标。
  onConfigureDelivery: (target: DeliveryRuleTarget) => void;
  // onDeliveryTargetHandled 负责确认规则页已经消费跳转目标。
  onDeliveryTargetHandled: () => void;
}

// AppContent 按当前导航标识选择业务页面，并隔离页面代码的动态加载边界。
export const AppContent: React.FC<AppContentProps> = ({
  activeTab,
  isAdmin,
  deliveryRuleTarget,
  onConfigureDelivery,
  onDeliveryTargetHandled,
}) => {
  // handleConfigureDelivery 将商品页面对象转换成路由壳使用的最小联动载荷。
  const handleConfigureDelivery = (item /* item 表示用户选择的商品。 */: Item) => {
    onConfigureDelivery({
      cookieId: item.cookie_id,
      itemId: item.item_id,
      requestId: Date.now(),
    });
  };

  // renderPage 根据当前页面标识选择唯一的业务页面组件。
  const renderPage = () => {
    switch (activeTab) {
      case 'dashboard': return <Dashboard />;
      case 'accounts': return <AccountList />;
      case 'chat': return <Chat />;
      case 'orders': return <OrderList />;
      case 'cards': return <CardList />;
      case 'items': return <ItemList onConfigureDelivery={handleConfigureDelivery} />;
      case 'rules': return <Rules
        initialDeliveryTarget={deliveryRuleTarget}
        onDeliveryTargetHandled={onDeliveryTargetHandled}
      />;
      case 'delivery-templates': return <DeliveryTemplates />;
      case 'notifications': return <Notifications isAdmin={isAdmin} />;
      case 'settings': return isAdmin ? <Settings /> : <Dashboard />;
      default: return <Dashboard />;
    }
  };

  return (
    <Suspense fallback={<PageLoading />}>
      {renderPage()}
    </Suspense>
  );
};

// AuthenticatedShell 组合认证后的侧边栏、主内容区域和页面动态加载边界。
const AuthenticatedShell: React.FC<AuthenticatedShellProps> = ({
  activeTab,
  isAdmin,
  collapsed,
  deliveryRuleTarget,
  onToggleCollapsed,
  onNavigate,
  onLogout,
  onConfigureDelivery,
  onDeliveryTargetHandled,
}) => {
  // buildInfo 保存壳层加载的公开构建版本，侧边栏保持为无请求的共享展示组件。
  const [buildInfo, setBuildInfo] = useState<BuildInfo>({ version: 'dev', commit: 'unknown' });
  // hasUnreadChatMessage 保存侧边栏在线聊天入口的服务端/实时聚合未读状态，不因导航动作改变。
  const { hasUnreadChatMessage } = useChatTitleNotification();

  useEffect(/* effect 在壳层挂载时读取版本信息，并在卸载时中止尚未完成的请求。 */ () => {
    // controller 取消壳层卸载后不再需要的健康检查请求。
    const controller = new AbortController();
    getHealth({ signal: controller.signal })
      .then(/* response 是健康接口返回的公开构建标识。 */ response => setBuildInfo({
        version: String(response.version || 'dev'),
        commit: String(response.commit || 'unknown'),
      }))
      .catch(/* error 是健康检查失败原因，取消请求不需要改变默认展示版本。 */ error => {
        if (error instanceof DOMException && error.name === 'AbortError') return;
      });
    return /* cleanup 在壳层卸载时释放健康检查请求。 */ () => controller.abort();
  }, []);

  return (
    <div className="flex min-h-screen bg-canvas text-ink">
    <Sidebar
      activeTab={activeTab}
      isAdmin={isAdmin}
      collapsed={collapsed}
      onToggleCollapsed={onToggleCollapsed}
      onNavigate={onNavigate}
      onLogout={onLogout}
      buildInfo={buildInfo}
      hasUnreadChatMessage={hasUnreadChatMessage}
    />

    <main className={`h-screen min-w-0 flex-1 overflow-x-hidden overflow-y-auto scroll-smooth transition-[margin] duration-300 ${collapsed ? 'ml-16' : 'ml-64'} ${activeTab === 'chat' ? 'p-4 md:p-6' : 'p-8 md:p-12'}`}>
      {/* 主内容区域的背景装饰不参与交互，避免遮挡业务页面。 */}
      <div className="fixed top-0 right-0 -z-10 h-[800px] w-[800px] pointer-events-none rounded-full bg-gradient-to-bl from-blue-50 to-transparent opacity-60 blur-[120px]" />

      <div className={`${activeTab === 'chat' ? 'mx-auto max-w-[1680px]' : 'mx-auto max-w-[1400px] pb-10'}`}>
        <AppContent
          activeTab={activeTab}
          isAdmin={isAdmin}
          deliveryRuleTarget={deliveryRuleTarget}
          onConfigureDelivery={onConfigureDelivery}
          onDeliveryTargetHandled={onDeliveryTargetHandled}
        />
      </div>
    </main>
    </div>
  );
};

export default AuthenticatedShell;
