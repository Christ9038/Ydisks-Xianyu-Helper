import React,{ useEffect,useState } from 'react';
import type { LucideIcon } from 'lucide-react';
import {
  Bell, Box, CreditCard, FileStack, GitCommitHorizontal, LayoutDashboard, Library, LogOut,
  MessageCircleMore, MoreHorizontal, ScrollText, Settings, ShoppingBag, TrendingUp, Users, X, Zap,
} from 'lucide-react';

/** MobileNavBuildInfo 描述移动端底部导航展示的公开构建版本信息。 */
export interface MobileNavBuildInfo {
  /** version 是当前服务构建版本，只用于界面展示，不参与任何业务判断。 */
  version: string;
  /** commit 是当前服务提交标识，用于在版本旁展示可追溯的构建来源。 */
  commit: string;
}

/** MobileNavProps 描述移动端底部主导航和“更多”抽屉所需的导航、权限与会话回调。 */
export interface MobileNavProps {
  /** activeTab 表示当前展示的业务页面标识，用于高亮对应主入口或“更多”入口。 */
  activeTab: string;
  /** isAdmin 表示当前会话是否拥有管理员权限，决定抽屉是否展示系统与日志入口。 */
  isAdmin: boolean;
  /** buildInfo 是应用壳读取的公开构建版本，只在抽屉底部展示。 */
  buildInfo: MobileNavBuildInfo;
  /** hasUnreadChatMessage 表示在线聊天仍有未读消息，聊天主入口据此展示红点。 */
  hasUnreadChatMessage?: boolean;
  /** onNavigate 由已登录应用壳提供，负责切换业务页面并同步浏览器路由。 */
  onNavigate: (tab: string) => void;
  /** onLogout 负责注销当前会话并清理认证状态。 */
  onLogout: () => void;
}

/** MobileNavItem 描述一个移动端导航入口的稳定标识、中文标签和图标。 */
interface MobileNavItem {
  /** id 是传给 onNavigate 的业务页面标识，必须与页面组合器的页面标识保持一致。 */
  id: string;
  /** label 是按钮展示和读屏使用的中文名称。 */
  label: string;
  /** icon 是入口使用的 lucide 图标组件。 */
  icon: LucideIcon;
}

/** mobilePrimaryNavItems 是移动端常驻的五个主入口；“更多”入口由抽屉状态驱动。 */
const mobilePrimaryNavItems: MobileNavItem[] = [
  { id: 'dashboard', label: '仪表盘', icon: LayoutDashboard },
  { id: 'chat', label: '聊天', icon: MessageCircleMore },
  { id: 'accounts', label: '账号', icon: Users },
  { id: 'orders', label: '订单', icon: ShoppingBag },
  { id: 'more', label: '更多', icon: MoreHorizontal },
];

/** mobileMoreNavItems 是“更多”抽屉中的常规业务入口，顺序与侧边栏分组保持一致。 */
const mobileMoreNavItems: MobileNavItem[] = [
  { id: 'library', label: '本地资源库', icon: Library },
  { id: 'cards', label: '卡密库存', icon: CreditCard },
  { id: 'items', label: '商品列表', icon: Box },
  { id: 'sales', label: '销售统计', icon: TrendingUp },
  { id: 'delivery-templates', label: '发货模板', icon: FileStack },
  { id: 'rules', label: '自动化规则', icon: Zap },
  { id: 'notifications', label: '通知设置', icon: Bell },
];

/** mobileAdminNavItems 是仅管理员可见的系统入口，普通账号既不渲染也不接收点击。 */
const mobileAdminNavItems: MobileNavItem[] = [
  { id: 'settings', label: '系统与AI', icon: Settings },
  { id: 'logs', label: '运行日志', icon: ScrollText },
];

/** MobileNav 在 lg 以下渲染固定底部主导航，并通过“更多”底部抽屉承载次级页面入口。 */
const MobileNav: React.FC<MobileNavProps> = ({
  activeTab,
  isAdmin,
  buildInfo,
  hasUnreadChatMessage = false,
  onNavigate,
  onLogout,
}) => {
  /** moreOpen 表示“更多”底部抽屉是否展开；它属于短暂 UI 状态，不写入服务端或持久化存储。 */
  const [moreOpen, setMoreOpen] = useState(false);
  /** sheetItems 是当前权限下抽屉可见的次级入口；管理员额外获得系统设置与运行日志。 */
  const sheetItems = isAdmin ? [...mobileMoreNavItems, ...mobileAdminNavItems] : mobileMoreNavItems;
  /** sheetItemIDs 保存抽屉内全部页面标识，用于让“更多”主入口在次级页面保持高亮。 */
  const sheetItemIDs = sheetItems.map(/* item 是当前抽屉入口，用于提取其业务页面标识。 */ item => item.id);
  /** moreActive 表示当前页面属于抽屉入口或抽屉正在展开，“更多”入口据此使用品牌色高亮。 */
  const moreActive = moreOpen || sheetItemIDs.includes(activeTab);
  /** displayVersion 沿用侧边栏格式：标准三段版本加 v 前缀，其余构建标识原样展示。 */
  const displayVersion = /^\d+\.\d+\.\d+$/.test(buildInfo.version) ? `v${buildInfo.version}` : buildInfo.version;

  /** handleToggleMore 由“更多”主入口点击触发，切换底部抽屉的展开状态。 */
  const handleToggleMore = (): void => {
    setMoreOpen(/* currentOpen 是切换前的抽屉状态，新状态与其相反。 */ currentOpen => !currentOpen);
  };

  /** handleCloseMore 由遮罩、关闭按钮或 Escape 触发，收起抽屉但保留当前页面不变。 */
  const handleCloseMore = (): void => {
    setMoreOpen(false);
  };

  /** handleNavigate 由主入口或抽屉条目点击触发，先收起抽屉再交给应用壳切换页面。 */
  const handleNavigate = (tab: string): void => {
    setMoreOpen(false);
    onNavigate(tab);
  };

  /** handleLogout 由抽屉退出按钮触发，先收起抽屉再注销会话，避免遮罩残留在认证页。 */
  const handleLogout = (): void => {
    setMoreOpen(false);
    onLogout();
  };

  useEffect(/* effect 在抽屉展开时监听全局 Escape 按键，关闭或卸载时移除监听。 */ () => {
    if (!moreOpen) return;
    /** handleKeyDown 接收全局按键事件，只在 Escape 时收起抽屉，其他按键保持页面行为。 */
    const handleKeyDown = (event: KeyboardEvent): void => {
      if (event.key === 'Escape') setMoreOpen(false);
    };
    window.addEventListener('keydown', handleKeyDown);
    return /* cleanup 移除本轮抽屉的全局按键监听，避免关闭后继续响应 Escape。 */ () => window.removeEventListener('keydown', handleKeyDown);
  }, [moreOpen]);

  return (
    <>
      <nav
        aria-label="移动端主导航"
        className="fixed inset-x-0 bottom-0 z-30 border-t border-line/80 bg-surface/95 pb-[env(safe-area-inset-bottom)] shadow-[0_-8px_24px_rgb(var(--color-slate-900)/0.06)] backdrop-blur-xl lg:hidden"
      >
        <div className="flex h-[var(--mobile-nav-height)] items-stretch gap-1 px-1">
          {mobilePrimaryNavItems.map(/* item 是当前渲染的底部主入口。 */ item => {
            /** active 表示当前入口是否对应当前页面；“更多”入口在抽屉页面保持高亮。 */
            const active = item.id === 'more' ? moreActive : activeTab === item.id;
            /** Icon 渲染当前入口的 lucide 图标组件。 */
            const Icon = item.icon;
            return (
              <button
                key={item.id}
                type="button"
                aria-label={item.label}
                aria-current={active ? 'page' : undefined}
                aria-haspopup={item.id === 'more' ? 'dialog' : undefined}
                aria-expanded={item.id === 'more' ? moreOpen : undefined}
                onClick={/* 用户点击主入口时直接导航，点击“更多”则切换抽屉。 */ () => {
                  if (item.id === 'more') {
                    handleToggleMore();
                    return;
                  }
                  handleNavigate(item.id);
                }}
                className={`relative flex h-full min-h-[44px] flex-1 flex-col items-center justify-center gap-1 rounded-xl transition-colors ${active ? 'bg-brand/10 text-brand' : 'text-muted hover:bg-surface-subtle hover:text-ink'}`}
              >
                <span className="relative flex h-5 w-5 items-center justify-center">
                  <Icon className="h-5 w-5" aria-hidden="true" />
                  {item.id === 'chat' && hasUnreadChatMessage && (
                    <span className="absolute -right-1.5 -top-1 h-2 w-2 rounded-full bg-danger-500" role="status" aria-label="在线聊天有未读消息" />
                  )}
                </span>
                <span className="text-xs font-bold leading-none">{item.label}</span>
              </button>
            );
          })}
        </div>
      </nav>

      {moreOpen && (
        <div className="fixed inset-0 z-40 lg:hidden" role="dialog" aria-modal="true" aria-labelledby="mobile-more-title">
          {/* 半透明遮罩独立成层，点击只关闭抽屉，不把事件传给底层导航或业务页面。 */}
          <div
            className="absolute inset-0 bg-slate-950/40 backdrop-blur-[2px]"
            data-testid="mobile-nav-backdrop"
            aria-hidden="true"
            onClick={handleCloseMore}
          />
          {/* 抽屉从底部出现并在内容超高时自身滚动；不锁定页面滚动，也不影响桌面布局。 */}
          <div className="absolute inset-x-0 bottom-0 max-h-[80vh] overflow-y-auto rounded-t-2xl border-t border-line bg-surface pb-[env(safe-area-inset-bottom)] shadow-2xl animate-slide-up">
            <div className="mx-auto flex max-w-2xl flex-col">
              <div className="flex items-start justify-between gap-4 px-5 pt-4">
                <div className="min-w-0">
                  <h2 id="mobile-more-title" className="text-base font-black tracking-tight text-ink">更多功能</h2>
                  <p className="mt-1 text-xs font-medium text-muted">选择需要前往的页面，当前页面内容不会被清除。</p>
                </div>
                <button
                  type="button"
                  aria-label="关闭更多导航"
                  onClick={handleCloseMore}
                  className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl text-muted transition-colors hover:bg-surface-subtle hover:text-ink"
                >
                  <X className="h-5 w-5" aria-hidden="true" />
                </button>
              </div>

              <div className="grid grid-cols-3 gap-2 px-4 py-4">
                {sheetItems.map(/* item 是当前权限下可见的抽屉入口。 */ item => {
                  /** active 表示抽屉入口是否对应当前页面。 */
                  const active = activeTab === item.id;
                  /** Icon 渲染当前抽屉入口的 lucide 图标组件。 */
                  const Icon = item.icon;
                  return (
                    <button
                      key={item.id}
                      type="button"
                      aria-current={active ? 'page' : undefined}
                      onClick={/* 用户选择抽屉入口后关闭抽屉并导航到对应页面。 */ () => handleNavigate(item.id)}
                      className={`flex min-h-[44px] flex-col items-center justify-center gap-1.5 rounded-xl px-2 py-3 text-xs font-bold transition-colors ${active ? 'bg-brand/10 text-brand' : 'bg-surface-subtle text-ink hover:bg-brand/5 hover:text-brand'}`}
                    >
                      <Icon className="h-5 w-5" aria-hidden="true" />
                      <span className="truncate">{item.label}</span>
                    </button>
                  );
                })}
              </div>

              <div className="flex items-center justify-between gap-3 border-t border-line/70 px-5 pb-4 pt-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-1.5 text-[10px] font-extrabold uppercase tracking-[0.18em] text-muted">
                    <GitCommitHorizontal className="h-3.5 w-3.5" aria-hidden="true" />
                    运行版本
                  </div>
                  <div className="mt-1 flex items-baseline gap-2">
                    <span className="truncate font-mono text-xs font-bold text-ink">{displayVersion}</span>
                    <span className="truncate font-mono text-[10px] text-muted">{buildInfo.commit}</span>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={handleLogout}
                  className="flex min-h-[44px] shrink-0 items-center gap-2 rounded-xl px-3 text-sm font-bold text-danger-600 transition-colors hover:bg-danger-50"
                >
                  <LogOut className="h-4 w-4" aria-hidden="true" />
                  退出登录
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default MobileNav;
