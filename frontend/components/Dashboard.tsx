import React, { useEffect, useRef, useState } from 'react';
import { DashboardStats, OrderAnalytics, Order, OrderStatus, Item } from '../types';
import { getDashboardStats, getOrderAnalytics, getValidOrders, getItems } from '../services/api';
import { getDateRange, getPreviousDateRange, TimeRange } from '../dateRange';
import { TrendingUp, Users, ShoppingCart, AlertCircle, DollarSign, Activity, Package, ArrowUpRight, Calendar, X, BarChart3, PackageCheck, ExternalLink, Eye, Edit } from 'lucide-react';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, BarChart, Bar, PieChart, Pie, Cell, Legend } from 'recharts';

// 状态徽章组件
const StatusBadge: React.FC<{ status: OrderStatus }> = ({ status }) => {
  const styles = {
    processing: 'bg-blue-100 text-blue-800',
    pending_ship: 'bg-[#0094f7] text-white',
    shipped: 'bg-blue-100 text-blue-700',
    completed: 'bg-green-100 text-green-700',
    cancelled: 'bg-gray-100 text-gray-500',
    refunding: 'bg-red-100 text-red-600',
    unknown: 'bg-gray-100 text-gray-500',
  };

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
    <span className={`px-3 py-1.5 rounded-lg text-xs font-bold ${styles[status] || styles.cancelled}`}>
      {labels[status] || status}
    </span>
  );
};

const StatCard: React.FC<{ title: string; value: string | number; icon: React.ElementType; colorClass: string; trend?: string }> = ({ title, value, icon: Icon, colorClass, trend }) => (
  <div className="ios-card p-6 rounded-xl flex flex-col justify-between hover:translate-y-[-4px] transition-all duration-300 h-full relative overflow-hidden group border-0">
    <div className={`absolute -right-6 -top-6 w-32 h-32 ${colorClass} opacity-10 rounded-full group-hover:scale-125 transition-transform duration-500 blur-2xl`}></div>
    <div className="flex justify-between items-start mb-6">
      <div className={`p-4 rounded-2xl ${colorClass} bg-opacity-10 backdrop-blur-sm`}>
        <Icon className={`w-6 h-6 ${colorClass.replace('bg-', 'text-')}`} />
      </div>
      {trend && <span className="text-xs font-bold text-white bg-[#0094f7] px-3 py-1.5 rounded-full flex items-center gap-1 shadow-sm">
        <TrendingUp className="w-3 h-3" /> {trend}
      </span>}
    </div>
    <div className="relative z-10">
      <h3 className="text-3xl font-extrabold text-gray-900 tracking-tight font-feature-settings-tnum">{value}</h3>
      <p className="text-gray-500 text-sm font-medium mt-1">{title}</p>
    </div>
  </div>
);

const Dashboard: React.FC = () => {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [analytics, setAnalytics] = useState<OrderAnalytics | null>(null);
  const [loadError, setLoadError] = useState('');
  const [timeRange, setTimeRange] = useState<TimeRange>('7days');
  const [customStartDate, setCustomStartDate] = useState('');
  const [customEndDate, setCustomEndDate] = useState('');
  const [previousAnalytics, setPreviousAnalytics] = useState<OrderAnalytics | null>(null); // 用于计算趋势

  // 搜索词
  const [searchTerm, setSearchTerm] = useState('');
  // 参与统计的订单列表
  const [validOrders, setValidOrders] = useState<Order[]>([]);
	const [validOrdersTotal, setValidOrdersTotal] = useState(0);
	const [validOrdersTruncated, setValidOrdersTruncated] = useState(false);
  const [ordersLoading, setOrdersLoading] = useState(false);
  // 商品列表
  const [items, setItems] = useState<Item[]>([]);
  const [itemNames, setItemNames] = useState<Record<string, string>>({});
  const rangeRequestSequence = useRef(0);

  // 颜色配置
  const COLORS = ['#0094f7', '#3B82F6', '#10B981', '#F59E0B', '#8B5CF6'];
  const formatCurrency = (value: number) => `¥${Number(value || 0).toLocaleString('zh-CN', { maximumFractionDigits: 2 })}`;
  const loadRange = async (range: TimeRange) => {
    let currentRange;
    try {
      currentRange = getDateRange(range, new Date(), customStartDate, customEndDate);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : '日期范围无效');
      return;
    }
    const { startDate, endDate } = currentRange;
    const params = { start_date: startDate, end_date: endDate };
    const previous = getPreviousDateRange(currentRange);
    const sequence = ++rangeRequestSequence.current;
    setOrdersLoading(true);
    setLoadError('');
    try {
      const [currentAnalytics, previousResult, orders] = await Promise.all([
        getOrderAnalytics(params),
        getOrderAnalytics({ start_date: previous.startDate, end_date: previous.endDate }),
        getValidOrders(params),
      ]);
      if (sequence !== rangeRequestSequence.current) return;
      setAnalytics(currentAnalytics);
      setPreviousAnalytics(previousResult);
		setValidOrders(orders.orders);
		setValidOrdersTotal(orders.total);
		setValidOrdersTruncated(orders.truncated);
    } catch (error) {
      if (sequence === rangeRequestSequence.current) {
        setLoadError(error instanceof Error ? error.message : '经营数据加载失败');
      }
    } finally {
      if (sequence === rangeRequestSequence.current) setOrdersLoading(false);
    }
  };

  // 计算趋势百分比
  const getTrendPercent = () => {
    if (!analytics || !previousAnalytics) return null;

    const currentAmount = analytics.revenue_stats.total_amount;
    const previousAmount = previousAnalytics.revenue_stats.total_amount;

    if (previousAmount === 0) {
      return currentAmount > 0 ? '+100%' : '0%';
    }

    const percent = ((currentAmount - previousAmount) / previousAmount) * 100;
    const sign = percent >= 0 ? '+' : '';
    return `${sign}${percent.toFixed(1)}%`;
  };

  useEffect(() => {
    let cancelled = false;
    Promise.all([getDashboardStats(), getItems()]).then(([dashboardStats, itemList]) => {
      if (cancelled) return;
      setStats(dashboardStats);
      setItems(itemList);
      // 建立 item_id 到 item_title 的映射
      const nameMap: Record<string, string> = {};
      itemList.forEach(item => {
        nameMap[item.item_id] = item.item_title || item.item_id;
      });
      setItemNames(nameMap);
    }).catch(error => {
      if (!cancelled) setLoadError(error instanceof Error ? error.message : '概览加载失败');
    });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    if (timeRange !== 'custom') void loadRange(timeRange);
  }, [timeRange]);

  if (loadError && (!stats || !analytics)) {
    return (
      <div className="p-8 flex flex-col items-center gap-3 text-red-600">
        <AlertCircle className="w-8 h-8" />
        <span>{loadError}</span>
        <button type="button" className="ios-btn-primary px-4 py-2 rounded-xl" onClick={() => window.location.reload()}>重新加载</button>
      </div>
    );
  }
  if (!stats || !analytics) return <div className="p-8 flex justify-center text-gray-400"><Activity className="w-8 h-8 animate-spin text-[#0094f7]" /></div>;

  const chartData = analytics.daily_stats?.map(d => ({
      name: d.date.slice(5), // MM-DD
      amount: d.amount,
      orders: d.order_count,
      avgAmount: d.order_count > 0 ? (d.amount / d.order_count).toFixed(2) : 0
  })) || [];

  // 计算图表数据（在渲染时直接计算）
  const itemStats = analytics.item_stats || [];
  const totalOrders = analytics.revenue_stats.total_orders || 0;
  const totalAmount = analytics.revenue_stats.total_amount || 0;

  // 1. 商品销量排行：按订单数量排序
  const productSalesData = itemStats.length > 0 ? itemStats
    .map(item => ({
      name: (itemNames[item.item_id] || item.item_id).length > 12
        ? (itemNames[item.item_id] || item.item_id).substring(0, 12) + '...'
        : (itemNames[item.item_id] || item.item_id),
      sales: item.order_count
    }))
    .sort((a, b) => b.sales - a.sales)
    .slice(0, 10) : [];
  const maxProductSales = Math.max(...productSalesData.map(item => item.sales), 1);

  // 2. 商品下单占比：每个商品的订单数占总订单数的百分比
  const sourceDataData = itemStats.length > 0 ? itemStats
    .map(item => ({
      name: (itemNames[item.item_id] || item.item_id).length > 10
        ? (itemNames[item.item_id] || item.item_id).substring(0, 10) + '...'
        : (itemNames[item.item_id] || item.item_id),
      value: item.order_count,
      percent: totalOrders > 0 ? (item.order_count / totalOrders) * 100 : 0
    }))
    .sort((a, b) => b.value - a.value)
    .slice(0, 6)
    .map((item, index) => ({
      ...item,
      color: COLORS[index % COLORS.length]
    })) : [];

  // 3. 商品金额分析：按金额排序，取前5
  const categoryDataData = itemStats.length > 0 ? itemStats
    .map(item => ({
      name: (itemNames[item.item_id] || item.item_id).length > 12
        ? (itemNames[item.item_id] || item.item_id).substring(0, 12) + '...'
        : (itemNames[item.item_id] || item.item_id),
      value: item.total_amount,
      orderCount: item.order_count,
      percentage: totalAmount > 0
        ? (item.total_amount / totalAmount) * 100
        : 0
    }))
    .sort((a, b) => b.value - a.value)
    .slice(0, 5)
    .map((item, index) => ({
      ...item,
      color: COLORS[index % COLORS.length],
      percentage: item.percentage.toFixed(1)
    })) : [];

  const timeRangeOptions = [
    { key: 'today' as TimeRange, label: '今天' },
    { key: 'yesterday' as TimeRange, label: '昨天' },
    { key: '3days' as TimeRange, label: '三天内' },
    { key: '7days' as TimeRange, label: '7天内' },
    { key: '30days' as TimeRange, label: '一个月内' },
    { key: 'custom' as TimeRange, label: '自定义' },
  ];
  const selectedRangeLabel = timeRangeOptions.find(option => option.key === timeRange)?.label || '所选范围';
  let currentRangeDates;
  try {
    currentRangeDates = getDateRange(timeRange, new Date(), customStartDate, customEndDate);
  } catch {
    currentRangeDates = { startDate: customStartDate, endDate: customEndDate };
  }
  const normalizedSearchTerm = searchTerm.trim().toLowerCase();
  const filteredValidOrders = validOrders.filter((order) =>
    order.order_id?.toLowerCase().includes(normalizedSearchTerm) ||
    order.item_id?.toLowerCase().includes(normalizedSearchTerm) ||
    order.item_title?.toLowerCase().includes(normalizedSearchTerm) ||
    order.buyer_id?.toLowerCase().includes(normalizedSearchTerm)
  );

  return (
    <div className="space-y-8 animate-fade-in">
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
        {timeRangeOptions.map((option) => (
          <button
            key={option.key}
            onClick={() => setTimeRange(option.key)}
            className={`px-5 py-2.5 rounded-xl text-sm font-bold transition-all ${
              timeRange === option.key
                ? 'bg-[#0094f7] text-white shadow-md'
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
              onChange={(e) => setCustomStartDate(e.target.value)}
              className="px-3 py-2 rounded-xl text-sm border border-gray-200 focus:outline-none focus:ring-2 focus:ring-[#0094f7]"
            />
            <span className="self-center text-gray-400">-</span>
            <input
              type="date"
              value={customEndDate}
              onChange={(e) => setCustomEndDate(e.target.value)}
              className="px-3 py-2 rounded-xl text-sm border border-gray-200 focus:outline-none focus:ring-2 focus:ring-[#0094f7]"
            />
            <button
              onClick={() => {
                try {
                  setLoadError('');
                  void loadRange('custom');
                } catch (error) {
                  setLoadError(error instanceof Error ? error.message : '日期范围无效');
                }
              }}
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
          trend={getTrendPercent() || undefined}
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

      {/* Main Chart Section */}
      <div className="ios-card p-8 rounded-xl">
        <div className="mb-10">
          <h3 className="text-xl font-bold text-gray-900">营收趋势分析</h3>
          <p className="text-sm text-gray-400 mt-1">{selectedRangeLabel}的销售额走势</p>
        </div>
        <div className="h-[350px] w-full">
          {chartData.length === 0 || analytics.revenue_stats.total_amount === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-gray-400">
              <ShoppingCart className="w-16 h-16 mb-4 opacity-20" />
              <p className="text-lg font-medium">暂无营收数据</p>
              <p className="text-sm mt-2">所选时间范围内暂无订单记录</p>
            </div>
          ) : chartData.length <= 2 ? (
            // 数据点少于等于2个时使用美化柱状图
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={chartData} margin={{ top: 30, right: 20, left: 0, bottom: 30 }} barCategoryGap="45%">
                <XAxis
                  dataKey="name"
                  axisLine={false}
                  tickLine={false}
                  tick={{fill: '#374151', fontSize: 14, fontWeight: 600}}
                  dy={10}
                />
                <YAxis
                  axisLine={false}
                  tickLine={false}
                  tick={{fill: '#9CA3AF', fontSize: 13, fontWeight: 500}}
                  tickFormatter={(value) => `¥${value}`}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: '#fff',
                    borderRadius: '8px',
                    border: '1px solid #e5e7eb',
                    boxShadow: '0 10px 25px -5px rgba(0, 0, 0, 0.1)',
                    padding: '12px 16px'
                  }}
                  labelStyle={{ color: '#6b7280', fontWeight: 500 }}
                  itemStyle={{ color: '#0094f7', fontWeight: 600 }}
                  cursor={{ fill: 'rgba(0, 148, 247, 0.08)' }}
                  formatter={(value) => {
                    const num = Number(value);
                    return [`¥${num.toFixed(2)}`, '营收'];
                  }}
                />
                <Bar
                  dataKey="amount"
                  fill="#0094f7"
                  maxBarSize={72}
                  radius={[12, 12, 0, 0]}
                  stroke="#000000"
                  strokeWidth={0}
                >
                  {chartData.map((entry, index) => (
                    <Cell key={`cell-${index}`} fill={['#0094f7', '#FCD34D', '#0071e3'][index % 3]} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          ) : (
            // 数据点多于2个时使用折线图
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                <defs>
                  <linearGradient id="colorAmount" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#0094f7" stopOpacity={0.5}/>
                    <stop offset="95%" stopColor="#0094f7" stopOpacity={0}/>
                  </linearGradient>
                </defs>
                <XAxis
                  dataKey="name"
                  axisLine={false}
                  tickLine={false}
                  tick={{fill: '#9CA3AF', fontSize: 13, fontWeight: 500}}
                  dy={15}
                />
                <YAxis
                  axisLine={false}
                  tickLine={false}
                  tick={{fill: '#9CA3AF', fontSize: 13, fontWeight: 500}}
                />
                <CartesianGrid vertical={false} stroke="#F3F4F6" strokeDasharray="3 3" />
                <Tooltip
                  contentStyle={{
                    backgroundColor: '#fff',
                    borderRadius: '8px',
                    border: '1px solid #e5e7eb',
                    boxShadow: '0 10px 25px -5px rgba(0, 0, 0, 0.1)',
                    padding: '12px 16px'
                  }}
                  labelStyle={{ color: '#6b7280', fontWeight: 500 }}
                  itemStyle={{ color: '#0094f7', fontWeight: 600 }}
                  cursor={{ stroke: '#0094f7', strokeWidth: 2, strokeDasharray: '4 4' }}
                />
                <Area type="monotone" dataKey="amount" stroke="#0094f7" strokeWidth={4} fillOpacity={1} fill="url(#colorAmount)" activeDot={{ r: 8, fill: '#fff', stroke: "#0094f7", strokeWidth: 2 }} />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>

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
                {productSalesData.map((item, index) => (
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
                        className="h-full rounded-full bg-[#0094f7]"
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
          <div className="h-[280px]">
            {sourceDataData.length === 0 ? (
              <div className="flex items-center justify-center h-full text-gray-400">暂无数据</div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={sourceDataData}
                    cx="50%"
                    cy="50%"
                    innerRadius={60}
                    outerRadius={90}
                    paddingAngle={2}
                    dataKey="value"
                    label={({ name, payload }) => `${name} ${Number(payload?.percent || 0).toFixed(1)}%`}
                    labelLine={false}
                  >
                    {sourceDataData.map((entry, index) => (
                      <Cell key={`cell-${index}`} fill={entry.color} />
                    ))}
                  </Pie>
                  <Legend
                    verticalAlign="bottom"
                    height={36}
                    iconType="circle"
                    formatter={(value) => <span style={{ color: '#6B7280', fontWeight: 500 }}>{value}</span>}
                  />
                </PieChart>
              </ResponsiveContainer>
            )}
          </div>
        </div>
      </div>

      {/* 收支明细和品类营收 */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* 参与统计的订单列表 */}
        <div className="lg:col-span-2 ios-card p-0 rounded-xl border-0 bg-white overflow-hidden flex flex-col">
          <div className="p-6 border-b border-gray-50 flex justify-between items-center bg-[#FAFAFA]">
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
                onChange={(e) => setSearchTerm(e.target.value)}
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
              <table className="w-full text-left border-collapse">
                <thead>
                  <tr className="bg-white text-gray-400 text-xs font-bold uppercase tracking-wider border-b border-gray-50">
                    <th className="px-6 py-4">订单信息</th>
                    <th className="px-6 py-4">买家信息</th>
                    <th className="px-6 py-4">金额</th>
                    <th className="px-6 py-4">状态</th>
                    <th className="px-6 py-4 text-right">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-50">
                  {filteredValidOrders.map((order) => (
                      <tr key={order.order_id} className="hover:bg-[#FFFDE7]/50 transition-colors group">
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
                            <div className="text-xs text-gray-400 mt-1">{order.created_at}</div>
                          )}
                        </td>
                        <td className="px-6 py-4 text-base font-extrabold text-gray-900 font-feature-settings-tnum">
                          ¥{order.amount || '0.00'}
                        </td>
                        <td className="px-6 py-4">
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
              <div className="h-[300px] relative">
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie
                      data={categoryDataData}
                      cx="50%"
                      cy="50%"
                      innerRadius={60}
                      outerRadius={100}
                      paddingAngle={2}
                      dataKey="value"
                      label={({ name, payload }) => `${name} ${payload?.percentage || '0.0'}%`}
                    >
                      {categoryDataData.map((entry, index) => (
                        <Cell key={`cell-${index}`} fill={entry.color || COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip
                      contentStyle={{
                        backgroundColor: '#fff',
                        border: '1px solid #e5e7eb',
                        borderRadius: '6px',
                        boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.1)'
                      }}
                      formatter={(value) => `¥${Number(value || 0).toLocaleString()}`}
                    />
                  </PieChart>
                </ResponsiveContainer>
              </div>
              <div className="space-y-3 mt-4">
                {categoryDataData.map((cat) => (
                  <div key={cat.name} className="flex justify-between items-center text-sm">
                    <div className="flex items-center gap-2">
                      <div
                        className="w-3 h-3 rounded-full"
                        style={{ backgroundColor: cat.color || COLORS[categoryDataData.indexOf(cat) % COLORS.length] }}
                      ></div>
                      <span className="text-gray-600 font-medium">{cat.name}</span>
                    </div>
                    <div className="flex items-center gap-3">
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
