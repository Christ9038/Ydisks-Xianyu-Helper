// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Card } from '../api';

// cardListMocks 保存卡密页面测试使用的 Hook、API 和批量弹窗替身。
const cardListMocks = vi.hoisted(/* cardListMockFactory 创建卡密页面共享替身。 */ () => ({
  cards: [] as Card[],
  loadCards: vi.fn(),
  openBatchModal: vi.fn(),
  createCard: vi.fn(),
  deleteCard: vi.fn(),
  updateCard: vi.fn(),
  testCardAPI: vi.fn(),
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
  testCardAPI: cardListMocks.testCardAPI,
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
    cardListMocks.testCardAPI.mockResolvedValue({ status: 'success', status_code: 200, response_content_type: 'application/json', response_fields: ['data', 'message'], extracted_value: 'TEST-CODE' });
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

  test('API 卡密使用三段请求编辑器并支持切换正文类型', /* 当前回调验证 API 地址、Headers、Params、Body 和 Content-Type 控件。 */ () => {
    render(<CardList />);
    fireEvent.click(screen.getByText('添加新卡密'));
    fireEvent.click(screen.getByRole('button', { name: 'API 接口' }));
    expect(screen.getByRole('heading', { name: 'API 请求配置' })).toBeTruthy();
    expect(screen.getByLabelText('Headers / 请求头第1行键名')).toBeTruthy();
    expect(screen.getByLabelText('Params / 查询参数第1行键名')).toBeTruthy();
    expect(screen.getByLabelText('JSON 请求正文')).toBeTruthy();
    fireEvent.change(screen.getByRole('combobox', { name: '请求正文 Content-Type' }), { target: { value: 'application/x-www-form-urlencoded' } });
    expect(screen.getByLabelText('Body 字段第1行键名')).toBeTruthy();
    fireEvent.click(screen.getByTitle('添加Headers / 请求头字段'));
    expect(screen.getByLabelText('Headers / 请求头第2行键名')).toBeTruthy();
  });

  test('新增卡密类型单行排列且 API 测试展示远端诊断', /* 当前回调验证类型顺序和 API 测试结果可见。 */ async () => {
    render(<CardList />);
    fireEvent.click(screen.getByText('添加新卡密'));
    // typeButtons 保存新增弹窗中按显示顺序排列的四种卡密类型按钮。
    const typeButtons = ['批量库存', '文本', '图片', 'API 接口'].map(/* typeButtonMapper 查找当前类型按钮。 */ typeName => screen.getByRole('button', { name: typeName }));
    expect(typeButtons.map(/* typeNameMapper 读取当前类型按钮名称。 */ button => button.textContent?.trim())).toEqual(['批量库存', '文本', '图片', 'API 接口']);
    fireEvent.click(typeButtons[3]);
    fireEvent.click(screen.getByRole('button', { name: '测试请求' }));
    await waitFor(/* testResultAssertion 等待 API 测试结果展示。 */ () => expect(screen.getByText('HTTP 状态：200')).toBeTruthy());
    expect(screen.getByText('响应字段：data、message')).toBeTruthy();
    expect(screen.getByText('提取结果：TEST-CODE')).toBeTruthy();
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
