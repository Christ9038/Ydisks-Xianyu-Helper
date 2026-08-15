import ReactDOMServer from 'react-dom/server';
import { describe, expect, test } from 'vitest';
import { CardIcon } from './cards/components/CardIcon';
import { BatchPhaseIndicator } from './items/components/BatchPhaseIndicator';
import { NotificationEventSelector } from './notifications/components/NotificationEventSelector';
import { OrderFilterBar } from './orders/components/OrderFilterBar';

// render 将 React 展示组件转换为静态 HTML，验证不依赖浏览器 DOM 的渲染分支。
const render = (element: React.ReactElement): string => ReactDOMServer.renderToStaticMarkup(element);

// noopEventToggle 是静态渲染测试使用的通知事件回调占位实现。
const noopEventToggle = (): void => undefined;

// noopFilterChange 是静态渲染测试使用的订单状态回调占位实现。
const noopFilterChange = (): void => undefined;

// resolveAccountName 是静态渲染测试使用的账号展示名称解析器。
const resolveAccountName = (id: string): string => id === 'account-1' ? '测试账号' : id;

// noopSearchChange 是静态渲染测试使用的订单搜索回调占位实现。
const noopSearchChange = (): void => undefined;

describe('前端纯展示组件', /* 当前回调处理无浏览器依赖的展示组件断言。 */ () => {
  test('卡密图标覆盖各交付类型', /* 当前回调处理库存图标类型分支。 */ () => {
    expect(render(<CardIcon type="text" />)).toContain('text-blue-500');
    expect(render(<CardIcon type="image" />)).toContain('text-purple-500');
    expect(render(<CardIcon type="api" />)).toContain('text-blue-500');
    expect(render(<CardIcon type={'unknown' as never} />)).toContain('text-gray-500');
  });

  test('批量阶段指示器只高亮当前阶段', /* 当前回调处理批量发布步骤样式分支。 */ () => {
    // markup 是预检阶段批量指示器生成的静态 HTML。
    const markup = render(<BatchPhaseIndicator phase="preview" />);
    expect(markup).toContain('1 上传');
    expect(markup).toContain('2 预检');
    expect(markup).toContain('bg-blue-600 text-white');
    expect(markup.match(/bg-blue-600 text-white/g)).toHaveLength(1);
  });

  test('通知事件选择器渲染已选与未选事件', /* 当前回调处理通知事件列表的选择状态。 */ () => {
    // selected 是包含一个已选事件的通知选择器静态 HTML。
    const selected = render(<NotificationEventSelector selectedEvents={['account_offline']} onToggleEvent={noopEventToggle} />);
    expect(selected).toContain('掉线通知');
    expect(selected).toContain('border-brand bg-blue-50');
    expect(selected).toContain('系统错误');
    expect(selected).toContain('border-gray-100 hover:border-gray-300');
  });

  test('订单筛选栏渲染状态、账号和关键词输入', /* 当前回调处理订单筛选工具栏的静态结构。 */ () => {
    // markup 是订单筛选栏生成的静态 HTML。
    const markup = render(<OrderFilterBar
      filter="paid"
      onFilterChange={noopFilterChange}
      accountFilter="account-1"
      onAccountFilterChange={noopFilterChange}
      accounts={[{ id: 'account-1', nickname: '测试账号', enabled: true, auto_confirm: false }]}
      accountName={resolveAccountName}
      searchText="订单"
      onSearchChange={noopSearchChange}
    />);
    expect(markup).toContain('已发货');
    expect(markup).toContain('测试账号');
    expect(markup).toContain('订单号/商品/买家');
    expect(markup).toContain('aria-label="按账号筛选订单"');
  });
});
