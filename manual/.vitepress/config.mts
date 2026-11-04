import { defineConfig, type DefaultTheme } from 'vitepress';
import { groups } from '../scripts/pages.mjs';

// sidebar 按语言生成侧边栏；prefix 为该语言的站内路径前缀，idx 取页面元组中的标题列。
const sidebar = (prefix: string, groupIdx: 'zh' | 'en', titleIdx: number): DefaultTheme.SidebarItem[] =>
  groups.map((g) => ({
    text: g[groupIdx],
    items: g.pages.map((p) => ({ text: p[titleIdx] as string, link: `${prefix}${p[0] === 'index' ? '' : p[0]}` })),
  }));

export default defineConfig({
  title: 'Ydisks闲鱼助手手册',
  base: '/manual/',
  outDir: 'dist',
  cleanUrls: true,
  lastUpdated: false,
  head: [['link', { rel: 'icon', type: 'image/svg+xml', href: '/manual/favicon.svg' }]],
  // 手册内容由 docs/wiki 生成，其中可能出现指向仓库其它位置的相对链接，不让它们阻断构建。
  ignoreDeadLinks: false,
  themeConfig: {
    logo: '/favicon.svg',
    socialLinks: [{ icon: 'github', link: 'https://github.com/Christ9038/Ydisks-Xianyu-Helper' }],
    search: { provider: 'local' },
    outline: { level: [2, 3] },
  },
  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      description: 'Ydisks闲鱼助手完整使用手册：部署、账号、卡密、商品、订单、自动发货、AI、通知与风控。',
      themeConfig: {
        nav: [
          { text: '快速开始', link: '/deployment' },
          { text: '常见问题', link: '/faq' },
          { text: '官网', link: 'https://xianyu.ydisks.com/', target: '_self' },
        ],
        sidebar: sidebar('/', 'zh', 3),
        outline: { label: '本页目录', level: [2, 3] },
        docFooter: { prev: '上一页', next: '下一页' },
        returnToTopLabel: '回到顶部',
        sidebarMenuLabel: '菜单',
        darkModeSwitchLabel: '深色模式',
        langMenuLabel: '切换语言',
        search: {
          provider: 'local',
          options: {
            translations: {
              button: { buttonText: '搜索手册', buttonAriaLabel: '搜索手册' },
              modal: {
                noResultsText: '没有找到相关内容',
                resetButtonTitle: '清除',
                footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' },
              },
            },
          },
        },
      },
    },
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      description: 'Complete user manual for Ydisks Xianyu Helper.',
      themeConfig: {
        nav: [
          { text: 'Get Started', link: '/en/deployment' },
          { text: 'FAQ', link: '/en/faq' },
          { text: 'Website', link: 'https://xianyu.ydisks.com/', target: '_self' },
        ],
        sidebar: sidebar('/en/', 'en', 4),
      },
    },
  },
});
