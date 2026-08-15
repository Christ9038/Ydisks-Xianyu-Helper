import React, { lazy, Suspense, useState, useEffect } from 'react';
import Sidebar from './components/Sidebar';
import { readSidebarCollapsed, writeSidebarCollapsed } from './components/sidebarState';
import { YdisksBrandIcon } from './components/YdisksLogo';
import { initializeAdmin, login, logout, verifySession } from './app/features/session/api';
import { ShieldCheck, ArrowRight, Loader2, User, Lock } from 'lucide-react';

// Dashboard 是按需加载的仪表盘页面，避免首屏同步载入图表依赖。
const Dashboard = lazy(/* Dashboard 页面按路由激活时加载。 */ () => import('./components/Dashboard'));
// AccountList 是按需加载的账号管理页面，避免未访问时载入账号弹窗和二维码代码。
const AccountList = lazy(/* AccountList 页面按路由激活时加载。 */ () => import('./components/AccountList'));
// OrderList 是按需加载的订单页面，避免首屏载入订单导入与刷新代码。
const OrderList = lazy(/* OrderList 页面按路由激活时加载。 */ () => import('./components/OrderList'));
// CardList 是按需加载的卡密页面，避免首屏载入卡密批量处理代码。
const CardList = lazy(/* CardList 页面按路由激活时加载。 */ () => import('./components/CardList'));
// ItemList 是按需加载的商品页面，避免首屏载入商品发布编辑器代码。
const ItemList = lazy(/* ItemList 页面按路由激活时加载。 */ () => import('./components/ItemList'));
// Settings 是按需加载的系统设置页面，仅在管理员访问时加载。
const Settings = lazy(/* Settings 页面按路由激活时加载。 */ () => import('./components/Settings'));
// Rules 是按需加载的自动化规则页面，避免首屏载入规则编辑器代码。
const Rules = lazy(/* Rules 页面按路由激活时加载。 */ () => import('./components/Rules'));
// Notifications 是按需加载的通知页面，避免首屏载入通知配置代码。
const Notifications = lazy(/* Notifications 页面按路由激活时加载。 */ () => import('./components/Notifications'));
// Chat 是按需加载的聊天页面，避免未访问时载入聊天历史和 WebSocket 视图。
const Chat = lazy(/* Chat 页面按路由激活时加载。 */ () => import('./components/Chat'));

// PageLoading 展示路由页面代码加载期间的统一占位状态。
const PageLoading: React.FC = () => (
  <div className="flex min-h-[24rem] items-center justify-center" role="status" aria-label="正在加载页面">
    <Loader2 className="h-8 w-8 animate-spin text-brand" />
  </div>
);

interface DeliveryRuleTarget {
// cookieId 表示cookieId。
    cookieId: string;
// itemId 表示当前商品Id。
    itemId: string;
// requestId 表示接口请求对象Id。
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
  Object.entries(ROUTES).map(([path, tab]) => [tab, path] /* 回调函数负责当前业务流程。 */)
); /* TAB_TO_PATH 表示TABTO当前路径。 */
const tabFromPath = (): string => ROUTES[window.location.pathname] || 'dashboard'; /* tabFromPath 表示tabFrom当前路径。 */

const App: React.FC = () => {
  const [isLoggedIn, setIsLoggedIn] = useState(false); /* [isLoggedIn, setIsLoggedIn] 表示isLoggedInsetIsLoggedIn。 */
  const [isAdmin, setIsAdmin] = useState(false); /* [isAdmin, setIsAdmin] 表示isAdminsetIsAdmin。 */
  const [activeTab, setActiveTab] = useState(tabFromPath); /* [activeTab, setActiveTab] 表示activeTabsetActiveTab。 */
  const [checkingAuth, setCheckingAuth] = useState(true); /* [checkingAuth, setCheckingAuth] 表示checkingAuthsetCheckingAuth。 */
  const [needsInit, setNeedsInit] = useState(false); /* [needsInit, setNeedsInit] 表示needsInitsetNeedsInit。 */
  const [username, setUsername] = useState(''); /* [username, setUsername] 表示usernamesetUsername。 */
  const [password, setPassword] = useState(''); /* [password, setPassword] 表示passwordsetPassword。 */
  const [loginLoading, setLoginLoading] = useState(false); /* [loginLoading, setLoginLoading] 表示login加载状态setLogin加载状态。 */
  const [loginError, setLoginError] = useState(''); /* [loginError, setLoginError] 表示login当前操作返回的错误setLogin当前操作返回的错误。 */
  const [initialPassword, setInitialPassword] = useState(''); /* [initialPassword, setInitialPassword] 表示initialPasswordsetInitialPassword。 */
  const [initialPasswordConfirm, setInitialPasswordConfirm] = useState(''); /* [initialPasswordConfirm, setInitialPasswordConfirm] 表示initialPasswordConfirmsetInitialPasswordConfirm。 */
  const [initializing, setInitializing] = useState(false); /* [initializing, setInitializing] 表示initializingsetInitializing。 */
  const [initializationError, setInitializationError] = useState(''); /* [initializationError, setInitializationError] 表示initialization当前操作返回的错误setInitialization当前操作返回的错误。 */
  const [deliveryRuleTarget, setDeliveryRuleTarget] = useState<DeliveryRuleTarget | undefined>(); /* [deliveryRuleTarget, setDeliveryRuleTarget] 表示delivery当前规则TargetsetDelivery当前规则Target。 */
	const [sidebarCollapsed, setSidebarCollapsed] = useState(readSidebarCollapsed); /* [sidebarCollapsed, setSidebarCollapsed] 表示sidebarCollapsedsetSidebarCollapsed。 */

  // 切换 tab 并同步 URL。若 tab 没有对应 path（不应发生）则只切 tab。
  const navigate = (tab: string) => {
    const nextTab = tab === 'settings' && !isAdmin ? 'dashboard' : tab; /* nextTab 表示nextTab。 */
    const path = TAB_TO_PATH[nextTab]; /* path 表示当前路径。 */
    if (path && path !== window.location.pathname) {
      window.history.pushState({}, '', path);
    }
    setActiveTab(nextTab);
  };

  // 浏览器后退/前进同步 tab。
  useEffect(() => {
    const onPopState = () => setActiveTab(tabFromPath()); /* onPopState 表示onPopState。 */
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState) /* 回调函数负责当前业务流程。 */;
  } /* 回调函数负责当前业务流程。 */, []);

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
        } /* 回调函数负责当前业务流程。 */)
        .catch(() => {
          setIsLoggedIn(false);
          setIsAdmin(false);
        } /* 回调函数负责当前业务流程。 */)
        .finally(() => setCheckingAuth(false) /* 回调函数负责当前业务流程。 */);

      const handleAuthLogoutEvent = () => {
        setIsLoggedIn(false);
        setIsAdmin(false);
      }; /* handleAuthLogoutEvent 表示handleAuthLogoutEvent。 */
      window.addEventListener('auth:logout', handleAuthLogoutEvent);
      return () => window.removeEventListener('auth:logout', handleAuthLogoutEvent) /* 回调函数负责当前业务流程。 */;
  } /* 回调函数负责当前业务流程。 */, []);

  useEffect(() => {
    if (!checkingAuth && isLoggedIn && !isAdmin && activeTab === 'settings') {
      window.history.replaceState({}, '', TAB_TO_PATH.dashboard);
      setActiveTab('dashboard');
    }
  } /* 回调函数负责当前业务流程。 */, [checkingAuth, isLoggedIn, isAdmin, activeTab]);

  const handleLogin = async (e: React.FormEvent) => {
      e.preventDefault();
      setLoginLoading(true);
      setLoginError('');
      
      try {
          const res = await login({ username, password }); /* res 表示接口响应结果。 */
          if (res.success) {
              setIsLoggedIn(true);
              setIsAdmin(res.is_admin === true);
          } else {
              setLoginError(res.message || '登录失败');
          }
      } catch (err /* err 表示当前操作返回的错误。 */) {
          const msg = err instanceof Error ? err.message : String(err); /* msg 表示msg。 */
          setLoginError(msg || '登录失败');
      } finally {
          setLoginLoading(false);
      }
  }; /* handleLogin 表示handleLogin。 */

  const handleInitialize = async (e: React.FormEvent) => {
    e.preventDefault();
    setInitializationError('');
    if (initialPassword.length < 8) {
      setInitializationError('密码至少需要 8 个字符');
      return;
    }
    if (initialPassword !== initialPasswordConfirm) {
      setInitializationError('两次输入的密码不一致');
      return;
    }

    setInitializing(true);
    try {
      const res = await initializeAdmin(initialPassword); /* res 表示接口响应结果。 */
      if (!res.success) {
        setInitializationError(res.message || '初始化失败，请重试');
        return;
      }
      setNeedsInit(false);
      setIsLoggedIn(true);
      setIsAdmin(res.is_admin === true);
      setInitialPassword('');
      setInitialPasswordConfirm('');
    } catch (err /* err 表示当前操作返回的错误。 */) {
      const msg = err instanceof Error ? err.message : String(err); /* msg 表示msg。 */
      setInitializationError(msg || '初始化失败，请重试');
    } finally {
      setInitializing(false);
    }
  }; /* handleInitialize 表示handleInitialize。 */

  const handleLogout = async () => {
      try {
          await logout();
      } catch (err /* err 表示当前操作返回的错误。 */) {
          console.error('退出登录失败', err);
      } finally {
          setIsLoggedIn(false);
          setIsAdmin(false);
      }
  }; /* handleLogout 表示handleLogout。 */


  if (checkingAuth) {
      return (
          <div className="min-h-screen flex items-center justify-center bg-surface">
              <Loader2 className="w-8 h-8 text-brand animate-spin" />
          </div>
      );
  }

  // Init Screen (system not initialized)
  if (needsInit) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-canvas p-4 relative overflow-hidden font-sans">
        <div className="absolute top-[-10%] left-[-10%] w-[60%] h-[60%] bg-blue-200/40 rounded-full blur-[120px] animate-pulse"></div>
        <div className="absolute bottom-[-10%] right-[-10%] w-[60%] h-[60%] bg-blue-200/30 rounded-full blur-[120px] animate-pulse" style={{animationDelay: '2s'}}></div>

        <div className="bg-white/80 backdrop-blur-3xl p-8 md:p-12 rounded-xl shadow-panel w-full max-w-xl border border-white relative z-10 animate-fade-in">
          <div className="text-center mb-8">
            <div className="mx-auto mb-6 flex justify-center">
              <YdisksBrandIcon sizeClass="w-24 h-24" />
            </div>
            <h2 className="text-3xl font-extrabold text-gray-900 mb-2 tracking-tight">首次设置管理员密码</h2>
            <p className="text-gray-600 font-medium">设置完成后会自动进入系统，管理员账号为 admin。</p>
          </div>

          <form onSubmit={handleInitialize} className="space-y-5">
            <div className="space-y-4">
              <div className="relative group">
                <Lock className="absolute left-5 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400 group-focus-within:text-black transition-colors" />
                <input
                  type="password"
                  placeholder="设置管理员密码（至少 8 个字符）"
                  value={initialPassword}
                  onChange={e => setInitialPassword(e.target.value) /* 回调函数负责当前业务流程。 */}
                  autoFocus
                  className="w-full ios-input pl-14 pr-6 py-4.5 rounded-2xl text-base h-14"
                />
              </div>
              <div className="relative group">
                <ShieldCheck className="absolute left-5 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400 group-focus-within:text-black transition-colors" />
                <input
                  type="password"
                  placeholder="再次输入密码"
                  value={initialPasswordConfirm}
                  onChange={e => setInitialPasswordConfirm(e.target.value) /* 回调函数负责当前业务流程。 */}
                  className="w-full ios-input pl-14 pr-6 py-4.5 rounded-2xl text-base h-14"
                />
              </div>
            </div>

            {initializationError && (
              <div className="p-3 rounded-xl bg-red-50 text-red-500 text-sm text-center font-bold flex items-center justify-center gap-2">
                <ShieldCheck className="w-4 h-4" /> {initializationError}
              </div>
            )}

            <button
              type="submit"
              disabled={initializing}
              className="w-full ios-btn-primary h-14 rounded-2xl text-lg shadow-xl shadow-blue-200 mt-2 flex items-center justify-center gap-2 group disabled:opacity-70"
            >
              {initializing ? <Loader2 className="w-5 h-5 animate-spin" /> : <>设置密码并进入系统 <ArrowRight className="w-5 h-5 group-hover:translate-x-1 transition-transform" /></>}
            </button>
          </form>

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
      <div className="min-h-screen flex items-center justify-center bg-canvas p-4 relative overflow-hidden font-sans">
        {/* Animated Background Blobs */}
        <div className="absolute top-[-10%] left-[-10%] w-[60%] h-[60%] bg-blue-200/40 rounded-full blur-[120px] animate-pulse"></div>
        <div className="absolute bottom-[-10%] right-[-10%] w-[60%] h-[60%] bg-blue-200/30 rounded-full blur-[120px] animate-pulse" style={{animationDelay: '2s'}}></div>

        <div className="bg-white/80 backdrop-blur-3xl p-8 md:p-12 rounded-xl shadow-panel w-full max-w-lg border border-white relative z-10 animate-fade-in">
          
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
                        onChange={e => setUsername(e.target.value) /* 回调函数负责当前业务流程。 */}
                        className="w-full ios-input pl-14 pr-6 py-4.5 rounded-2xl text-base h-14"
                    />
                </div>
                <div className="relative group">
                    <Lock className="absolute left-5 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400 group-focus-within:text-black transition-colors" />
                    <input 
                        type="password" 
                        placeholder="密码" 
                        value={password}
                        onChange={e => setPassword(e.target.value) /* 回调函数负责当前业务流程。 */}
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
      } /* 回调函数负责当前业务流程。 */} />;
      case 'rules': return <Rules
        initialDeliveryTarget={deliveryRuleTarget}
        onDeliveryTargetHandled={() => setDeliveryRuleTarget(undefined) /* 回调函数负责当前业务流程。 */}
      />;
      case 'notifications': return <Notifications isAdmin={isAdmin} />;
      case 'settings': return isAdmin ? <Settings /> : <Dashboard />;
      default: return <Dashboard />;
    }
  }; /* renderContent 表示renderContent。 */

  return (
    <div className="flex min-h-screen bg-canvas text-ink">
      <Sidebar
        activeTab={activeTab}
        isAdmin={isAdmin}
		collapsed={sidebarCollapsed}
		onToggleCollapsed={() => setSidebarCollapsed(current => {
		  const next = !current; /* next 表示next。 */
		  writeSidebarCollapsed(next);
		  return next;
		} /* 回调函数负责当前业务流程。 */) /* 回调函数负责当前业务流程。 */}
        onNavigate={navigate}
        onLogout={handleLogout}
      />
      
      <main className={`h-screen min-w-0 flex-1 overflow-x-hidden overflow-y-auto scroll-smooth transition-[margin] duration-300 ${sidebarCollapsed ? 'ml-16' : 'ml-64'} ${activeTab === 'chat' ? 'p-4 md:p-6' : 'p-8 md:p-12'}`}>
        {/* Subtle background decoration */}
        <div className="fixed top-0 right-0 w-[800px] h-[800px] bg-gradient-to-bl from-blue-50 to-transparent rounded-full blur-[120px] pointer-events-none -z-10 opacity-60"></div>
        
			<div className={`${activeTab === 'chat' ? 'mx-auto max-w-[1680px]' : 'mx-auto max-w-[1400px] pb-10'}`}>
            <Suspense fallback={<PageLoading />}>
              {renderContent()}
            </Suspense>
        </div>
      </main>
    </div>
  );
}; /* App 表示App。 */

export default App;
