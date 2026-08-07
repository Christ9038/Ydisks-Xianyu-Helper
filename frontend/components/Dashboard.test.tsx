import React from 'react';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, test } from 'vitest';
import { StatusBadge } from './Dashboard';

const dashboardSource = readFileSync(resolve(__dirname, 'Dashboard.tsx'), 'utf8');
const globalStyles = readFileSync(resolve(__dirname, '../index.css'), 'utf8');

describe('Dashboard presentation safeguards', () => {
  test('keeps order status badges on one line', () => {
    const html = renderToStaticMarkup(<StatusBadge status="shipped" />);

    expect(html).toContain('已发货');
    expect(html).toContain('inline-flex');
    expect(html).toContain('whitespace-nowrap');
    expect(dashboardSource).toContain('min-w-[760px]');
  });

  test('pie charts use enlarged active sectors without focus rings or external label lines', () => {
    expect(dashboardSource.match(/accessibilityLayer=\{false\}/g)).toHaveLength(2);
    expect(dashboardSource.match(/activeShape=\{\{/g)).toHaveLength(2);
    expect(dashboardSource).toContain('outerRadius: 96');
    expect(dashboardSource).toContain('outerRadius: 98');
    expect(dashboardSource.match(/stroke: 'none'/g)).toHaveLength(2);
    expect(dashboardSource.match(/strokeWidth: 0/g)).toHaveLength(2);
    expect(dashboardSource.match(/rootTabIndex=\{-1\}/g)).toHaveLength(2);
    expect(dashboardSource.match(/label=\{false\}/g)).toHaveLength(2);
    expect(dashboardSource.match(/labelLine=\{false\}/g)).toHaveLength(2);
    expect(dashboardSource.match(/wrapperStyle=\{\{ zIndex: 30, outline: 'none' \}\}/g)).toHaveLength(2);
    expect(dashboardSource.match(/absolute inset-0 z-10/g)).toHaveLength(2);
    expect(globalStyles).toContain('.dashboard-pie-chart .recharts-sector:focus');
    expect(dashboardSource).toContain('dashboard-revenue-chart h-[350px] w-full');
    expect(dashboardSource).toContain('activeBar={false}');
    expect(dashboardSource).not.toContain('stroke="#000000"');
    expect(dashboardSource).not.toContain("['#0094f7', '#FCD34D', '#0071e3']");
    expect(dashboardSource).toContain('fill="#0094f7"');
    expect(globalStyles).toContain('.dashboard-revenue-chart .recharts-rectangle:focus');
    expect(globalStyles).toContain('.dashboard-revenue-chart .recharts-curve:focus');
    expect(globalStyles).toContain('.dashboard-revenue-chart [class*="recharts-zIndex-layer_"]:focus');
    expect(globalStyles).toContain('stroke: none !important');
  });
});
