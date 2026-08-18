import { Activity,AlertCircle,DollarSign,ExternalLink,Package,PackageCheck,ShoppingCart,TrendingUp,Users } from 'lucide-react';
import React,{ useState } from 'react';
import { Cell,Legend,Pie,PieChart,ResponsiveContainer,Tooltip } from 'recharts';
import { getDateRange,TimeRange } from '../../../../dateRange';
import { formatLocalDateTime } from '../../../../dateTime';
import { OrderStatus } from '../api';
import { DashboardTrendChart } from '../DashboardTrendChart';
import { useDashboard } from '../hooks';

// cssColor 状态颜色样式。
const cssColor = (token: string, alpha?: number) => (
  alpha === undefined
    ? `rgb(var(--color-${token}))`
    : `rgb(var(--color-${token}) / ${alpha})`
);

// 状态徽章组件
export const StatusBadge: React.FC<{ /** status 表示状态。 */ status: OrderStatus }> = ({ status }) => {
  // styles 样式表。
  const styles = {
    processing: 'bg-blue-100 text-blue-800',
    pending_ship: 'bg-brand text-white',
    shipped: 'bg-blue-100 text-blue-700',
    completed: 'bg-green-100 text-green-700',
    cancelled: 'bg-gray-100 text-gray-500',
    refunding: 'bg-red-100 text-red-600',
    unknown: 'bg-gray-100 text-gray-500',
  };

  // labels labels，负责当前功能中的对应处理。
  const labels = {
    processing: '处理中',
    pending_ship: '待发货',
    shipped: '已发货',
    completed: '已完成',
    cancelled: '已取消',
    refunding: '退款中',
    unknown: '未知',
  };

  return (
    <span className={`inline-flex items-center justify-center whitespace-nowrap px-3 py-1.5 rounded-lg text-xs leading-none font-bold ${styles[status] || styles.cancelled}`}>
      {labels[status] || status}
    </span>
  );
};

// StatCard 渲染统计卡片组件。
const StatCard: React.FC<{ /** title 表示标题。 */ title: string; /** value 表示值。 */ value: string | number; /** icon 表示icon。 */ icon: React.ElementType; /** colorClass 表示颜色Class。 */ colorClass: string; /** trend 表示趋势。 */ trend?: string }> = ({ title, value, icon: Icon, colorClass, trend }) => (
  <div className="ios-card p-6 rounded-xl flex flex-col justify-between hover:translate-y-[-4px] transition-all duration-300 h-full relative overflow-hidden group border-0">
    <div className={`absolute -right-6 -top-6 w-32 h-32 ${colorClass} opacity-10 rounded-full group-hover:scale-125 transition-transform duration-500 blur-2xl`}></div>
    <div className="flex justify-between items-start mb-6">
      <div className={`p-4 rounded-2xl ${colorClass} bg-opacity-10 backdrop-blur-sm`}>
        <Icon className={`w-6 h-6 ${colorClass.replace('bg-', 'text-')}`} />
      </div>
      {trend && <span className="text-xs font-bold text-white bg-brand px-3 py-1.5 rounded-full flex items-center gap-1 shadow-sm">
        <TrendingUp className="w-3 h-3" /> {trend}
      </span>}
    </div>
    <div className="relative z-10">
      <h3 className="text-3xl font-extrabold text-gray-900 tracking-tight font-feature-settings-tnum">{value}</h3>
      <p className="text-gray-500 text-sm font-medium mt-1">{title}</p>
    </div>
  </div>
);

// Dashboard 渲染仪表盘页面组件。
const Dashboard: React.FC = () => {
  // [timeRange, 解构得到当前 Hook 返回的状态和操作函数。
  const [timeRange, setTimeRange] = useState<TimeRange>('7days');
  // [customStartDate, 解构得到当前 Hook 返回的状态和操作函数。
  const [customStartDate, setCustomStartDate] = useState('');
  // [customEndDate, 解构得到当前 Hook 返回的状态和操作函数。
  const [customEndDate, setCustomEndDate] = useState('');
  // [searchTerm, 解构得到当前 Hook 返回的状态和操作函数。
  const [searchTerm, setSearchTerm] = useState('');
  // [customRangeVersion, 解构得到当前 Hook 返回的状态和操作函数。
  const [customRangeVersion, setCustomRangeVersion] = useState(0);
  // dashboard 仪表盘数据。
  const dashboard = useDashboard({ range: timeRange, customStartDate, customEndDate, customRangeVersion });
  // { 解构得到当前 Hook 返回的状态和操作函数。
  const { data, status, chartData, productSalesData, sourceData: sourceDataData, categoryData: categoryDataData, maxProductSales, trendPercent, selectedRangeLabel, refresh } = dashboard;
  // stats 统计概览数据。
  const stats = data?.stats || null;
  // analytics 统计分析数据。
  const analytics = data?.analytics || null;
  // validOrders 有效订单列表。
  const validOrders = data?.validOrders.orders || [];
  // validOrdersTotal 有效数据订单列表总数，负责当前功能中的对应处理。
  const validOrdersTotal = data?.validOrders.total || 0;
  // validOrdersTruncated 有效数据订单列表Truncated，负责当前功能中的对应处理。
  const validOrdersTruncated = data?.validOrders.truncated || false;
  // ordersLoading 订单加载状态。
  const ordersLoading = status.range === 'loading';
  // loadError 加载当前数据（错误）。
  const loadError = status.error;

  // 颜色配置
  const COLORS = [
    cssColor('brand'),
    cssColor('brand-highlight'),
    cssColor('success-500'),
    cssColor('warning-500'),
    cssColor('accent-500'),
  ];
  // formatCurrency 格式化金额函数。
  const formatCurrency = (value: number) => `¥${Number(value || 0).toLocaleString('zh-CN', { maximumFractionDigits: 2 })}`;

  if (loadError && (!stats || !analytics)) {
    return (
      <div className="p-8 flex flex-col items-center gap-3 text-red-600">
        <AlertCircle className="w-8 h-8" />
        <span>{loadError}</span>
        <button type="button" className="ios-btn-primary px-4 py-2 rounded-xl" onClick={refresh}>重新加载</button>
      </div>
    );
  }
  if (!stats || !analytics) return <div className="p-8 flex justify-center text-gray-400"><Activity className="w-8 h-8 animate-spin text-brand" /></div>;
  // totalOrders 总数订单列表，负责当前功能中的对应处理。
  const totalOrders = analytics.revenue_stats.total_orders || 0;
  // totalAmount 订单总金额。
  const totalAmount = analytics.revenue_stats.total_amount || 0;

  // timeRangeOptions time范围Options，负责当前功能中的对应处理。
  const timeRangeOptions = [
    { key: 'today' as TimeRange, label: '今天' },
    { key: 'yesterday' as TimeRange, label: '昨天' },
    { key: '3days' as TimeRange, label: '三天内' },
    { key: '7days' as TimeRange, label: '7天内' },
    { key: '30days' as TimeRange, label: '一个月内' },
    { key: 'custom' as TimeRange, label: '自定义' },
  ];
  // currentRangeDates 当前统计日期范围。
  let currentRangeDates;
  try {
    currentRangeDates = getDateRange(timeRange, new Date(), customStartDate, customEndDate);
  } catch {
    currentRangeDates = { startDate: customStartDate, endDate: customEndDate };
  }
  // normalizedSearchTerm 归一化当前数据（d搜索条件Term）。
  const normalizedSearchTerm = searchTerm.trim().toLowerCase();
  // filteredValidOrders 过滤后的有效订单列表。
  const filteredValidOrders = validOrders.filter(/* 当前回调处理集合中的单个元素。 */ (order) =>
    order.order_id?.toLowerCase().includes(normalizedSearchTerm) ||
    order.item_id?.toLowerCase().includes(normalizedSearchTerm) ||
    order.item_title?.toLowerCase().includes(normalizedSearchTerm) ||
    order.buyer_id?.toLowerCase().includes(normalizedSearchTerm)
  );

  return (
    <div className="space-y-8 animate-fade-in">
      {loadError && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">
          <span>{loadError}</span>
          <button type="button" className="font-bold underline" onClick={refresh}>重试</button>
        </div>
      )}
      <div className="flex flex-col md:flex-row justify-between md:items-end gap-4">
        <div>
          <h2 className="text-4xl font-extrabold text-gray-900 tracking-tight">运营概览</h2>
          <p className="text-gray-500 mt-2 text-base">欢迎回来，以下是闲鱼店铺的实时经营数据。</p>
        </div>
        <div className="flex items-center gap-3">
          <div className="text-sm font-bold text-gray-700 bg-white px-5 py-2.5 rounded-full shadow-sm border border-gray-100 flex items-center gap-2">
            <span className="w-2.5 h-2.5 bg-green-500 rounded-full animate-pulse"></span>
            系统正常运行
          </div>
        </div>
      </div>

      {/* Time Range Selector */}
      <div className="flex flex-wrap gap-2 p-2 bg-gray-100/50 rounded-2xl">
        {timeRangeOptions.map(/* 当前回调处理集合中的单个元素。 */ (option) => (
          <button
            key={option.key}
            onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setTimeRange(option.key)}
            className={`px-5 py-2.5 rounded-xl text-sm font-bold transition-all ${
              timeRange === option.key
                ? 'bg-brand text-white shadow-md'
                : 'bg-white text-gray-600 hover:text-black hover:bg-gray-50'
            }`}
          >
            {option.label}
          </button>
        ))}
        {timeRange === 'custom' && (
          <>
            <input
              type="date"
              value={customStartDate}
              onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setCustomStartDate(e.target.value)}
              className="px-3 py-2 rounded-xl text-sm border border-gray-200 focus:outline-none focus:ring-2 focus:ring-brand"
            />
            <span className="self-center text-gray-400">-</span>
            <input
              type="date"
              value={customEndDate}
              onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setCustomEndDate(e.target.value)}
              className="px-3 py-2 rounded-xl text-sm border border-gray-200 focus:outline-none focus:ring-2 focus:ring-brand"
            />
            <button
              onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => setCustomRangeVersion(/* 当前回调处理用户交互或异步状态变化。 */ value => value + 1)}
              className="px-4 py-2 rounded-xl text-sm font-bold bg-black text-white hover:bg-gray-800 transition-colors"
            >
              应用
            </button>
          </>
        )}
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <StatCard
          title="累计营收 (CNY)"
          value={`¥${analytics.revenue_stats.total_amount.toLocaleString('zh-CN', { minimumFractionDigits: 2 })}`}
          icon={DollarSign}
          colorClass="bg-blue-400"
          trend={trendPercent || undefined}
        />
        <StatCard
          title="活跃账号 / 总数"
          value={`${stats.active_cookies} / ${stats.total_cookies}`}
          icon={Users}
          colorClass="bg-blue-500"
        />
        <StatCard
          title="订单数"
          value={analytics.revenue_stats.total_orders.toLocaleString()}
          icon={ShoppingCart}
          colorClass="bg-blue-500"
        />
        <StatCard
          title="库存卡密余量"
          value={stats.available_card_stock}
          icon={Package}
          colorClass="bg-purple-500"
        />
      </div>

      <DashboardTrendChart chartData={chartData} selectedRangeLabel={selectedRangeLabel} totalAmount={totalAmount} />

      {/* 商品销量排行和订单来源分布 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* 商品销量排行 */}
        <div className="ios-card p-6 rounded-xl">
          <h3 className="font-bold text-lg text-gray-900 mb-6">商品销量排行</h3>
          <div className="h-[280px]">
            {productSalesData.length === 0 ? (
              <div className="flex items-center justify-center h-full text-gray-400">暂无数据</div>
            ) : (
              <div className="h-full space-y-4 overflow-y-auto pr-2">
                {productSalesData.map(/* 当前回调处理集合中的单个元素。 */ (item, index) => (
                  <div key={`${item.name}-${index}`} className="space-y-2">
                    <div className="flex items-center justify-between gap-4">
                      <div className="flex items-center gap-3 min-w-0">
                        <span className={`w-7 h-7 rounded-xl flex items-center justify-center text-xs font-extrabold ${index < 3 ? 'bg-blue-600 text-white' : 'bg-gray-100 text-gray-500'}`}>
                          {index + 1}
                        </span>
                        <span className="font-bold text-gray-800 text-sm truncate">{item.name}</span>
                      </div>
                      <span className="font-mono text-sm font-extrabold text-gray-900">{item.sales} 单</span>
                    </div>
                    <div className="h-3 rounded-full bg-gray-100 overflow-hidden">
                      <div
                        className="h-full rounded-full bg-brand"
                        style={{ width: `${Math.max(8, (item.sales / maxProductSales) * 100)}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* 商品下单占比 */}
        <div className="ios-card p-6 rounded-xl">
          <h3 className="font-bold text-lg text-gray-900 mb-6">商品下单占比</h3>
          <div
            className="dashboard-pie-chart h-[280px] relative"
            role="img"
            aria-label={`商品下单占比，共 ${totalOrders} 单`}
          >
            {sourceDataData.length === 0 ? (
              <div className="flex items-center justify-center h-full text-gray-400">暂无数据</div>
            ) : (
              <>
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart accessibilityLayer={false}>
                    <Pie
                      data={sourceDataData}
                      cx="50%"
                      cy="45%"
                      innerRadius={60}
                      outerRadius={90}
                      paddingAngle={2}
                      dataKey="value"
                      activeShape={{
                        outerRadius: 96,
                        stroke: 'none',
                        strokeWidth: 0,
                      }}
                      rootTabIndex={-1}
                      label={false}
                      labelLine={false}
                    >
                      {sourceDataData.map(/* 当前回调处理集合中的单个元素。 */ (entry, index) => (
                        <Cell key={`cell-${index}`} fill={entry.color} />
                      ))}
                    </Pie>
                    <Tooltip
                      formatter={/* 当前回调处理用户交互或异步状态变化。 */ (value) => `${Number(value || 0)} 单`}
                      wrapperStyle={{ zIndex: 30, outline: 'none' }}
                      contentStyle={{
                        backgroundColor: cssColor('white'),
                        border: `1px solid ${cssColor('neutral-200')}`,
                        borderRadius: '10px',
                        boxShadow: 'var(--shadow-md)'
                      }}
                    />
                    <Legend
                      verticalAlign="bottom"
                      height={36}
                      iconType="circle"
                      formatter={/* 当前回调处理用户交互或异步状态变化。 */ (value) => <span style={{ color: cssColor('neutral-500'), fontWeight: 500 }}>{value}</span>}
                    />
                  </PieChart>
                </ResponsiveContainer>
                <div className="pointer-events-none absolute inset-0 z-10 flex flex-col items-center justify-center pb-9">
                  <span className="text-2xl font-extrabold text-gray-900 tabular-nums">{totalOrders}</span>
                  <span className="text-xs font-medium text-gray-400 mt-0.5">总订单</span>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {/* 收支明细和品类营收 */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* 参与统计的订单列表 */}
        <div className="lg:col-span-2 ios-card p-0 rounded-xl border-0 bg-white overflow-hidden flex flex-col">
          <div className="p-6 border-b border-gray-50 flex justify-between items-center bg-surface-muted">
			<div>
			  <h3 className="font-bold text-lg text-gray-900">参与统计的订单</h3>
			  {validOrdersTruncated && (
				<p className="text-xs text-amber-700 mt-1">当前显示最近 {validOrders.length} / {validOrdersTotal} 条，搜索仅覆盖已加载明细。</p>
			  )}
			</div>
            <div className="relative">
              <input
                placeholder="搜索订单号/商品/买家..."
                value={searchTerm}
                onChange={/* 当前回调处理用户交互或异步状态变化。 */ (e) => setSearchTerm(e.target.value)}
                className="pl-4 pr-4 py-2 rounded-xl bg-white border border-gray-100 text-sm focus:border-blue-400 outline-none w-48"
                type="text"
              />
            </div>
          </div>
          <div className="overflow-x-auto flex-1 max-h-[400px]">
            {ordersLoading ? (
              <div className="flex items-center justify-center py-20 text-gray-400">
                <Activity className="w-6 h-6 animate-spin mr-2" />
                加载中...
              </div>
            ) : filteredValidOrders.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-16 px-8 text-center">
                <div className="w-14 h-14 rounded-2xl bg-gray-100 flex items-center justify-center mb-4">
                  <PackageCheck className="w-7 h-7 text-gray-300" />
                </div>
                {normalizedSearchTerm ? (
                  <>
                    <div className="text-sm font-extrabold text-gray-900">没有匹配的订单</div>
                    <div className="text-xs text-gray-400 mt-2 max-w-md">
                      当前共有 {validOrders.length} 单参与统计，但没有订单号、商品、买家匹配“{searchTerm}”。
                    </div>
                  </>
                ) : (
                  <>
                    <div className="text-sm font-extrabold text-gray-900">当前范围内没有参与统计的订单</div>
                    <div className="text-xs text-gray-400 mt-2 max-w-lg leading-6">
                      日期范围：{currentRangeDates.startDate} 至 {currentRangeDates.endDate}；
                      统计口径：待发货、已发货、已完成，且订单金额不为空。
                      当前统计卡片订单数：{analytics.revenue_stats.total_orders} 单。
                    </div>
                  </>
                )}
              </div>
            ) : (
              <table className="w-full min-w-[760px] text-left border-collapse">
                <thead>
                  <tr className="bg-white text-gray-400 text-xs font-bold uppercase tracking-wider border-b border-gray-50">
                    <th className="px-6 py-4">订单信息</th>
                    <th className="px-6 py-4">买家信息</th>
                    <th className="px-6 py-4">金额</th>
                    <th className="px-6 py-4 whitespace-nowrap">状态</th>
                    <th className="px-6 py-4 text-right">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-50">
                  {filteredValidOrders.map(/* 当前回调处理集合中的单个元素。 */ (order) => (
                      <tr key={order.order_id} className="hover:bg-warning-50/50 transition-colors group">
                        <td className="px-6 py-4">
                          <div className="flex items-center gap-3">
                            <div className="w-12 h-12 rounded-xl bg-gray-100 overflow-hidden shadow-sm border border-gray-100 flex-shrink-0">
                              <PackageCheck className="w-full h-full text-gray-300 p-2" />
                            </div>
                            <div className="min-w-0">
                              <div className="font-bold text-gray-900 text-sm line-clamp-1">
                                {order.item_title || order.item_id || '未知商品'}
                              </div>
                              <div className="text-xs text-gray-500 mt-1 font-mono">{order.order_id}</div>
                              <div className="text-xs text-gray-400 mt-0.5">数量: {order.quantity || 1}</div>
                            </div>
                          </div>
                        </td>
                        <td className="px-6 py-4">
                          <div className="text-sm font-bold text-gray-800">{order.buyer_id}</div>
                          {order.created_at && (
                            <div className="text-xs text-gray-400 mt-1">{formatLocalDateTime(order.created_at)}</div>
                          )}
                        </td>
                        <td className="px-6 py-4 text-base font-extrabold text-gray-900 font-feature-settings-tnum">
                          ¥{order.amount || '0.00'}
                        </td>
                        <td className="px-6 py-4 whitespace-nowrap">
                          <StatusBadge status={order.status || order.order_status || 'unknown'} />
                        </td>
                        <td className="px-6 py-4 text-right">
                          <a
                            href={`https://www.goofish.com/order-detail?orderId=${order.order_id}&role=seller`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex text-gray-400 hover:text-blue-600 p-2 rounded-xl hover:bg-blue-50 transition-colors"
                            title="查看闲鱼详情"
                          >
                            <ExternalLink className="w-4 h-4" />
                          </a>
                        </td>
                      </tr>
                    ))}
                </tbody>
              </table>
            )}
          </div>
        </div>

        {/* 商品金额分析 */}
        <div className="ios-card p-6 rounded-xl bg-white">
          <h3 className="font-bold text-lg text-gray-900 mb-6">商品金额分析 (TOP5)</h3>
          {categoryDataData.length === 0 ? (
            <div className="flex items-center justify-center h-[300px] text-gray-400">暂无数据</div>
          ) : (
            <>
              <div
                className="dashboard-pie-chart h-[280px] relative"
                role="img"
                aria-label={`商品金额分析，总金额 ${formatCurrency(totalAmount)}`}
              >
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart accessibilityLayer={false}>
                    <Pie
                      data={categoryDataData}
                      cx="50%"
                      cy="50%"
                      innerRadius={60}
                      outerRadius={92}
                      paddingAngle={2}
                      dataKey="value"
                      activeShape={{
                        outerRadius: 98,
                        stroke: 'none',
                        strokeWidth: 0,
                      }}
                      rootTabIndex={-1}
                      label={false}
                      labelLine={false}
                    >
                      {categoryDataData.map(/* 当前回调处理集合中的单个元素。 */ (entry, index) => (
                        <Cell key={`cell-${index}`} fill={entry.color || COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip
                      wrapperStyle={{ zIndex: 30, outline: 'none' }}
                      contentStyle={{
                        backgroundColor: cssColor('white'),
                        border: `1px solid ${cssColor('neutral-200')}`,
                        borderRadius: '6px',
                        boxShadow: 'var(--shadow-md)'
                      }}
                      formatter={/* 当前回调处理用户交互或异步状态变化。 */ (value) => `¥${Number(value || 0).toLocaleString()}`}
                    />
                  </PieChart>
                </ResponsiveContainer>
                <div className="pointer-events-none absolute inset-0 z-10 flex flex-col items-center justify-center">
                  <span className="text-lg font-extrabold text-gray-900 tabular-nums">{formatCurrency(totalAmount)}</span>
                  <span className="text-xs font-medium text-gray-400 mt-0.5">总金额</span>
                </div>
              </div>
              <div className="space-y-3 mt-4">
                {categoryDataData.map(/* 当前回调处理集合中的单个元素。 */ (cat) => (
                  <div key={cat.name} className="flex justify-between items-center gap-3 text-sm">
                    <div className="flex items-center gap-2 min-w-0 flex-1">
                      <div
                        className="w-3 h-3 rounded-full shrink-0"
                        style={{ backgroundColor: cat.color || COLORS[categoryDataData.indexOf(cat) % COLORS.length] }}
                      ></div>
                      <span className="text-gray-600 font-medium truncate" title={cat.name}>{cat.name}</span>
                    </div>
                    <div className="flex items-center gap-3 shrink-0 whitespace-nowrap">
                      <span className="font-bold text-gray-900">¥{cat.value.toLocaleString()}</span>
                      <span className="text-xs text-gray-500 bg-gray-100 px-2 py-0.5 rounded">{cat.percentage}%</span>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export default Dashboard;
