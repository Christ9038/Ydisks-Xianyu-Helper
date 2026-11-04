// 手册页面清单：slug 是站内路径，zh/en 是 docs/wiki 下对应的源文件（不含 .md），分组决定侧边栏结构。
export const groups = [
  { zh: '开始使用', en: 'Getting Started', pages: [
    ['index', 'Home.zh-CN', 'Home', '首页与上线路径', 'Home & launch path'],
    ['deployment', '部署与数据库.zh-CN', 'Deployment-and-Database', '部署与数据库', 'Deployment and Database'],
    ['config', '配置参考.zh-CN', 'Configuration-Reference', '配置参考', 'Configuration Reference'],
    ['overview', '账号、库存与商品.zh-CN', 'Accounts-Inventory-and-Products', '上线前总览', 'Pre-launch Checklist'],
  ] },
  { zh: '账号、库存与商品', en: 'Accounts, Inventory & Products', pages: [
    ['accounts', '账号管理.zh-CN', 'Account-Management', '账号管理', 'Account Management'],
    ['cards', '卡密库存.zh-CN', 'Card-Inventory', '卡密库存', 'Card Inventory'],
    ['products', '商品管理.zh-CN', 'Product-Management', '商品管理', 'Product Management'],
    ['bulk-listing', '批量铺货.zh-CN', 'Bulk-Listing', '批量铺货', 'Bulk Listing'],
    ['library', '资源库.zh-CN', 'Resource-Library', '本地资源库', 'Resource Library'],
  ] },
  { zh: '订单与自动发货', en: 'Orders & Delivery', pages: [
    ['orders', '订单管理.zh-CN', 'Orders', '订单管理', 'Orders'],
    ['templates', '发货模板.zh-CN', 'Delivery-Templates', '发货模板', 'Delivery Templates'],
    ['automation', '自动化发货与回复.zh-CN', 'Automated-Delivery-and-Replies', '自动化发货与回复', 'Automated Delivery and Replies'],
    ['bargain', '砍价订单与自动发货流程.zh-CN', 'Bargain-Orders-and-Delivery-Flow', '砍价订单与自动发货流程', 'Bargain Orders and Delivery Flow'],
  ] },
  { zh: '沟通与经营', en: 'Chat & Operations', pages: [
    ['dashboard', '仪表盘.zh-CN', 'Dashboard', '仪表盘', 'Dashboard'],
    ['chat', '在线聊天与账号自动任务.zh-CN', 'Chat-and-Account-Automation', '在线聊天与账号自动任务', 'Chat and Account Automation'],
    ['sales', '销售统计.zh-CN', 'Sales-Statistics', '销售统计', 'Sales Statistics'],
    ['logs', '运行日志.zh-CN', 'Runtime-Logs', '运行日志', 'Runtime Logs'],
  ] },
  { zh: 'AI 与通知', en: 'AI & Notifications', pages: [
    ['system', '系统设置.zh-CN', 'System-and-AI', '系统与 AI', 'System and AI'],
    ['notifications', 'AI、通知与运维.zh-CN', 'AI-Notifications-and-Operations', '通知设置与运维', 'Notifications and Operations'],
    ['notification-events', '通知事件参考.zh-CN', 'Notification-Events', '通知事件参考', 'Notification Events'],
  ] },
  { zh: '风控与排障', en: 'Risk Control & Troubleshooting', pages: [
    ['pacing', '请求频率与风控设计.zh-CN', 'Request-Pacing-and-Risk-Control', '请求频率与风控设计', 'Request Pacing and Risk Control'],
    ['risk', '风控处理.zh-CN', 'Risk-Control-Troubleshooting', '账号被风控了怎么办', 'Risk-Control Troubleshooting'],
    ['faq', '常见问题.zh-CN', 'FAQ', '常见问题', 'FAQ'],
  ] },
  { zh: '参考', en: 'Reference', pages: [
    ['admin-api', '管理API.zh-CN', 'Admin-API', '管理 API', 'Admin API'],
  ] },
];
