import React from 'react';
import {
  Bell, Box, ChevronLeft, ChevronRight, CreditCard, FileStack, LayoutDashboard,
  GitCommitHorizontal, LogOut, MessageCircleMore, Settings, ShoppingBag, Users, Zap,
} from 'lucide-react';
import { YdisksBrandIcon } from './YdisksLogo';

// SidebarBuildInfo 描述应用壳传递给侧边栏的公开构建版本信息。
export interface SidebarBuildInfo {
  // version 是当前服务构建版本，只用于界面展示。
  version: string;
  // commit 是当前服务构建提交标识，只用于界面展示。
  commit: string;
}

// SidebarProps 描述侧边栏所需的导航和会话回调。
interface SidebarProps {
  /** activeTab 表示当前Tab。 */ activeTab: string;
  /** isAdmin 表示当前用户是否为管理员。 */ isAdmin?: boolean;
  /** collapsed 表示侧边栏是否折叠。 */ collapsed: boolean;
  /** onToggleCollapsed 表示切换侧边栏折叠状态的回调。 */ onToggleCollapsed: () => void;
  /** onNavigate 表示切换主导航页面的回调。 */ onNavigate: (tab: string) => void;
  /** onLogout 表示注销当前会话的回调。 */ onLogout: () => void;
  /** buildInfo 是由应用壳加载并传入的公开构建版本信息。 */ buildInfo: SidebarBuildInfo;
  /** hasUnreadChatMessage 表示在线聊天入口是否仍有未读消息，需要展示红点。 */ hasUnreadChatMessage?: boolean;
}

// Sidebar 渲染侧边栏导航组件。
const Sidebar: React.FC<SidebarProps> = ({
  activeTab, isAdmin = false, collapsed, onToggleCollapsed, onNavigate, onLogout, buildInfo, hasUnreadChatMessage = false,
}) => {
  // menuItems 侧边栏菜单项。
  const menuItems = [
    { id: 'dashboard', icon: LayoutDashboard, label: '仪表盘' },
    { id: 'accounts', icon: Users, label: '账号管理' },
    { id: 'chat', icon: MessageCircleMore, label: '在线聊天' },
    { id: 'cards', icon: CreditCard, label: '卡密库存' },
    { id: 'items', icon: Box, label: '商品列表' },
    { id: 'orders', icon: ShoppingBag, label: '订单管理' },
    { id: 'delivery-templates', icon: FileStack, label: '发货模板' },
    { id: 'rules', icon: Zap, label: '自动化规则' },
    { id: 'notifications', icon: Bell, label: '通知设置' },
    ...(isAdmin ? [{ id: 'settings', icon: Settings, label: '系统与AI' }] : []),
  ];
  // displayVersion 显示版本号。
  const displayVersion = /^\d+\.\d+\.\d+$/.test(buildInfo.version)
    ? `v${buildInfo.version}`
    : buildInfo.version;

  return (
    <aside className={`fixed inset-y-0 left-0 z-20 flex flex-col border-r border-slate-200/80 bg-white/95 shadow-sidebar backdrop-blur-xl transition-[width] duration-300 ${collapsed ? 'w-16' : 'w-64'}`}>
      <div className={`flex h-20 items-center border-b border-slate-100 ${collapsed ? 'justify-center px-2' : 'gap-3 px-5'}`}>
        <YdisksBrandIcon sizeClass="h-10 w-10" />
        {!collapsed && (
          <div className="min-w-0 leading-tight">
            <div className="truncate text-base font-black tracking-tight text-slate-950">Ydisks 闲鱼助手</div>
            <div className="mt-1 text-[10px] font-extrabold uppercase tracking-[0.22em] text-sky-600">Operations</div>
          </div>
        )}
      </div>

      <nav className={`flex-1 space-y-1.5 overflow-y-auto pt-5 ${collapsed ? 'px-2' : 'px-3'}`} aria-label="主导航">
        {menuItems.map(/* 当前回调处理集合中的单个元素。 */ (item) => {
          // Icon 渲染Icon React 组件。
          const Icon = item.icon;
          // active 当前状态。
          const active = activeTab === item.id;
          return (
            <React.Fragment key={item.id}>
              <button
              type="button"
              title={collapsed ? item.label : undefined}
              aria-label={item.label}
              aria-current={active ? 'page' : undefined}
              onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => onNavigate(item.id)}
              className={`group relative flex h-11 w-full items-center rounded-xl transition-colors ${collapsed ? 'justify-center' : 'gap-3 px-3.5'} ${
                active
                  ? 'bg-brand text-white shadow-brand-active'
                  : 'text-slate-500 hover:bg-slate-100 hover:text-slate-900'
              }`}
            >
              <span className="relative flex h-[19px] w-[19px] shrink-0">
                <Icon className={`h-[19px] w-[19px] ${active ? 'text-white' : 'text-slate-400 group-hover:text-slate-700'}`} />
                {item.id === 'chat' && hasUnreadChatMessage && <span className="absolute -right-1 -top-1 h-3 w-3 rounded-full border-2 border-white bg-red-500" role="status" aria-label="在线聊天有未读消息" />}
              </span>
              {!collapsed && <span className="truncate text-sm font-bold">{item.label}</span>}
              {active && !collapsed && <span className="ml-auto h-1.5 w-1.5 rounded-full bg-white/90" />}
              </button>
            </React.Fragment>
          );
        })}
      </nav>

      <div>
        <div
          title={collapsed ? `版本 ${buildInfo.version} · ${buildInfo.commit}` : undefined}
          className={`border-y border-slate-100 bg-slate-50/70 py-2.5 ${collapsed ? 'flex justify-center px-1' : 'px-6'}`}
        >
          {collapsed ? (
            <GitCommitHorizontal className="h-[18px] w-[18px] text-slate-400" aria-label={`版本 ${buildInfo.version}`} />
          ) : (
            <div className="min-w-0">
              <div className="flex items-center gap-2 text-[10px] font-extrabold uppercase tracking-[0.18em] text-slate-400">
                <GitCommitHorizontal className="h-3.5 w-3.5" />
                运行版本
              </div>
              <div className="mt-1 flex items-baseline gap-2">
                <span className="truncate font-mono text-xs font-bold text-slate-700">{displayVersion}</span>
                <span className="truncate font-mono text-[10px] text-slate-400">{buildInfo.commit}</span>
              </div>
            </div>
          )}
        </div>
        <div className={`space-y-2 p-2 ${collapsed ? '' : 'p-3'}`}>
          <button
            type="button"
            onClick={onToggleCollapsed}
            title={collapsed ? '展开侧边栏' : '收起侧边栏'}
            aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'}
            className={`flex h-10 w-full items-center rounded-xl text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 ${collapsed ? 'justify-center' : 'gap-3 px-3'}`}
          >
            {collapsed ? <ChevronRight className="h-5 w-5" /> : <ChevronLeft className="h-5 w-5" />}
            {!collapsed && <span className="text-sm font-bold">收起侧边栏</span>}
          </button>
          <button
            type="button"
            onClick={onLogout}
            title={collapsed ? '退出登录' : undefined}
            aria-label="退出登录"
            className={`flex h-10 w-full items-center rounded-xl text-slate-500 transition-colors hover:bg-red-50 hover:text-red-600 ${collapsed ? 'justify-center' : 'gap-3 px-3'}`}
          >
            <LogOut className="h-5 w-5" />
            {!collapsed && <span className="text-sm font-bold">退出登录</span>}
          </button>
        </div>
      </div>
    </aside>
  );
};

export default Sidebar;
