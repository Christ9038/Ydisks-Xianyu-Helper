import React, { useState, useEffect } from 'react';
import Sidebar from './components/Sidebar';
import Dashboard from './components/Dashboard';
import AccountList from './components/AccountList';
import OrderList from './components/OrderList';
import CardList from './components/CardList';
import ItemList from './components/ItemList';
import Settings from './components/Settings';
import Rules from './components/Rules';
import Notifications from './components/Notifications';
import Chat from './components/Chat';
import { readSidebarCollapsed, writeSidebarCollapsed } from './components/sidebarState';
import { YdisksBrandIcon } from './components/YdisksLogo';
import { login, logout, verifySession } from './services/api';
import { ShieldCheck, ArrowRight, Loader2, User, Lock } from 'lucide-react';

interface DeliveryRuleTarget {
  cookieId: string;
  itemId: string;
  requestId: number;
}

// 路由：URL path ↔ tab id。所有 SPA 路由统一挂 /app/ 前缀，避免和后端 API
// 路径（/orders、/cards、/items 等）冲突——后者在 chi 里先注册，刷新会直接
// 返回 JSON 而不是 SPA 页面。
const ROUTES: Record<string, string> = {
  '/app/dashboard': 'dashboard',
  '/app/accounts': 'accounts',
	'/app/chat': 'chat',
  '/app/orders': 'orders',
  '/app/cards': 'cards',
  '/app/items': 'items',
  '/app/rules': 'rules',
  '/app/notifications': 'notifications',
  '/app/settings': 'settings',
};
const TAB_TO_PATH: Record<string, string> = Object.fromEntries(
  Object.entries(ROUTES).map(([path, tab]) => [tab, path])
);
const tabFromPath = (): string => ROUTES[window.location.pathname] || 'dashboard';

// 检测 Windows 以显示对应的 CLI 命令（路径分隔符、引号、.exe 后缀）。
const isWindows = /Win/i.test(navigator.platform || navigator.userAgent);

const App: React.FC = () => {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);
  const [activeTab, setActiveTab] = useState(tabFromPath);
  const [checkingAuth, setCheckingAuth] = useState(true);
  const [needsInit, setNeedsInit] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [loginLoading, setLoginLoading] = useState(false);
  const [loginError, setLoginError] = useState('');
  const [deliveryRuleTarget, setDeliveryRuleTarget] = useState<DeliveryRuleTarget | undefined>();
	const [sidebarCollapsed, setSidebarCollapsed] = useState(readSidebarCollapsed);

  // 切换 tab 并同步 URL。若 tab 没有对应 path（不应发生）则只切 tab。
  const navigate = (tab: string) => {
    const nextTab = tab === 'settings' && !isAdmin ? 'dashboard' : tab;
    const path = TAB_TO_PATH[nextTab];
    if (path && path !== window.location.pathname) {
      window.history.pushState({}, '', path);
    }
    setActiveTab(nextTab);
  };

  // 浏览器后退/前进同步 tab。
  useEffect(() => {
    const onPopState = () => setActiveTab(tabFromPath());
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  // Check auth on mount
  useEffect(() => {
      verifySession()
        .then((res) => {
          if (res?.initialized === false) {
            setNeedsInit(true);
            setIsLoggedIn(false);
            setIsAdmin(false);
            return;
          }

          setNeedsInit(false);
          if (res?.authenticated) {
            setIsLoggedIn(true);
            setIsAdmin(res.is_admin === true);
          } else {
            setIsAdmin(false);
          }
        })
        .catch(() => {
          setIsLoggedIn(false);
          setIsAdmin(false);
        })
        .finally(() => setCheckingAuth(false));

      const handleAuthLogoutEvent = () => {
        setIsLoggedIn(false);
        setIsAdmin(false);
      };
      window.addEventListener('auth:logout', handleAuthLogoutEvent);
      return () => window.removeEventListener('auth:logout', handleAuthLogoutEvent);
  }, []);

  useEffect(() => {
    if (!checkingAuth && isLoggedIn && !isAdmin && activeTab === 'settings') {
      window.history.replaceState({}, '', TAB_TO_PATH.dashboard);
      setActiveTab('dashboard');
    }
  }, [checkingAuth, isLoggedIn, isAdmin, activeTab]);

  const handleLogin = async (e: React.FormEvent) => {
      e.preventDefault();
      setLoginLoading(true);
      setLoginError('');
      
      try {
          const res = await login({ username, password });
          if (res.success) {
              setIsLoggedIn(true);
              setIsAdmin(res.is_admin === true);
          } else {
              setLoginError(res.message || '登录失败');
          }
      } catch (err) {
          const msg = err instanceof Error ? err.message : String(err);
          setLoginError(msg || '登录失败');
      } finally {
          setLoginLoading(false);
      }
  };

  const handleLogout = async () => {
      try {
          await logout();
      } catch (err) {
          console.error('退出登录失败', err);
      } finally {
          setIsLoggedIn(false);
          setIsAdmin(false);
      }
  };


  if (checkingAuth) {
      return (
          <div className="min-h-screen flex items-center justify-center bg-[#f5f5f7]">
              <Loader2 className="w-8 h-8 text-[#0094f7] animate-spin" />
          </div>
      );
  }

  // Init Screen (system not initialized)
  if (needsInit) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-[#F4F5F7] p-4 relative overflow-hidden font-sans">
        <div className="absolute top-[-10%] left-[-10%] w-[60%] h-[60%] bg-blue-200/40 rounded-full blur-[120px] animate-pulse"></div>
        <div className="absolute bottom-[-10%] right-[-10%] w-[60%] h-[60%] bg-blue-200/30 rounded-full blur-[120px] animate-pulse" style={{animationDelay: '2s'}}></div>

        <div className="bg-white/80 backdrop-blur-3xl p-8 md:p-12 rounded-xl shadow-[0_20px_60px_-15px_rgba(0,0,0,0.05)] w-full max-w-xl border border-white relative z-10 animate-fade-in">
          <div className="text-center mb-8">
            <div className="mx-auto mb-6 flex justify-center">
              <YdisksBrandIcon sizeClass="w-24 h-24" />
            </div>
            <h2 className="text-3xl font-extrabold text-gray-900 mb-2 tracking-tight">系统尚未初始化</h2>
            <p className="text-gray-600 font-medium">为避免默认口令风险，管理员必须通过服务器本机 CLI 初始化。</p>
          </div>

          <div className="space-y-4">
            <div className="p-4 rounded-2xl bg-gray-50 border border-gray-100">
              <div className="text-sm font-bold text-gray-900 mb-2">请在服务器上执行：</div>
              <pre className="text-xs bg-black text-white p-4 rounded-2xl overflow-x-auto">{isWindows
                ? '.\\xianyu-server.exe -init-admin -db data\\xianyu_data.db -admin-password "请设置密码"'
                : './xianyu-server -init-admin -db data/xianyu_data.db -admin-password \'请设置密码\''}</pre>
              <div className="text-xs text-gray-500 mt-2">完成后刷新页面即可进入登录。</div>
            </div>

            <button
              type="button"
              onClick={() => window.location.reload()}
              className="w-full ios-btn-primary h-14 rounded-2xl text-lg shadow-xl shadow-blue-200 mt-2 flex items-center justify-center gap-2 group"
            >
              我已初始化，刷新 <ArrowRight className="w-5 h-5 group-hover:translate-x-1 transition-transform" />
            </button>
          </div>

          <div className="mt-8 pt-6 border-t border-gray-100 text-center">
            <span className="text-xs text-gray-400 font-medium tracking-widest uppercase">Secure Bootstrap</span>
          </div>
        </div>
      </div>
    );
  }

  // Login Screen Component
  if (!isLoggedIn) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-[#F4F5F7] p-4 relative overflow-hidden font-sans">
        {/* Animated Background Blobs */}
        <div className="absolute top-[-10%] left-[-10%] w-[60%] h-[60%] bg-blue-200/40 rounded-full blur-[120px] animate-pulse"></div>
        <div className="absolute bottom-[-10%] right-[-10%] w-[60%] h-[60%] bg-blue-200/30 rounded-full blur-[120px] animate-pulse" style={{animationDelay: '2s'}}></div>

        <div className="bg-white/80 backdrop-blur-3xl p-8 md:p-12 rounded-xl shadow-[0_20px_60px_-15px_rgba(0,0,0,0.05)] w-full max-w-lg border border-white relative z-10 animate-fade-in">
          
          {/* Header with Logo */}
          <div className="text-center mb-10">
             <div className="group mx-auto mb-6 flex cursor-pointer justify-center">
                <YdisksBrandIcon sizeClass="w-24 h-24" logoClassName="w-full h-full text-white group-hover:scale-110 transition-transform" />
             </div>
             <h2 className="text-3xl font-extrabold text-gray-900 mb-2 tracking-tight">欢迎回来</h2>
             <p className="text-gray-500 font-medium">Ydisks闲鱼助手 · 自动发货与管家系统</p>
          </div>
          
          <form onSubmit={handleLogin} className="space-y-5">
            <div className="space-y-4">
                <div className="relative group">
                    <User className="absolute left-5 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400 group-focus-within:text-black transition-colors" />
                    <input 
                        type="text" 
                        placeholder="管理员账号" 
                        value={username}
                        onChange={e => setUsername(e.target.value)}
                        className="w-full ios-input pl-14 pr-6 py-4.5 rounded-2xl text-base h-14"
                    />
                </div>
                <div className="relative group">
                    <Lock className="absolute left-5 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400 group-focus-within:text-black transition-colors" />
                    <input 
                        type="password" 
                        placeholder="密码" 
                        value={password}
                        onChange={e => setPassword(e.target.value)}
                        className="w-full ios-input pl-14 pr-6 py-4.5 rounded-2xl text-base h-14"
                    />
                </div>
            </div>
            
            {loginError && (
                <div className="p-3 rounded-xl bg-red-50 text-red-500 text-sm text-center font-bold flex items-center justify-center gap-2">
                    <ShieldCheck className="w-4 h-4" /> {loginError}
                </div>
            )}

            <button 
              type="submit" 
              disabled={loginLoading}
              className="w-full ios-btn-primary h-14 rounded-2xl text-lg shadow-xl shadow-blue-200 mt-2 flex items-center justify-center gap-2 group disabled:opacity-70"
            >
              {loginLoading ? <Loader2 className="w-5 h-5 animate-spin" /> : <>立即登录 <ArrowRight className="w-5 h-5 group-hover:translate-x-1 transition-transform" /></>}
            </button>
          </form>
          
          <div className="mt-8 pt-6 border-t border-gray-100">
             <div className="mt-6 text-center">
                 <span className="text-xs text-gray-400 font-medium tracking-widest uppercase">
                    Ydisks闲鱼助手 v1.0
                 </span>
             </div>
          </div>
        </div>
      </div>
    );
  }

  // Main App Layout
  const renderContent = () => {
    switch (activeTab) {
      case 'dashboard': return <Dashboard />;
      case 'accounts': return <AccountList />;
	  case 'chat': return <Chat />;
      case 'orders': return <OrderList />;
      case 'cards': return <CardList />;
      case 'items': return <ItemList onConfigureDelivery={(item) => {
        setDeliveryRuleTarget({ cookieId: item.cookie_id, itemId: item.item_id, requestId: Date.now() });
        navigate('rules');
      }} />;
      case 'rules': return <Rules
        initialDeliveryTarget={deliveryRuleTarget}
        onDeliveryTargetHandled={() => setDeliveryRuleTarget(undefined)}
      />;
      case 'notifications': return <Notifications isAdmin={isAdmin} />;
      case 'settings': return isAdmin ? <Settings /> : <Dashboard />;
      default: return <Dashboard />;
    }
  };

  return (
    <div className="flex min-h-screen bg-[#F4F5F7] text-[#111]">
      <Sidebar
        activeTab={activeTab}
        isAdmin={isAdmin}
		collapsed={sidebarCollapsed}
		onToggleCollapsed={() => setSidebarCollapsed(current => {
		  const next = !current;
		  writeSidebarCollapsed(next);
		  return next;
		})}
        onNavigate={navigate}
        onLogout={handleLogout}
      />
      
      <main className={`h-screen min-w-0 flex-1 overflow-x-hidden overflow-y-auto scroll-smooth transition-[margin] duration-300 ${sidebarCollapsed ? 'ml-16' : 'ml-64'} ${activeTab === 'chat' ? 'p-4 md:p-6' : 'p-8 md:p-12'}`}>
        {/* Subtle background decoration */}
        <div className="fixed top-0 right-0 w-[800px] h-[800px] bg-gradient-to-bl from-blue-50 to-transparent rounded-full blur-[120px] pointer-events-none -z-10 opacity-60"></div>
        
		<div className={`${activeTab === 'chat' ? 'mx-auto max-w-[1680px]' : 'mx-auto max-w-[1400px] pb-10'}`}>
            {renderContent()}
        </div>
      </main>
    </div>
  );
};

export default App;
