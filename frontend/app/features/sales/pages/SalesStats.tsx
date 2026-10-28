import { Activity,AlertCircle,DollarSign,PackageCheck,RotateCcw,TrendingUp } from 'lucide-react';
import React,{ useState } from 'react';
import { Area,AreaChart,CartesianGrid,ResponsiveContainer,Tooltip,XAxis,YAxis } from 'recharts';
import { TimeRange, validateCustomDateRange } from '../../../../dateRange';
import { useSalesStats } from '../hooks';
import { PageHeader } from '../../../../shared/ui/PageHeader';

/** 读取设计系统颜色变量。 */
const cssColor = (token: string, alpha?: number) => (
  alpha === undefined
    ? `rgb(var(--color-${token}))`
    : `rgb(var(--color-${token}) / ${alpha})`
);

/** StatCard 渲染销售统计卡片。 */
const StatCard: React.FC<{ /** title 表示卡片标题。 */ title: string; /** value 表示金额文本。 */ value: string; /** count 表示订单数文本。 */ count: string; /** icon 表示图标组件。 */ icon: React.ElementType; /** colorClass 表示主题色类名。 */ colorClass: string }> = ({ title, value, count, icon: Icon, colorClass }) => (
  <div className="ios-card p-6 rounded-xl flex flex-col justify-between hover:translate-y-[-4px] transition-all duration-300 h-full relative overflow-hidden group border-0">
    <div className={`absolute -right-6 -top-6 w-32 h-32 ${colorClass} opacity-10 rounded-full group-hover:scale-125 transition-transform duration-500 blur-2xl`}></div>
    <div className="flex justify-between items-start mb-6">
      <div className={`p-4 rounded-2xl ${colorClass} bg-opacity-10 backdrop-blur-sm`}>
        <Icon className={`w-6 h-6 ${colorClass.replace('bg-', 'text-')}`} />
      </div>
    </div>
    <div className="relative z-10">
      <h3 className="text-3xl font-extrabold text-gray-900 tracking-tight font-feature-settings-tnum">{value}</h3>
      <p className="text-gray-500 text-sm font-medium mt-1">{title}</p>
      <p className="text-gray-400 text-xs font-medium mt-0.5">{count}</p>
    </div>
  </div>
);

/** SalesStats 渲染销售统计页面。 */
const SalesStats: React.FC = () => {
  // [timeRange, setTimeRange] 保存用户当前选中的范围标签，自定义标签在点击“应用”前不会改变已生效范围。
  const [timeRange, setTimeRange] = useState<TimeRange>('7days');
  // [appliedRange, setAppliedRange] 保存真正用于请求的已生效范围，预设立即生效，自定义经校验后才生效。
  const [appliedRange, setAppliedRange] = useState<{ /** range 是已生效的范围类型。 */ range: TimeRange; /** startDate 是已生效的自定义起始日期。 */ startDate: string; /** endDate 是已生效的自定义结束日期。 */ endDate: string }>({ range: '7days', startDate: '', endDate: '' });
  // [draftStartDate, setDraftStartDate] 保存自定义起始日期草稿，编辑时不触发请求。
  const [draftStartDate, setDraftStartDate] = useState('');
  // [draftEndDate, setDraftEndDate] 保存自定义结束日期草稿，编辑时不触发请求。
  const [draftEndDate, setDraftEndDate] = useState('');
  // [draftError, setDraftError] 保存自定义日期草稿的行内校验错误，不影响已加载的数据。
  const [draftError, setDraftError] = useState('');
  // sales 保存销售统计数据与加载状态。
  const sales = useSalesStats({ range: appliedRange.range, customStartDate: appliedRange.startDate, customEndDate: appliedRange.endDate });
  // { 解构得到当前 Hook 返回的状态和操作函数。
  const { data, loading, error, refresh } = sales;

  // selectRange 处理范围标签点击：预设范围立即生效，自定义标签只切换输入区。
  const selectRange = (/* key 是被点击的范围标签。 */ key: TimeRange) => {
    setTimeRange(key);
    setDraftError('');
    if (key !== 'custom') setAppliedRange({ range: key, startDate: '', endDate: '' });
  };
  // applyCustomRange 校验并应用自定义日期；校验失败只提示，不清空已加载数据；范围未变时重新加载。
  const applyCustomRange = () => {
    // message 是自定义日期校验结果，空字符串表示合法。
    const message = validateCustomDateRange(draftStartDate, draftEndDate);
    setDraftError(message);
    if (message) return;
    if (appliedRange.range === 'custom' && appliedRange.startDate === draftStartDate && appliedRange.endDate === draftEndDate) {
      refresh();
      return;
    }
    setAppliedRange({ range: 'custom', startDate: draftStartDate, endDate: draftEndDate });
  };

  // timeRangeOptions 是预设时间范围选项。
  const timeRangeOptions = [
    { key: 'today' as TimeRange, label: '今天' },
    { key: 'yesterday' as TimeRange, label: '昨天' },
    { key: '3days' as TimeRange, label: '三天内' },
    { key: '7days' as TimeRange, label: '7天内' },
    { key: '30days' as TimeRange, label: '一个月内' },
    { key: 'custom' as TimeRange, label: '自定义' },
  ];
  // formatCurrency 格式化金额函数。
  const formatCurrency = (value: number) => `¥${Number(value || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

  // chartData 是按日聚合的趋势图数据点。
  const chartData = (data?.daily ?? []).map(/* day 是服务端按日聚合的销售数据点。 */ day => ({
    name: day.date.slice(5),
    销售额: day.salesAmount,
    已收货款: day.receivedAmount,
    退货货款: day.refundedAmount,
  }));

  return (
    <div className="min-w-0 space-y-8 animate-fade-in">
      {error && data && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" className="font-bold underline" onClick={refresh}>重试</button>
        </div>
      )}
      <PageHeader title="销售统计" description="按订单创建时间统计一段时间内的销售额、已收货款与退货货款。" />

      {/* 时间范围选择 */}
      <div className="flex flex-wrap gap-2 p-2 bg-gray-100/50 rounded-2xl">
        {timeRangeOptions.map(/* 当前回调处理集合中的单个元素。 */ option => (
          <button
            key={option.key}
            onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => selectRange(option.key)}
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
              value={draftStartDate}
              onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setDraftStartDate(e.target.value)}
              className="px-3 py-2 rounded-xl text-sm border border-gray-200 focus:outline-none focus:ring-2 focus:ring-brand"
            />
            <span className="self-center text-gray-400">-</span>
            <input
              type="date"
              value={draftEndDate}
              onChange={/* 当前回调处理用户交互或异步状态变化。 */ e => setDraftEndDate(e.target.value)}
              className="px-3 py-2 rounded-xl text-sm border border-gray-200 focus:outline-none focus:ring-2 focus:ring-brand"
            />
            <button
              onClick={applyCustomRange}
              className="px-4 py-2 rounded-xl text-sm font-bold bg-black text-white hover:bg-gray-800 transition-colors"
            >
              应用
            </button>
            {draftError && <span role="alert" className="self-center text-sm text-red-600">{draftError}</span>}
          </>
        )}
      </div>

      {!data && (
        error ? (
          <div className="p-8 flex flex-col items-center gap-3 text-red-600">
            <AlertCircle className="w-8 h-8" />
            <span>{error}</span>
            <button type="button" className="ios-btn-primary px-4 py-2 rounded-xl" onClick={refresh}>重新加载</button>
          </div>
        ) : (
          <div className="p-8 flex justify-center text-gray-400"><Activity className="w-8 h-8 animate-spin text-brand" /></div>
        )
      )}
      {data && (<>

      {/* 三项核心指标 */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <StatCard
          title="销售额 (CNY)"
          value={formatCurrency(data.salesAmount)}
          count={`${data.salesOrders} 单（已发货 + 已完成）`}
          icon={TrendingUp}
          colorClass="bg-blue-500"
        />
        <StatCard
          title="已收货款 (CNY)"
          value={formatCurrency(data.receivedAmount)}
          count={`${data.receivedOrders} 单（已完成）`}
          icon={PackageCheck}
          colorClass="bg-green-500"
        />
        <StatCard
          title="退货货款 (CNY)"
          value={formatCurrency(data.refundedAmount)}
          count={`${data.refundedOrders} 单（已取消/退款）`}
          icon={RotateCcw}
          colorClass="bg-red-500"
        />
      </div>

      {/* 按日趋势 */}
      <div className="ios-card p-8 rounded-xl">
        <div className="mb-10">
          <h3 className="text-xl font-bold text-gray-900">按日趋势</h3>
          <p className="text-sm text-gray-400 mt-1">所选时间范围内的销售额、已收货款与退货货款走势</p>
        </div>
        <div className="h-[350px] w-full">
          {loading ? (
            <div className="h-full flex items-center justify-center text-gray-400">
              <Activity className="w-6 h-6 animate-spin mr-2" />
              加载中...
            </div>
          ) : chartData.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-gray-400">
              <DollarSign className="w-16 h-16 mb-4 opacity-20" />
              <p className="text-lg font-medium">暂无销售数据</p>
              <p className="text-sm mt-2">所选时间范围内没有已发货、已完成或已取消的订单</p>
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                <defs>
                  <linearGradient id="colorSales" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={cssColor('brand')} stopOpacity={0.5} />
                    <stop offset="95%" stopColor={cssColor('brand')} stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="colorReceived" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={cssColor('success-500')} stopOpacity={0.5} />
                    <stop offset="95%" stopColor={cssColor('success-500')} stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="colorRefunded" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={cssColor('danger-500')} stopOpacity={0.5} />
                    <stop offset="95%" stopColor={cssColor('danger-500')} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis dataKey="name" axisLine={false} tickLine={false} tick={{ fill: cssColor('neutral-400'), fontSize: 13, fontWeight: 500 }} dy={15} />
                <YAxis axisLine={false} tickLine={false} tick={{ fill: cssColor('neutral-400'), fontSize: 13, fontWeight: 500 }} />
                <CartesianGrid vertical={false} stroke={cssColor('neutral-100')} strokeDasharray="3 3" />
                <Tooltip
                  contentStyle={{ backgroundColor: cssColor('white'), borderRadius: '8px', border: `1px solid ${cssColor('neutral-200')}`, boxShadow: 'var(--shadow-xl)', padding: '12px 16px' }}
                  labelStyle={{ color: cssColor('neutral-500'), fontWeight: 500 }}
                  itemStyle={{ color: cssColor('brand'), fontWeight: 600 }}
                  cursor={{ stroke: cssColor('brand'), strokeWidth: 2, strokeDasharray: '4 4' }}
                  formatter={/* value 是提示框当前数据点的原始金额。 */ value => [`¥${Number(value).toFixed(2)}`, '']}
                />
                <Area type="monotone" dataKey="销售额" stroke={cssColor('brand')} strokeWidth={3} fillOpacity={1} fill="url(#colorSales)" activeDot={{ r: 6 }} />
                <Area type="monotone" dataKey="已收货款" stroke={cssColor('success-500')} strokeWidth={3} fillOpacity={1} fill="url(#colorReceived)" activeDot={{ r: 6 }} />
                <Area type="monotone" dataKey="退货货款" stroke={cssColor('danger-500')} strokeWidth={3} fillOpacity={1} fill="url(#colorRefunded)" activeDot={{ r: 6 }} />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>

      {/* 按日明细 */}
      <div className="ios-card p-0 rounded-xl border-0 bg-white overflow-hidden">
        <div className="p-6 border-b border-gray-50 bg-surface-muted">
          <h3 className="font-bold text-lg text-gray-900">按日明细</h3>
        </div>
        <div className="overflow-x-auto max-h-[400px]">
          {data.daily.length === 0 ? (
            <div className="flex items-center justify-center py-20 text-gray-400">暂无数据</div>
          ) : (
            <table className="w-full min-w-[640px] text-left border-collapse">
              <thead>
                <tr className="bg-white text-gray-400 text-xs font-bold uppercase tracking-wider border-b border-gray-50">
                  <th className="px-6 py-4">日期</th>
                  <th className="px-6 py-4 text-right">订单数</th>
                  <th className="px-6 py-4 text-right">销售额</th>
                  <th className="px-6 py-4 text-right">已收货款</th>
                  <th className="px-6 py-4 text-right">退货货款</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-50">
                {data.daily.map(/* 当前回调处理集合中的单个元素。 */ day => (
                  <tr key={day.date} className="hover:bg-gray-50/50 transition-colors">
                    <td className="px-6 py-4 text-sm font-bold text-gray-900">{day.date}</td>
                    <td className="px-6 py-4 text-sm text-right text-gray-700">{day.orderCount}</td>
                    <td className="px-6 py-4 text-sm text-right font-extrabold text-gray-900">{formatCurrency(day.salesAmount)}</td>
                    <td className="px-6 py-4 text-sm text-right text-green-600 font-bold">{formatCurrency(day.receivedAmount)}</td>
                    <td className="px-6 py-4 text-sm text-right text-red-600 font-bold">{formatCurrency(day.refundedAmount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
      </>)}
    </div>
  );
};

export default SalesStats;
