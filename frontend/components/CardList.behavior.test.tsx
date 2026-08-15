// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import CardList from './CardList';
import { useCardBatchActions, useCardsData } from '../app/features/cards/hooks';

vi.mock('../app/features/cards/hooks', /* cardsHookMockFactory 提供卡密页面数据和批量状态。 */ () => ({ useCardBatchActions: vi.fn(), useCardsData: vi.fn() }));
vi.mock('../app/features/cards/components/BatchCardImportModal', /* batchModalMockFactory 隔离批量卡密弹窗。 */ () => ({ BatchCardImportModal: /* batchModalRenderer 渲染批量弹窗占位节点。 */ () => <div data-testid="batch-card-modal" /> }));
vi.mock('../app/features/cards/components/CardIcon', /* cardIconMockFactory 隔离卡密图标组件。 */ () => ({ CardIcon: /* cardIconRenderer 渲染卡密图标占位节点。 */ () => <span data-testid="card-icon" /> }));

// useCardsDataMock 是卡密列表数据 Hook 的可控替身。
const useCardsDataMock = vi.mocked(useCardsData);
// useCardBatchActionsMock 是卡密批量操作 Hook 的可控替身。
const useCardBatchActionsMock = vi.mocked(useCardBatchActions);

describe('CardList 页面空状态', /* 当前回调覆盖卡密列表无数据和批量入口。 */ () => {
  test('没有卡密时展示空状态和添加入口', /* 当前回调验证卡密库存空列表页面。 */ () => {
    // loadCards 是卡密列表刷新动作替身。
    const loadCards = vi.fn().mockResolvedValue(undefined);
    // openBatchModal 是批量导入弹窗打开动作替身。
    const openBatchModal = vi.fn();
    useCardsDataMock.mockReturnValue({ cards: [], loadCards } as never);
    useCardBatchActionsMock.mockReturnValue({ openBatchModal } as never);
    render(<CardList />);
    expect(screen.getByText('暂无卡密配置，请点击右上角添加。')).toBeTruthy();
    expect(screen.getByRole('button', { name: '添加新卡密' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '批量导入' })).toBeTruthy();
  });
});
