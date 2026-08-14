import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';

const repoRoot = resolve(__dirname);

const readFrontendFile = (relativePath: string) =>
  readFileSync(resolve(repoRoot, relativePath), 'utf8');

const extractSingleQuotedValues = (source: string, pattern: RegExp) => {
  const values = new Set<string>();
  for (const match of source.matchAll(pattern)) {
    values.add(match[1]);
  }
  return values;
};

describe('frontend navigation routing', () => {
  test('sidebar entries and App activeTab routes stay in sync', () => {
    const sidebar = readFrontendFile('components/Sidebar.tsx');
    const app = readFrontendFile('App.tsx');

    const sidebarIDs = extractSingleQuotedValues(sidebar, /id:\s*'([^']+)'/g);
    const appRouteIDs = extractSingleQuotedValues(app, /case\s+'([^']+)'/g);

    expect([...sidebarIDs].sort()).toEqual([...appRouteIDs].sort());
  });

  test('active navigation uses the primary action blue', () => {
    const sidebar = readFrontendFile('components/Sidebar.tsx');

    expect(sidebar).toContain("'bg-brand text-white shadow-brand-active'");
    expect(sidebar).not.toContain("'bg-sky-500 text-white'");
  });

  test('logout button invalidates the backend session before clearing UI state', () => {
    const app = readFrontendFile('App.tsx');

    expect(app).toContain('import { initializeAdmin, login, logout, verifySession }');
    expect(app).toContain('await logout();');
    expect(app).toContain('onLogout={handleLogout}');
  });

  test('settings page does not expose backend-inactive system controls', () => {
    const settings = readFrontendFile('components/Settings.tsx');

    expect(settings).not.toContain('允许用户注册');
    expect(settings).not.toContain('显示默认登录信息');
    expect(settings).not.toContain('启用商品自动同步');
    expect(settings).not.toContain('商品同步间隔');
    expect(settings).not.toContain('默认自动回复内容');
    expect(settings).toContain('SETTINGS_SAVE_OMIT_KEYS');
    expect(settings).toContain('保存后需重启服务生效');
  });

  test('admin-only settings navigation is gated by session role', () => {
    const app = readFrontendFile('App.tsx');
    const sidebar = readFrontendFile('components/Sidebar.tsx');
    const settings = readFrontendFile('components/Settings.tsx');

    expect(app).toContain('const [isAdmin, setIsAdmin] = useState(false)');
    expect(app).toContain('setIsAdmin(res.is_admin === true)');
    expect(app).toContain("activeTab === 'settings'");
    expect(app).toContain('isAdmin ? <Settings /> : <Dashboard />');
    expect(sidebar).toContain('isAdmin = false');
    expect(sidebar).toContain("...(isAdmin ? [{ id: 'settings'");
    expect(settings).toContain('setLoadError');
  });

  test('captcha remote settings expose the reference privacy and fallback semantics', () => {
    const settings = readFrontendFile('components/Settings.tsx');

    expect(settings).toContain('远程过滑块配置');
    expect(settings).toContain("'captcha.remote_service_url'");
    expect(settings).toContain("'captcha.remote_secret_key'");
    expect(settings).toContain("'captcha.remote_pass_cookies'");
    expect(settings).toContain('默认关闭');
    expect(settings).toContain('只有网络不可用或超时才回退本机引擎');
  });

  test('email notification config separates system and custom SMTP modes', () => {
    const notifications = readFrontendFile('components/Notifications.tsx');
    const notificationState = readFrontendFile('app/features/notifications/state.ts');
    const notificationModal = readFrontendFile('app/features/notifications/components/NotificationChannelModal.tsx');

    expect(notifications).toContain('interface NotificationsProps');
    expect(notifications).toContain('{isAdmin && <NotificationSmtpSettings');
    expect(notificationState).toContain("key: 'to_email'");
    expect(notificationModal).toContain('完整继承系统 SMTP');
    expect(notificationModal).toContain('use_custom_smtp');
    expect(notificationState).not.toContain("key: 'from'");
    expect(notifications).not.toContain('注册验证码');
  });

  test('keyword reply UI matches contains-based backend behavior', () => {
    const rules = readFrontendFile('components/Rules.tsx');

    expect(rules).toContain('包含匹配');
    expect(rules).not.toContain('精确匹配');
    expect(rules).not.toContain('模糊包含');
    expect(rules).not.toContain('匹配类型');
  });

  test('vite proxy does not advertise removed backend routes', () => {
    const vite = readFrontendFile('vite.config.ts');

    for (const staleRoute of [
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
  });

  test('item delivery shortcut opens existing automation rule modal', () => {
    const rules = readFrontendFile('components/Rules.tsx');
    const existingRuleBranch = rules.match(/if \(rule\) \{([\s\S]*?)\} else \{/);

    expect(existingRuleBranch?.[1]).toContain('openAutomationRule(rule)');
  });

  test('item delivery shortcut is not marked handled before async open completes', () => {
    const rules = readFrontendFile('components/Rules.tsx');

    expect(rules).not.toContain('handledDeliveryTarget.current = initialDeliveryTarget.requestId');
    expect(rules).toContain('onDeliveryTargetHandled?.();');
  });

  test('automation editor keeps multiple delivery contents for normal products', () => {
    const rules = readFrontendFile('components/Rules.tsx');

    expect(rules).toContain('添加发货内容');
    expect(rules).toContain('{displayVariants.map((variant, index) => (');
    expect(rules).toContain(': variants.map(variant => ({');
    expect(rules).not.toContain(': (isMultiSpecRule ? variants : [variants[0]]).map');
  });

  test('batch publishing help explains card fields without required-field jargon', () => {
    const itemList = readFrontendFile('components/ItemList.tsx');

    expect(itemList).not.toContain('条件必填');
    expect(itemList).toContain('“付款后发送的卡密”怎么填');
    expect(itemList).toContain('101:1:0;102:2:3');
    expect(itemList).toContain('买家购买 3 件时会发送 6 份');
  });

  test('item publish image previews revoke object urls', () => {
    const itemList = readFrontendFile('components/ItemList.tsx');

    expect(itemList).toContain('setPublishImagePreviews');
    expect(itemList).toContain('URL.createObjectURL(file)');
    expect(itemList).toContain('URL.revokeObjectURL(preview.url)');
    expect(itemList).not.toContain('src={URL.createObjectURL(file)}');
  });

  test('QR verification removes the external link and clearly requires in-app risk verification', () => {
    const accounts = readFrontendFile('components/AccountList.tsx');
	const riskPanel = readFrontendFile('components/RiskVerificationPanel.tsx');
    expect(accounts).not.toContain('href={verificationUrl}');
    expect(accounts).not.toContain('setVerificationUrl');
	expect(accounts).toContain('RiskVerificationPanel');
	expect(riskPanel).toContain('需要完成安全风控验证');
	expect(riskPanel).toContain('请勿在浏览器中打开验证链接');
		expect(riskPanel).toContain('系统会自动检测并刷新登录状态');
		expect(riskPanel).not.toContain('我已在闲鱼 App 完成验证');
		expect(riskPanel).not.toContain('<button');
	expect(riskPanel).not.toContain('重试');
  });

  test('account editor exposes password-login refresh and never renders its verification URL', () => {
    const accounts = [
      readFrontendFile('components/AccountList.tsx'),
      readFrontendFile('app/features/accounts/components/AccountEditModal.tsx'),
    ].join('\n');
    expect(accounts).toContain('passwordLogin({');
	expect(accounts).toContain('checkPasswordLoginStatus(sessionId, controller.signal)');
    expect(accounts).toContain('密码登录刷新授权');
    expect(accounts).toContain('账号已触发平台风控，需要完成人脸识别');
    expect(accounts).not.toContain('result.verification_url');
  });
});
