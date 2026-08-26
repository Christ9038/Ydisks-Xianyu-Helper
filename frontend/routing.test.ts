import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

const repoRoot = resolve(__dirname); /* repoRoot 表示repoRoot。 */

const readFrontendFile = (relativePath: string) =>
  readFileSync(resolve(repoRoot, relativePath), 'utf8'); /* readFrontendFile 表示readFrontendFile。 */

const extractSingleQuotedValues = (source: string, pattern: RegExp) => {
  const values = new Set<string>(); /* values 表示值集合。 */
  for (const match /* match 表示匹配结果。 */ of source.matchAll(pattern)) {
    values.add(match[1]);
  }
  return values;
}; /* extractSingleQuotedValues 表示extractSingleQuoted值集合。 */

describe('frontend navigation routing', () => {
  test('sidebar entries and App activeTab routes stay in sync', () => {
    const sidebar = readFrontendFile('shared/ui/Sidebar.tsx'); /* sidebar 表示sidebar。 */
    const app = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* app 表示认证后页面组合器源码。 */

    const sidebarIDs = extractSingleQuotedValues(sidebar, /id:\s*'([^']+)'/g); /* sidebarIDs 表示sidebarIDs。 */
    const appRouteIDs = extractSingleQuotedValues(app, /case\s+'([^']+)'/g); /* appRouteIDs 表示appRouteIDs。 */

    expect([...sidebarIDs].sort()).toEqual([...appRouteIDs].sort());
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('active navigation uses the primary action blue', () => {
    const sidebar = readFrontendFile('shared/ui/Sidebar.tsx'); /* sidebar 表示sidebar。 */

    expect(sidebar).toContain("'bg-brand text-white shadow-brand-active'");
    expect(sidebar).not.toContain("'bg-sky-500 text-white'");
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('route pages are lazy-loaded behind a shared Suspense boundary', () => {
    const app = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* app 表示认证后页面组合器源码。 */
    const lazyPageCount = (app.match(/const (Dashboard|AccountList|OrderList|CardList|ItemList|Settings|Rules|Notifications|Chat) = lazy\(/g) || []).length; /* lazyPageCount 表示按路由懒加载的页面数量。 */

    expect(lazyPageCount).toBe(9);
    expect(app).toContain('const PageLoading: React.FC');
    expect(app).toContain('<Suspense fallback={<PageLoading />}>');
    expect(app).not.toContain("import Dashboard from '../../components/Dashboard'");
    expect(app).toContain("import('../features/dashboard/pages/Dashboard')");
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('authenticated shell owns sidebar and page composition', () => {
    const app = readFrontendFile('app/router/AppRouter.tsx'); /* app 表示认证后的路由组合器源码。 */
    const shell = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* shell 表示认证后应用壳源码。 */

    expect(app).toContain("import AuthenticatedShell, { type DeliveryRuleTarget }");
    expect(app).not.toContain('const renderContent = () =>');
    expect(shell).toContain('const AuthenticatedShell: React.FC<AuthenticatedShellProps>');
    expect(shell).toContain('<Sidebar');
    expect(shell).toContain('<AppContent');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('logout button invalidates the backend session before clearing UI state', () => {
    const app = readFrontendFile('app/router/AppRouter.tsx'); /* app 表示认证后路由组合器源码。 */
    const sessionProvider = readFrontendFile('app/providers/SessionProvider.tsx'); /* sessionProvider 表示认证 Provider 源码。 */

    expect(app).toContain("import { useSession }");
    expect(sessionProvider).toContain('await logout();');
    expect(sessionProvider).toContain("window.addEventListener('auth:logout'");
    expect(app).toContain('onLogout={handleLogout}');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('settings page does not expose backend-inactive system controls', () => {
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示settings。 */
    const settingsConstants = readFrontendFile('app/features/settings/constants.ts'); /* settingsConstants 表示settingsConstants。 */

    expect(settings).not.toContain('允许用户注册');
    expect(settings).not.toContain('显示默认登录信息');
    expect(settings).not.toContain('启用商品自动同步');
    expect(settings).not.toContain('商品同步间隔');
    expect(settings).not.toContain('默认自动回复内容');
    expect(settingsConstants).toContain('SETTINGS_SAVE_OMIT_KEYS');
    expect(settings).toContain('保存后需重启服务生效');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('chat page exposes the browser notification switch', () => {
    const chat = readFrontendFile('app/features/chat/components/BrowserNotificationToggle.tsx'); /* chat 表示聊天页通知入口源码，用于确认普通用户可见。 */
    const chatPage = readFrontendFile('app/features/chat/pages/Chat.tsx'); /* chatPage 表示聊天页组合源码，用于确认开关实际挂载。 */
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示设置页源码，用于确认入口已迁移。 */

    expect(chat).toContain('新消息系统通知');
    expect(chat).toContain('role="switch"');
    expect(chat).toContain('useBrowserNotificationPreference');
    expect(chatPage).toContain("import BrowserNotificationToggle from '../components/BrowserNotificationToggle'");
    expect(chatPage).toContain('<BrowserNotificationToggle />');
    expect(settings).not.toContain('新消息系统通知');
  } /* 测试回调断言聊天页包含浏览器通知开关且管理员设置页不再重复展示。 */);

  test('admin-only settings navigation is gated by session role', () => {
    const app = readFrontendFile('app/router/AppRouter.tsx'); /* app 表示认证后路由组合器源码。 */
    const sessionProvider = readFrontendFile('app/providers/SessionProvider.tsx'); /* sessionProvider 表示认证 Provider 源码。 */
    const shell = readFrontendFile('app/shell/AuthenticatedShell.tsx'); /* shell 表示认证后应用壳源码。 */
    const sidebar = readFrontendFile('shared/ui/Sidebar.tsx'); /* sidebar 表示sidebar。 */
    const settingsHook = readFrontendFile('app/features/settings/hooks.ts'); /* settingsHook 表示settingsHook。 */

    expect(app).toContain('const { isLoggedIn, isAdmin, signOut } = useSession();');
    expect(sessionProvider).toContain('setIsAdmin(response.is_admin === true)');
    expect(app).toContain("activeRoute === 'settings'");
    expect(shell).toContain('isAdmin ? <Settings /> : <Dashboard />');
    expect(sidebar).toContain('isAdmin = false');
    expect(sidebar).toContain("...(isAdmin ? [{ id: 'settings'");
    expect(settingsHook).toContain('setLoadError');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('captcha remote settings expose the reference privacy and fallback semantics', () => {
    const settings = readFrontendFile('app/features/settings/pages/Settings.tsx'); /* settings 表示settings。 */

    expect(settings).toContain('远程过滑块配置');
    expect(settings).toContain("'captcha.remote_service_url'");
    expect(settings).toContain("'captcha.remote_secret_key'");
    expect(settings).toContain("'captcha.remote_pass_cookies'");
    expect(settings).toContain('默认关闭');
    expect(settings).toContain('只有网络不可用或超时才回退本机引擎');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('email notification config separates system and custom SMTP modes', () => {
    const notifications = readFrontendFile('app/features/notifications/pages/Notifications.tsx'); /* notifications 表示notifications。 */
    const notificationState = readFrontendFile('app/features/notifications/state.ts'); /* notificationState 表示notificationState。 */
    const notificationModal = readFrontendFile('app/features/notifications/components/NotificationChannelModal.tsx'); /* notificationModal 表示notificationModal。 */

    expect(notifications).toContain('interface NotificationsProps');
    expect(notifications).toContain('{isAdmin && <NotificationSmtpSettings');
    expect(notificationState).toContain("key: 'to_email'");
    expect(notificationModal).toContain('完整继承系统 SMTP');
    expect(notificationModal).toContain('use_custom_smtp');
    expect(notificationState).not.toContain("key: 'from'");
    expect(notifications).not.toContain('注册验证码');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('keyword reply UI matches contains-based backend behavior', () => {
    const rules = readFrontendFile('app/features/rules/pages/Rules.tsx'); /* rules 表示规则集合。 */

    expect(rules).toContain('包含匹配');
    expect(rules).not.toContain('精确匹配');
    expect(rules).not.toContain('模糊包含');
    expect(rules).not.toContain('匹配类型');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('vite proxy does not advertise removed backend routes', () => {
    const vite = readFrontendFile('vite.config.ts'); /* vite 表示vite。 */

    for (const staleRoute /* staleRoute 表示staleRoute。 */ of [
      '/backup',
      '/logs',
      '/register',
      '/generate-captcha',
      '/verify-captcha',
      '/geetest',
      '/send-verification-code',
    ]) {
      expect(vite).not.toContain(`'${staleRoute}'`);
    }
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('item delivery shortcut opens existing automation rule modal', () => {
    const actions = readFrontendFile('app/features/rules/ruleActions.ts'); /* actions 表示规则动作协调器源码。 */
    const existingRuleBranch = actions.match(/if \(rule\) \{([\s\S]*?)\} else \{/); /* existingRuleBranch 表示existing当前规则Branch。 */

    expect(existingRuleBranch?.[1]).toContain('openAutomationRule(rule)');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('item delivery shortcut is not marked handled before async open completes', () => {
    const actions = readFrontendFile('app/features/rules/ruleActions.ts'); /* actions 表示规则动作协调器源码。 */

    expect(actions).not.toContain('handledDeliveryTarget.current = initialDeliveryTarget.requestId');
    expect(actions).toContain('onDeliveryTargetHandled?.();');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('automation editor keeps multiple delivery contents for normal products', () => {
    const rules = readFrontendFile('app/features/rules/pages/Rules.tsx'); /* rules 表示规则集合。 */
    const actions = readFrontendFile('app/features/rules/ruleActions.ts'); /* actions 表示规则动作协调器源码。 */

    expect(rules).toContain('添加发货内容');
    expect(rules).toContain('{displayVariants.map((variant, index) => (');
    expect(actions).toContain('variants.map(');
    expect(rules).not.toContain(': (isMultiSpecRule ? variants : [variants[0]]).map');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('batch publishing help explains card fields without required-field jargon', () => {
    const itemList = readFrontendFile('app/features/items/pages/ItemList.tsx'); /* itemList 表示当前商品List。 */

    expect(itemList).not.toContain('条件必填');
    expect(itemList).toContain('“付款后发送的卡密”怎么填');
    expect(itemList).toContain('101:1:0;102:2:3');
    expect(itemList).toContain('买家购买 3 件时会发送 6 份');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('item publish image previews revoke object urls', () => {
    const itemList = readFrontendFile('app/features/items/itemActions.ts'); /* itemList 表示商品动作协调器源码。 */

    expect(itemList).toContain('setPublishImagePreviews');
    expect(itemList).toContain('URL.createObjectURL(file)');
    expect(itemList).toContain('URL.revokeObjectURL(preview.url)');
    expect(itemList).not.toContain('src={URL.createObjectURL(file)}');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('QR verification removes the external link and clearly requires in-app risk verification', () => {
    const accounts = readFrontendFile('app/features/accounts/pages/AccountList.tsx'); /* accounts 表示账号集合。 */
    const qrModal = readFrontendFile('app/features/accounts/components/AccountQRCodeModal.tsx'); /* qrModal 表示二维码登录弹窗。 */
	const riskPanel = readFrontendFile('app/features/accounts/components/RiskVerificationPanel.tsx'); /* riskPanel 表示riskPanel。 */
    expect(accounts).not.toContain('href={verificationUrl}');
    expect(accounts).not.toContain('setVerificationUrl');
	expect(qrModal).toContain('RiskVerificationPanel');
	expect(riskPanel).toContain('需要完成安全风控验证');
	expect(riskPanel).toContain('请勿在浏览器中打开验证链接');
		expect(riskPanel).toContain('系统会自动检测并刷新登录状态');
		expect(riskPanel).not.toContain('我已在闲鱼 App 完成验证');
		expect(riskPanel).not.toContain('<button');
	expect(riskPanel).not.toContain('重试');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);

  test('account editor exposes password-login refresh and never renders its verification URL', () => {
    const accounts = [
      readFrontendFile('app/features/accounts/pages/AccountList.tsx'),
      readFrontendFile('app/features/accounts/components/AccountEditModal.tsx'),
      readFrontendFile('app/features/accounts/submoduleHooks.ts'),
    ].join('\n'); /* accounts 表示账号集合。 */
    expect(accounts).toContain('passwordLogin({');
	expect(accounts).toContain('checkPasswordLoginStatus(sessionId, controller.signal)');
    expect(accounts).toContain('密码登录刷新授权');
    expect(accounts).toContain('账号已触发平台风控，需要完成人脸识别');
    expect(accounts).not.toContain('result.verification_url');
  } /* 测试回调断言已登录应用的路由、访问控制或延迟加载契约。 */);
} /* 测试套件回调汇总路由装配与访问边界契约。 */);
