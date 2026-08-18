// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Card } from '../../../../shared/api-contract/cards';

// cardListMocks 保存卡密页面测试使用的 Hook、API 和批量弹窗替身。
const cardListMocks = vi.hoisted(/* cardListMockFactory 创建卡密页面共享替身。 */ () => ({
  cards: [] as Card[],
  loadCards: vi.fn(),
  openBatchModal: vi.fn(),
  createCard: vi.fn(),
  deleteCard: vi.fn(),
  updateCard: vi.fn(),
}));

vi.mock('../hooks', /* cardsHooksMockFactory 提供卡密库存和批量 Hook 替身。 */ () => ({
  useCardsData: /* useCardsDataMock 返回固定卡密库存。 */ () => ({
    cards: cardListMocks.cards,
    setCards: vi.fn(),
    loading: false,
    loadCards: cardListMocks.loadCards,
  }),
  useCardBatchActions: /* useCardBatchActionsMock 返回批量弹窗状态。 */ () => ({
    showBatchModal: false,
    setShowBatchModal: vi.fn(),
    batchTab: 'create',
    setBatchTab: vi.fn(),
    batchFile: null,
    setBatchFile: vi.fn(),
    batchResult: null,
    batchBusy: false,
    appendTargetId: '',
    setAppendTargetId: vi.fn(),
    appendContent: '',
    setAppendContent: vi.fn(),
    appendResult: null,
    appendError: '',
    appendPreview: [],
    openBatchModal: cardListMocks.openBatchModal,
    closeBatchModal: vi.fn(),
    handleBatchCreate: vi.fn(),
    handleBatchAppend: vi.fn(),
    handleRetryBatchCreate: vi.fn(),
    handleRetryBatchAppend: vi.fn(),
  }),
}));

vi.mock('../api', /* cardsApiMockFactory 提供卡密页面动作 API 替身。 */ () => ({
  createCard: cardListMocks.createCard,
  deleteCard: cardListMocks.deleteCard,
  updateCard: cardListMocks.updateCard,
}));

vi.mock('../components/BatchCardImportModal', /* batchModalMockFactory 提供批量弹窗替身。 */ () => ({
  BatchCardImportModal: /* BatchCardImportModalMock 表示批量导入弹窗替身。 */ () => null,
}));

import CardList from './CardList';

// cardFixture 表示卡密页面中的 data 类型库存。
const cardFixture: Card = {
  id: 1,
  name: '库存一',
  type: 'data',
  data_content: 'A\nB',
  description: '测试库存',
  enabled: true,
  delay_seconds: 0,
  created_at: '2026-08-15T00:00:00Z',
  updated_at: '2026-08-15T00:00:00Z',
};

// textCardFixture 表示卡密页面中的文本类型卡密组。
const textCardFixture: Card = {
  ...cardFixture,
  id: 2,
  name: '文案二',
  type: 'text',
  data_content: undefined,
  text_content: '感谢购买',
};

describe('CardList 页面组合行为', /* 当前回调验证卡密筛选、批量入口、新增、编辑、启停和删除流程。 */ () => {
  beforeEach(/* 当前回调重置卡密页面 API、库存和浏览器提示替身。 */ () => {
    vi.clearAllMocks();
    cardListMocks.cards = [cardFixture, textCardFixture];
    cardListMocks.loadCards.mockResolvedValue(undefined);
    cardListMocks.createCard.mockResolvedValue({ success: true, id: 3 });
    cardListMocks.deleteCard.mockResolvedValue({ success: true });
    cardListMocks.updateCard.mockResolvedValue({ success: true });
    vi.spyOn(window, 'alert').mockImplementation(/* alertImplementation 屏蔽卡密页面提示。 */ () => undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  afterEach(/* 当前回调清理卡密页面 DOM 和浏览器提示替身。 */ () => {
    cleanup();
    vi.restoreAllMocks();
  });

  test('筛选卡密并打开批量导入入口', /* 当前回调验证卡密列表筛选和批量操作转发。 */ () => {
    render(<CardList />);
    expect(screen.getByText('显示 2 / 2 组')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('按卡密类型筛选'), { target: { value: 'data' } });
    expect(screen.getByText('显示 1 / 2 组')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('按卡密名称搜索'), { target: { value: '库存' } });
    expect(screen.getByText('显示 1 / 2 组')).toBeTruthy();
    fireEvent.click(screen.getByText('批量导入'));
    expect(cardListMocks.openBatchModal).toHaveBeenCalledTimes(1);
  });

  test('新增和编辑卡密会映射表单字段并刷新库存', /* 当前回调验证卡密新增与编辑页面行为。 */ async () => {
    render(<CardList />);
    fireEvent.click(screen.getByText('添加新卡密'));
    fireEvent.change(screen.getByPlaceholderText('例如：VIP会员卡密'), { target: { value: '新文案' } });
    fireEvent.click(screen.getByRole('button', { name: '文本' }));
    fireEvent.change(screen.getByPlaceholderText('请输入每次发货时发送的固定文字'), { target: { value: '欢迎购买' } });
    fireEvent.click(screen.getByText('添加卡密'));
    await waitFor(/* addAssertion 等待卡密创建请求完成。 */ () => expect(cardListMocks.createCard).toHaveBeenCalledWith(expect.objectContaining({ name: '新文案', type: 'text', text_content: '欢迎购买' })));
    expect(cardListMocks.loadCards).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getAllByTitle('编辑')[0]);
    fireEvent.change(screen.getByDisplayValue('库存一'), { target: { value: '库存更新' } });
    fireEvent.click(screen.getByText('保存更改'));
    await waitFor(/* editAssertion 等待卡密更新请求完成。 */ () => expect(cardListMocks.updateCard).toHaveBeenCalledWith(1, expect.objectContaining({ name: '库存更新', data_content: 'A\nB' })));
    expect(cardListMocks.loadCards).toHaveBeenCalledTimes(2);
  });

  test('启停、复制和删除按钮调用对应页面动作', /* 当前回调验证卡密行级动作边界。 */ async () => {
    render(<CardList />);
    fireEvent.click(screen.getByRole('button', { name: '切换卡密 库存一 状态' }));
    await waitFor(/* toggleAssertion 等待卡密启停请求完成。 */ () => expect(cardListMocks.updateCard).toHaveBeenCalledWith(1, expect.objectContaining({ enabled: false })));
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    fireEvent.click(screen.getAllByTitle('复制卡密组ID，用于批量铺货表格')[0]);
    fireEvent.click(screen.getByRole('button', { name: '删除卡密 库存一' }));
    await waitFor(/* deleteAssertion 等待卡密删除请求完成。 */ () => expect(cardListMocks.deleteCard).toHaveBeenCalledWith(1));
    expect(cardListMocks.loadCards).toHaveBeenCalledTimes(2);
  });
});
