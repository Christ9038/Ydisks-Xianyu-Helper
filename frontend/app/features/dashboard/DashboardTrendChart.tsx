import { ShoppingCart } from 'lucide-react';
import React from 'react';
import { Area,AreaChart,Bar,BarChart,CartesianGrid,Cell,ResponsiveContainer,Tooltip,XAxis,YAxis } from 'recharts';
import type { DashboardChartPoint } from './state';

/** 趋势图组件的输入参数。 */
export type DashboardTrendChartProps = {
  /** 趋势图的日期与营收数据点。 */
  chartData: DashboardChartPoint[];
  /** 当前选择范围的中文名称。 */
  selectedRangeLabel: string;
  /** 当前范围的营收总额。 */
  totalAmount: number;
};

/** 读取设计系统颜色变量。 */
const cssColor = (token: string, alpha?: number): string => (
  alpha === undefined ? `rgb(var(--color-${token}))` : `rgb(var(--color-${token}) / ${alpha})`
);
/** 展示 Dashboard 营收趋势，并根据数据点数量选择柱状图或面积图。 */
export const DashboardTrendChart: React.FC<DashboardTrendChartProps> = ({ chartData, selectedRangeLabel, totalAmount }) => (
  <div className="ios-card p-8 rounded-xl">
    <div className="mb-10">
      <h3 className="text-xl font-bold text-gray-900">营收趋势分析</h3>
      <p className="text-sm text-gray-400 mt-1">{selectedRangeLabel}的销售额走势</p>
    </div>
    <div className="dashboard-revenue-chart h-[350px] w-full">
      {chartData.length === 0 || totalAmount === 0 ? (
        <div className="h-full flex flex-col items-center justify-center text-gray-400">
          <ShoppingCart className="w-16 h-16 mb-4 opacity-20" />
          <p className="text-lg font-medium">暂无营收数据</p>
          <p className="text-sm mt-2">所选时间范围内暂无订单记录</p>
        </div>
      ) : chartData.length <= 2 ? (
        // 数据点少于等于2个时使用美化柱状图。
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={chartData} margin={{ top: 30, right: 20, left: 0, bottom: 30 }} barCategoryGap="45%">
            <XAxis dataKey="name" axisLine={false} tickLine={false} tick={{ fill: cssColor('neutral-700'), fontSize: 14, fontWeight: 600 }} dy={10} />
            <YAxis axisLine={false} tickLine={false} tick={{ fill: cssColor('neutral-400'), fontSize: 13, fontWeight: 500 }} tickFormatter={
              // value 是纵轴刻度的原始金额。
              value => `¥${value}`
            } />
            <Tooltip
              contentStyle={{ backgroundColor: cssColor('white'), borderRadius: '8px', border: `1px solid ${cssColor('neutral-200')}`, boxShadow: 'var(--shadow-xl)', padding: '12px 16px' }}
              labelStyle={{ color: cssColor('neutral-500'), fontWeight: 500 }}
              itemStyle={{ color: cssColor('brand'), fontWeight: 600 }}
              cursor={{ fill: cssColor('brand', 0.08) }}
              formatter={
                // value 是提示框当前数据点的原始金额。
                value => [`¥${Number(value).toFixed(2)}`, '营收']
              }
            />
            <Bar dataKey="amount" fill={cssColor('brand')} maxBarSize={72} radius={[12, 12, 0, 0]} activeBar={false} stroke="none" strokeWidth={0}>
              {chartData.map(
                // index 用于生成稳定的图表扇区键。
                (_, index) => <Cell key={`cell-${index}`} fill={cssColor('brand')} stroke="none" strokeWidth={0} />,
              )}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      ) : (
        // 数据点多于2个时使用面积图。
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
            <defs>
              <linearGradient id="colorAmount" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={cssColor('brand')} stopOpacity={0.5} />
                <stop offset="95%" stopColor={cssColor('brand')} stopOpacity={0} />
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
            />
            <Area type="monotone" dataKey="amount" stroke={cssColor('brand')} strokeWidth={4} fillOpacity={1} fill="url(#colorAmount)" activeDot={{ r: 8, fill: cssColor('white'), stroke: cssColor('brand'), strokeWidth: 2 }} />
          </AreaChart>
        </ResponsiveContainer>
      )}
    </div>
  </div>
);
