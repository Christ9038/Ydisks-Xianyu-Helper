import React from 'react';
import { LayoutDashboard, Users, ShoppingBag, CreditCard, Settings, LogOut, Box, Sparkles, Zap, Bell } from 'lucide-react';
import { YdisksBrandIcon } from './YdisksLogo';

interface SidebarProps {
  activeTab: string;
  isAdmin?: boolean;
  onNavigate: (tab: string) => void;
  onLogout: () => void;
}

const Sidebar: React.FC<SidebarProps> = ({ activeTab, isAdmin = false, onNavigate, onLogout }) => {
  const menuItems = [
    { id: 'dashboard', icon: LayoutDashboard, label: '仪表盘' },
    { id: 'accounts', icon: Users, label: '账号管理' },
    { id: 'orders', icon: ShoppingBag, label: '订单管理' },
    { id: 'cards', icon: CreditCard, label: '卡密库存' },
    { id: 'items', icon: Box, label: '商品列表' },
    { id: 'rules', icon: Zap, label: '自动化规则' },
    { id: 'notifications', icon: Bell, label: '通知设置' },
    ...(isAdmin ? [{ id: 'settings', icon: Settings, label: '系统与AI' }] : []),
  ];

  return (
    <div className="w-64 h-screen fixed left-0 top-0 bg-white border-r border-gray-100 flex flex-col justify-between z-20 shadow-[4px_0_24px_rgba(0,0,0,0.02)]">
      <div className="p-6">
        <div className="flex items-center gap-3 mb-12 px-2">
          <div className="shrink-0">
            <YdisksBrandIcon sizeClass="w-10 h-10" />
          </div>
          <h1 className="text-xl font-extrabold tracking-tight text-gray-900">Ydisks闲鱼助手 <span className="text-xs bg-[#0094f7] text-white px-1.5 py-0.5 rounded ml-1">PRO</span></h1>
        </div>

        <nav className="space-y-2">
          {menuItems.map((item) => {
            const Icon = item.icon;
            const isActive = activeTab === item.id;
            return (
              <button
                key={item.id}
                onClick={() => onNavigate(item.id)}
                className={`w-full flex items-center gap-3 px-4 py-3.5 rounded-2xl transition-all duration-300 group relative overflow-hidden ${
                  isActive 
                    ? 'bg-[#0094f7] text-white font-bold shadow-lg shadow-blue-100 transform scale-[1.02]' 
                    : 'text-gray-500 hover:bg-gray-50 hover:text-gray-900'
                }`}
              >
                <Icon className={`w-5 h-5 transition-colors ${isActive ? 'text-white' : 'text-gray-400 group-hover:text-gray-600'}`} />
                <span className="text-sm tracking-wide">{item.label}</span>
                {isActive && <Sparkles className="w-4 h-4 absolute right-3 text-white/30 animate-pulse" />}
              </button>
            );
          })}
        </nav>
      </div>

      <div className="p-6 border-t border-gray-50">
        <button 
          onClick={onLogout}
          className="w-full flex items-center gap-3 px-4 py-3 text-gray-500 hover:text-red-500 hover:bg-red-50 rounded-2xl transition-all duration-200 font-medium"
        >
          <LogOut className="w-5 h-5" />
          <span className="text-sm">退出登录</span>
        </button>
      </div>
    </div>
  );
};

export default Sidebar;
