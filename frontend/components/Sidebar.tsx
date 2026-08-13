import React, { useEffect, useState } from 'react';
import {
  Bell, Box, ChevronLeft, ChevronRight, CreditCard, LayoutDashboard,
  GitCommitHorizontal, LogOut, MessageCircleMore, Settings, ShoppingBag, Users, Zap,
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

interface BuildInfo {
  version: string;
  commit: string;
}

const Sidebar: React.FC<SidebarProps> = ({
  activeTab, isAdmin = false, collapsed, onToggleCollapsed, onNavigate, onLogout,
}) => {
  const [buildInfo, setBuildInfo] = useState<BuildInfo>({ version: 'dev', commit: 'unknown' });

  useEffect(() => {
    const controller = new AbortController();
    fetch('/health', { signal: controller.signal })
      .then(response => response.ok ? response.json() : Promise.reject(new Error('health request failed')))
      .then(data => setBuildInfo({
        version: String(data.version || 'dev'),
        commit: String(data.commit || 'unknown'),
      }))
      .catch(error => {
        if (error instanceof DOMException && error.name === 'AbortError') return;
      });
    return () => controller.abort();
  }, []);

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
        {menuItems.map((item) => {
          const Icon = item.icon;
          const active = activeTab === item.id;
          return (
            <React.Fragment key={item.id}>
              <button
              type="button"
              title={collapsed ? item.label : undefined}
              aria-label={item.label}
              aria-current={active ? 'page' : undefined}
              onClick={() => onNavigate(item.id)}
              className={`group relative flex h-11 w-full items-center rounded-xl transition-colors ${collapsed ? 'justify-center' : 'gap-3 px-3.5'} ${
                active
                  ? 'bg-brand text-white shadow-brand-active'
                  : 'text-slate-500 hover:bg-slate-100 hover:text-slate-900'
              }`}
            >
              <Icon className={`h-[19px] w-[19px] shrink-0 ${active ? 'text-white' : 'text-slate-400 group-hover:text-slate-700'}`} />
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
