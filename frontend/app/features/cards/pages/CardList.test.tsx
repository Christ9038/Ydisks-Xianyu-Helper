// @vitest-environment jsdom
import { act,cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
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
  getCardAPIEditor: vi.fn(),
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
  getCardAPIEditor: cardListMocks.getCardAPIEditor,
  testCardAPI: cardListMocks.testCardAPI,
}));

vi.mock('../components/BatchCardImportModal', /* batchModalMockFactory 提供批量弹窗替身。 */ () => ({
  BatchCardImportModal: /* BatchCardImportModalMock 表示批量导入弹窗替身。 */ () => null,
}));

vi.mock('../components/CardStockDialog', /* stockDialogMockFactory 用可观察的替身代替库存记录弹窗。 */ () => ({
  // 卡密库存弹窗替身：card 是传入的卡密组（id、name），onClose 是关闭回调。
  CardStockDialog: /* CardStockDialogMock 展示收到的卡密组，并暴露关闭按钮。 */ ({ card, onClose }: { /* card 是传入的卡密组。 */ card: { /* id 是卡密组标识。 */ id: number; /* name 是卡密组名称。 */ name: string }; /* onClose 是关闭回调。 */ onClose: () => void }) => (
    <div role="dialog" aria-label="库存记录替身">{card.id}:{card.name}<button onClick={onClose}>关闭替身</button></div>
  ),
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
    cardListMocks.getCardAPIEditor.mockResolvedValue({ url: 'https://example.com/claim', method: 'POST', timeout_seconds: 10,
      content_type: 'application/json', headers: '{"Authorization":"Bearer fixture-secret","Idempotency-Key":"{idempotency_key}"}', params: '{"order_id":"{order_id}"}', body: '{"trigger_type":"{trigger_type}"}', response_path: 'data.content', retry_enabled: true });
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

  test('data 卡展示未售与已售数量，并只为 data/API 卡提供库存记录入口', /* 当前回调验证库存列文案和弹窗开关。 */ () => {
    cardListMocks.cards = [{ ...cardFixture, sold_count: 3 }, textCardFixture, { id: 9, name: '接口卡', type: 'api', enabled: true, sold_count: 1,
      api_config: { url: 'https://example.com/claim', method: 'GET', content_type: 'application/json', timeout_seconds: 10, retry_enabled: false, headers_configured: false, params_configured: false, ready: true } }];
    render(<CardList />);
    expect(screen.getByText('未售 2 条 · 已售 3 条')).toBeTruthy();
    expect(screen.queryByLabelText('查看卡密 文案二 库存记录')).toBeNull();

    fireEvent.click(screen.getByLabelText('查看卡密 库存一 库存记录'));
    expect(screen.getByRole('dialog', { name: '库存记录替身' }).textContent).toContain('1:库存一');
    fireEvent.click(screen.getByText('关闭替身'));
    expect(screen.queryByRole('dialog', { name: '库存记录替身' })).toBeNull();

    fireEvent.click(screen.getByLabelText('查看卡密 接口卡 库存记录'));
    expect(screen.getByRole('dialog', { name: '库存记录替身' }).textContent).toContain('9:接口卡');
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
    await waitFor(/* editAssertion 等待卡密更新请求完成。 */ () => expect(cardListMocks.updateCard).toHaveBeenCalledWith(1, expect.objectContaining({ name: '库存更新' })));
    expect(cardListMocks.loadCards).toHaveBeenCalledTimes(2);
  });

  test('API 卡密使用三段请求编辑器并支持切换正文类型', /* 当前回调验证 API 地址、Headers、Params、Body 和 Content-Type 控件。 */ async () => {
    render(<CardList />);
    fireEvent.click(screen.getByText('添加新卡密'));
    fireEvent.click(screen.getByRole('button', { name: 'API 接口' }));
    await screen.findByRole('heading', { name: 'API 请求配置' });
    expect(screen.getByRole('heading', { name: 'API 请求配置' })).toBeTruthy();
    expect(screen.getByLabelText('Headers / 请求头第1行键名')).toBeTruthy();
    expect(screen.getByLabelText('Params / 查询参数第1行键名')).toBeTruthy();
    expect(screen.getByLabelText('JSON 请求正文')).toBeTruthy();
    expect(screen.getByLabelText('cURL 命令')).toBeTruthy();
    fireEvent.change(screen.getByRole('combobox', { name: '请求正文 Content-Type' }), { target: { value: 'application/x-www-form-urlencoded' } });
    expect(screen.getByLabelText('Body 字段第1行键名')).toBeTruthy();
    fireEvent.click(screen.getByTitle('添加Headers / 请求头字段'));
    expect(screen.getByLabelText('Headers / 请求头第2行键名')).toBeTruthy();
  });

  test('cURL 导入会填充新增 API 卡密的所有受支持请求字段', /* 当前回调等待受控编辑器完成批量状态写回后验证新增表单。 */ async () => {
    render(<CardList />);
    fireEvent.click(screen.getByText('添加新卡密'));
    fireEvent.click(screen.getByRole('button', { name: 'API 接口' }));
    fireEvent.change(screen.getByLabelText('cURL 命令'), { target: { value: "curl -X POST 'https://api.example.com/code?shop=main' -H 'Authorization: Bearer sample-token' -H 'Content-Type: application/json' -d '{\"order_id\":\"{order_id}\"}'" } });
    fireEvent.click(screen.getByRole('button', { name: '解析并填充' }));
    await waitFor(/* curlImportAssertion 等待批量表单更新完成字段回填。 */ () => expect(screen.getByDisplayValue('https://api.example.com/code')).toBeTruthy());
    expect(screen.getByDisplayValue('Bearer sample-token')).toBeTruthy();
    expect(screen.getByDisplayValue('main')).toBeTruthy();
    // jsonBody 保存 cURL 导入后正文编辑器中的格式化 JSON 文本。
    const jsonBody = screen.getByLabelText('JSON 请求正文') as HTMLTextAreaElement;
    expect(jsonBody.value).toBe('{\n  "order_id": "{order_id}"\n}');
  });

  test('新增卡密类型单行排列且 API 测试展示远端诊断', /* 当前回调验证类型顺序和 API 测试结果可见。 */ async () => {
    render(<CardList />);
    fireEvent.click(screen.getByText('添加新卡密'));
    // typeButtons 保存新增弹窗中按显示顺序排列的四种卡密类型按钮。
    const typeButtons = ['批量库存', '文本', '图片', 'API 接口'].map(/* typeButtonMapper 查找当前类型按钮。 */ typeName => screen.getByRole('button', { name: typeName }));
    expect(typeButtons.map(/* typeNameMapper 读取当前类型按钮名称。 */ button => button.textContent?.trim())).toEqual(['批量库存', '文本', '图片', 'API 接口']);
    fireEvent.click(typeButtons[3]);
    await screen.findByRole('heading', { name: 'API 请求配置' });
    fireEvent.click(screen.getByRole('button', { name: '测试请求' }));
    await waitFor(/* testResultAssertion 等待 API 测试结果展示。 */ () => expect(screen.getByText('HTTP 状态：200')).toBeTruthy());
    expect(screen.getByText('响应字段：data、message')).toBeTruthy();
    expect(screen.getByText('提取结果：TEST-CODE')).toBeTruthy();
  });

  test('API 测试外部失败且缺少响应字段时仍展示诊断', /* 当前回调验证旧服务端返回 null 字段时页面不崩溃。 */ async () => {
    cardListMocks.testCardAPI.mockResolvedValue({
      status: 'failed',
      status_code: 404,
      response_content_type: 'application/json; charset=utf-8',
      response_fields: null,
      response_preview: '{"error":{"message":"Invalid URL (GET /v1)"}}',
    } as never);
    render(<CardList />);
    fireEvent.click(screen.getByText('添加新卡密'));
    fireEvent.click(screen.getByRole('button', { name: 'API 接口' }));
    await screen.findByRole('heading', { name: 'API 请求配置' });
    fireEvent.click(screen.getByRole('button', { name: '测试请求' }));
    await waitFor(/* failureResultAssertion 等待外部失败诊断完成渲染。 */ () => expect(screen.getByText('HTTP 状态：404')).toBeTruthy());
    expect(screen.getByText('测试失败')).toBeTruthy();
    expect(screen.getByText('响应字段：未识别 JSON 字段')).toBeTruthy();
  });

  test('编辑已保存 API 卡密完整回显，密钥默认隐藏且测试与保存共用草稿', /* editorRegression 覆盖截图中的重新打开后测试失败路径。 */ async () => {
    cardListMocks.cards = [{ id: 3, name: '积分卡', type: 'api', enabled: true }];
    render(<CardList />);
    fireEvent.click(screen.getByTitle('编辑'));
    await waitFor(/* waitEditor 等待完整模板载入。 */ () => expect(screen.getByDisplayValue('{idempotency_key}')).toBeTruthy());
    // secret 是默认遮罩的完整认证字段。
    const secret = screen.getByLabelText('Headers / 请求头第1行值') as HTMLInputElement;
    expect(secret.value).toBe('Bearer fixture-secret');
    expect(secret.type).toBe('password');
    fireEvent.click(screen.getByRole('button', { name: '显示Authorization' }));
    expect(secret.type).toBe('text');
    fireEvent.click(screen.getByRole('button', { name: '隐藏Authorization' }));
    expect(secret.type).toBe('password');
    expect((screen.getByLabelText('JSON 请求正文') as HTMLTextAreaElement).value).toContain('{trigger_type}');
    fireEvent.click(screen.getByRole('button', { name: '测试请求' }));
    await waitFor(/* waitSavedTest 等待保存配置的测试完成。 */ () => expect(screen.getByText('测试成功')).toBeTruthy());
    expect(cardListMocks.testCardAPI).toHaveBeenCalledWith(expect.objectContaining({ headers_action: 'retain', params_action: 'retain', retry_enabled: true }), 3, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    fireEvent.change(secret, { target: { value: 'Bearer replacement' } });
    fireEvent.click(screen.getByRole('button', { name: '测试请求' }));
    await waitFor(/* waitDraftTest 等待新草稿的测试完成。 */ () => expect(cardListMocks.testCardAPI).toHaveBeenCalledTimes(2));
    // tested 是测试实际接收的草稿，随后保存必须使用同样的映射。
    const tested = cardListMocks.testCardAPI.mock.calls[1][0];
    expect(tested.headers_action).toBe('replace');
    expect(tested.headers).toContain('Bearer replacement');
    fireEvent.click(screen.getByText('保存更改'));
    await waitFor(/* waitSave 验证保存和测试采用完全相同的 API 配置。 */ () => expect(cardListMocks.updateCard).toHaveBeenCalledWith(3, expect.objectContaining({ api_config: tested })));
    expect(cardListMocks.getCardAPIEditor).toHaveBeenCalledWith(3, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });

  test('读取失败禁用测试与保存，重试恢复且关闭会取消测试', /* editorFailure 验证错误恢复和弹窗生命周期。 */ async () => {
    cardListMocks.cards = [{ id: 3, name: '积分卡', type: 'api', enabled: true }];
    cardListMocks.getCardAPIEditor.mockRejectedValueOnce(new Error('内部错误不得回显'));
    render(<CardList />);
    fireEvent.click(screen.getByTitle('编辑'));
    await waitFor(/* waitFailure 等待安全错误提示。 */ () => expect(screen.getByRole('alert')).toBeTruthy());
    expect((screen.getByText('保存更改') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByText('重试'));
    await waitFor(/* waitRetry 等待重新载入完整配置。 */ () => expect(screen.getByDisplayValue('{idempotency_key}')).toBeTruthy());
    // finish 保存迟到的测试结果，用于验证关闭后的隔离。
    let finish!: (value: unknown) => void;
    cardListMocks.testCardAPI.mockImplementationOnce(/* pendingTest 模拟未完成的远端请求。 */ () => new Promise(/* captureTest 保留完成回调。 */ resolve => { finish = resolve; }));
    fireEvent.click(screen.getByRole('button', { name: '测试请求' }));
    // signal 是编辑器绑定的取消信号。
    const signal = cardListMocks.testCardAPI.mock.calls[0][2].signal as AbortSignal;
    fireEvent.click(screen.getByText('取消'));
    expect(signal.aborted).toBe(true);
    await act(/* finishClosedTest 完成已关闭弹窗的旧请求。 */ async () => finish({ status: 'success', status_code: 200, response_fields: [] }));
    expect(screen.queryByText('测试成功')).toBeNull();
    expect(screen.queryByDisplayValue('Bearer fixture-secret')).toBeNull();
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
