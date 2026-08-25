/** AppRoute 表示认证后可通过浏览器地址直接访问的业务页面。 */
export type AppRoute = 'dashboard' | 'accounts' | 'chat' | 'orders' | 'cards' | 'items' | 'rules' | 'delivery-templates' | 'notifications' | 'settings';

/** routeByPath 保存浏览器地址到业务路由标识的唯一映射，避免页面各自解析 URL。 */
export const routeByPath: Readonly<Record<string, AppRoute>> = {
  '/app/dashboard': 'dashboard',
  '/app/accounts': 'accounts',
  '/app/chat': 'chat',
  '/app/orders': 'orders',
  '/app/cards': 'cards',
  '/app/items': 'items',
  '/app/rules': 'rules',
  '/app/delivery-templates': 'delivery-templates',
  '/app/notifications': 'notifications',
  '/app/settings': 'settings',
};

/** pathByRoute 保存业务路由标识的规范 URL，所有应用内跳转都使用该映射。 */
export const pathByRoute: Readonly<Record<AppRoute, string>> = Object.fromEntries(
  Object.entries(routeByPath).map(/* routePairMapper 将地址映射翻转为应用内导航所需的路由映射。 */ ([path, route] /* path 是规范地址；route 是对应业务页面。 */) => [route, path]),
) as Record<AppRoute, string>;

/** routeFromLocation 从当前浏览器地址解析业务路由，未知地址安全回退到仪表盘。 */
export const routeFromLocation = (): AppRoute => routeByPath[window.location.pathname] ?? 'dashboard';
