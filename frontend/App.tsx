import React, { useState, useEffect } from 'react';
import { readSidebarCollapsed, writeSidebarCollapsed } from './components/sidebarState';
import { YdisksBrandIcon } from './components/YdisksLogo';
import { ShieldCheck, ArrowRight, Loader2, User, Lock } from 'lucide-react';
import AuthenticatedShell, { type DeliveryRuleTarget } from './app/shell/AuthenticatedShell';
import { SessionProvider, useSession } from './app/providers/SessionProvider';

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

// AppView 渲染认证表单、路由状态和认证成功后的应用壳。
const AppView: React.FC = () => {
  // session 从 Provider 读取认证状态和会话操作，避免页面直接管理全局会话副作用。
  const { checkingAuth, isLoggedIn, isAdmin, needsInit, signIn, initialize, signOut } = useSession();
  const [activeTab, setActiveTab] = useState(tabFromPath); /* [activeTab, setActiveTab] 表示activeTabsetActiveTab。 */
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
          const res = await signIn({ username, password }); /* res 表示接口响应结果。 */
          if (!res.success) {
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
      const res = await initialize(initialPassword); /* res 表示接口响应结果。 */
      if (!res.success) {
        setInitializationError(res.message || '初始化失败，请重试');
        return;
      }
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
          await signOut();
      } catch (err /* err 表示当前操作返回的错误。 */) {
          console.error('退出登录失败', err);
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

  // handleConfigureDelivery 保存商品页传来的规则目标并切换到规则页面。
  const handleConfigureDelivery = (target /* target 表示商品页传来的规则配置目标。 */: DeliveryRuleTarget) => {
    setDeliveryRuleTarget(target);
    navigate('rules');
  };

  // handleDeliveryTargetHandled 清理已经被规则页面消费的联动目标。
  const handleDeliveryTargetHandled = () => setDeliveryRuleTarget(undefined);

  // handleToggleSidebar 切换侧边栏状态并持久化用户偏好。
  const handleToggleSidebar = () => setSidebarCollapsed(/* current 表示更新前的侧边栏折叠状态。 */ current => {
    // next 表示切换后的侧边栏折叠状态。
    const next = !current;
    writeSidebarCollapsed(next);
    return next;
  });

  return (
    <AuthenticatedShell
      activeTab={activeTab}
      isAdmin={isAdmin}
      collapsed={sidebarCollapsed}
      deliveryRuleTarget={deliveryRuleTarget}
      onToggleCollapsed={handleToggleSidebar}
      onNavigate={navigate}
      onLogout={handleLogout}
      onConfigureDelivery={handleConfigureDelivery}
      onDeliveryTargetHandled={handleDeliveryTargetHandled}
    />
  );
}; /* AppView 表示认证页面和应用壳视图。 */

// App 在根部装配 SessionProvider，确保所有认证状态共享同一生命周期。
const App: React.FC = () => (
  <SessionProvider>
    <AppView />
  </SessionProvider>
);

export default App;
