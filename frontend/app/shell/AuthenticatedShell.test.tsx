// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import React from 'react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { Item } from '../../shared/api-contract/common';
import type { DeliveryRuleTarget } from './AuthenticatedShell';

vi.mock('../features/dashboard/pages/Dashboard', /* dashboardMockFactory 提供仪表盘页面的轻量替身。 */ () => {
  // DashboardMock 渲染仪表盘页面标识，避免行为测试加载真实图表依赖。
  const DashboardMock: React.FC = () => <div data-testid="dashboard-page">仪表盘</div>;
  return { default: DashboardMock };
});

vi.mock('../features/settings/pages/Settings', /* settingsMockFactory 提供设置页面的轻量替身。 */ () => {
  // SettingsMock 渲染设置页面标识，验证管理员权限分支。
  const SettingsMock: React.FC = () => <div data-testid="settings-page">设置</div>;
  return { default: SettingsMock };
});

vi.mock('../features/items/pages/ItemList', /* itemListMockFactory 提供商品页面的联动替身。 */ () => {
  // MockItemListProps 描述商品页面替身接收的规则配置回调。
  interface MockItemListProps {
    // onConfigureDelivery 负责通知父级打开商品规则配置。
    onConfigureDelivery: (item: Item) => void;
  }
  // ItemListMock 模拟用户点击商品规则配置入口。
  const ItemListMock: React.FC<MockItemListProps> = ({ onConfigureDelivery }) => {
    // itemFixture 是商品→规则联动测试使用的最小商品对象。
    const itemFixture = { cookie_id: 'cookie-1', item_id: 'item-1' } as Item;
    return <button data-testid="configure-delivery" onClick={/* configureAction 触发规则配置联动。 */ () => onConfigureDelivery(itemFixture)}>配置发货规则</button>;
  };
  return { default: ItemListMock };
});

vi.mock('../features/rules/pages/Rules', /* rulesMockFactory 提供规则页面的轻量替身。 */ () => {
  // MockRulesProps 描述规则页面替身接收的联动参数。
  interface MockRulesProps {
    // initialDeliveryTarget 保存商品页面传来的规则目标。
    initialDeliveryTarget?: DeliveryRuleTarget;
    // onDeliveryTargetHandled 表示规则页面已消费联动目标。
    onDeliveryTargetHandled?: () => void;
  }
  // RulesMock 展示规则目标，验证商品→规则页参数保持不变。
  const RulesMock: React.FC<MockRulesProps> = ({ initialDeliveryTarget, onDeliveryTargetHandled }) => (
    <div data-testid="rules-page">
      <span>{initialDeliveryTarget?.cookieId}:{initialDeliveryTarget?.itemId}</span>
      <button onClick={/* handledAction 确认规则页面已消费目标。 */ () => onDeliveryTargetHandled?.()}>完成</button>
    </div>
  );
  return { default: RulesMock };
});

// loadAppContent 动态导入认证后页面组合器，等待懒加载模块完成解析。
const loadAppContent = async () => import('./AuthenticatedShell');

describe('AuthenticatedShell 页面组合行为', /* 当前回调验证权限回退和跨页面联动边界。 */ () => {
  afterEach(/* 当前回调清理每个页面组合测试挂载的 DOM。 */ () => cleanup());

  test('非管理员访问设置页时回退到仪表盘', /* 当前回调验证非管理员的设置页权限回退。 */ async () => {
    // appContentModule 表示动态加载的页面组合模块。
    const appContentModule = await loadAppContent();
    // AppContentComponent 表示动态模块导出的页面组合组件。
    const AppContentComponent = appContentModule.AppContent;
    render(
      <AppContentComponent
        activeTab="settings"
        isAdmin={false}
        onConfigureDelivery={/* configureTargetAction 接收商品规则目标。 */ () => undefined}
        onDeliveryTargetHandled={/* handledAction 表示规则目标已消费。 */ () => undefined}
      />,
    );

    await waitFor(/* dashboardAssertion 等待仪表盘懒加载完成。 */ () => expect(screen.getByTestId('dashboard-page')).toBeTruthy());
    expect(screen.queryByTestId('settings-page')).toBeNull();
  });

  test('管理员访问设置页时加载设置页面', /* 当前回调验证管理员设置页面加载。 */ async () => {
    // appContentModule 表示动态加载的页面组合模块。
    const appContentModule = await loadAppContent();
    // AppContentComponent 表示动态模块导出的页面组合组件。
    const AppContentComponent = appContentModule.AppContent;
    render(
      <AppContentComponent
        activeTab="settings"
        isAdmin
        onConfigureDelivery={/* configureTargetAction 接收商品规则目标。 */ () => undefined}
        onDeliveryTargetHandled={/* handledAction 表示规则目标已消费。 */ () => undefined}
      />,
    );

    await waitFor(/* settingsAssertion 等待设置页面懒加载完成。 */ () => expect(screen.getByTestId('settings-page')).toBeTruthy());
  });

  test('商品配置入口把目标传递到规则页面并支持消费确认', /* 当前回调验证商品到规则页面的联动参数和消费确认。 */ async () => {
    // appContentModule 表示动态加载的页面组合模块。
    const appContentModule = await loadAppContent();
    // AppContentComponent 表示动态模块导出的页面组合组件。
    const AppContentComponent = appContentModule.AppContent;
    // configureTarget 保存商品页面发出的规则目标。
    const configureTarget = vi.fn<(target: DeliveryRuleTarget) => void>();
    render(
      <AppContentComponent
        activeTab="items"
        isAdmin={false}
        onConfigureDelivery={configureTarget}
        onDeliveryTargetHandled={/* handledAction 表示规则目标已消费。 */ () => undefined}
      />,
    );

    await waitFor(/* itemAssertion 等待商品页面懒加载完成。 */ () => expect(screen.getByTestId('configure-delivery')).toBeTruthy());
    fireEvent.click(screen.getByTestId('configure-delivery'));
    expect(configureTarget).toHaveBeenCalledWith(expect.objectContaining({ cookieId: 'cookie-1', itemId: 'item-1' }));
    // target 是商品页面刚刚发出的完整规则配置目标。
    const target = configureTarget.mock.calls[0][0];
    expect(target.requestId).toEqual(expect.any(Number));

    cleanup();
    // handledTarget 记录规则页面消费联动目标的回调。
    const handledTarget = vi.fn();
    render(
      <AppContentComponent
        activeTab="rules"
        isAdmin={false}
        deliveryRuleTarget={target}
        onConfigureDelivery={/* configureTargetAction 接收商品规则目标。 */ () => undefined}
        onDeliveryTargetHandled={handledTarget}
      />,
    );
    await waitFor(/* rulesAssertion 等待规则页面懒加载完成。 */ () => expect(screen.getByTestId('rules-page')).toBeTruthy());
    expect(screen.getByText('cookie-1:item-1')).toBeTruthy();
    fireEvent.click(screen.getByText('完成'));
    expect(handledTarget).toHaveBeenCalledTimes(1);
  });
});
