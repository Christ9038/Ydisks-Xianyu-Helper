import React from 'react';
import {
  Bell, Box, ChevronLeft, ChevronRight, CreditCard, LayoutDashboard,
  LogOut, MessageCircleMore, Settings, ShoppingBag, Users, Zap,
} from 'lucide-react';
import { YdisksBrandIcon } from './YdisksLogo';

interface SidebarProps {
  activeTab: string;
  isAdmin?: boolean;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  onNavigate: (tab: string) => void;
  onLogout: () => void;
}

const Sidebar: React.FC<SidebarProps> = ({
  activeTab, isAdmin = false, collapsed, onToggleCollapsed, onNavigate, onLogout,
}) => {
  const menuItems = [
    { id: 'dashboard', icon: LayoutDashboard, label: '仪表盘' },
    { id: 'accounts', icon: Users, label: '账号管理' },
    { id: 'chat', icon: MessageCircleMore, label: '在线聊天' },
    { id: 'cards', icon: CreditCard, label: '卡密库存' },
    { id: 'items', icon: Box, label: '商品列表' },
    { id: 'orders', icon: ShoppingBag, label: '订单管理' },
    { id: 'rules', icon: Zap, label: '自动化规则' },
    { id: 'notifications', icon: Bell, label: '通知设置' },
    ...(isAdmin ? [{ id: 'settings', icon: Settings, label: '系统与AI' }] : []),
  ];

  return (
    <aside className={`fixed inset-y-0 left-0 z-20 flex flex-col border-r border-slate-200/80 bg-white/95 shadow-[4px_0_24px_rgba(15,23,42,0.035)] backdrop-blur-xl transition-[width] duration-300 ${collapsed ? 'w-16' : 'w-64'}`}>
      <div className={`flex h-20 items-center border-b border-slate-100 ${collapsed ? 'justify-center px-2' : 'gap-3 px-5'}`}>
        <YdisksBrandIcon sizeClass="h-10 w-10" />
        {!collapsed && (
          <div className="min-w-0 leading-tight">
            <div className="truncate text-base font-black tracking-tight text-slate-950">Ydisks 闲鱼助手</div>
            <div className="mt-1 text-[10px] font-extrabold uppercase tracking-[0.22em] text-sky-600">Operations</div>
          </div>
        )}
      </div>

      <nav className={`flex-1 space-y-1.5 overflow-y-auto py-5 ${collapsed ? 'px-2' : 'px-3'}`} aria-label="主导航">
        {menuItems.map((item) => {
          const Icon = item.icon;
          const active = activeTab === item.id;
          return (
            <button
              key={item.id}
              type="button"
              title={collapsed ? item.label : undefined}
              aria-label={item.label}
              aria-current={active ? 'page' : undefined}
              onClick={() => onNavigate(item.id)}
              className={`group relative flex h-11 w-full items-center rounded-xl transition-colors ${collapsed ? 'justify-center' : 'gap-3 px-3.5'} ${
                active
                  ? 'bg-sky-500 text-white shadow-[0_8px_18px_rgba(14,165,233,0.2)]'
                  : 'text-slate-500 hover:bg-slate-100 hover:text-slate-900'
              }`}
            >
              <Icon className={`h-[19px] w-[19px] shrink-0 ${active ? 'text-white' : 'text-slate-400 group-hover:text-slate-700'}`} />
              {!collapsed && <span className="truncate text-sm font-bold">{item.label}</span>}
              {active && !collapsed && <span className="ml-auto h-1.5 w-1.5 rounded-full bg-white/90" />}
            </button>
          );
        })}
      </nav>

      <div className={`space-y-2 border-t border-slate-100 p-2 ${collapsed ? '' : 'p-3'}`}>
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
    </aside>
  );
};

export default Sidebar;
