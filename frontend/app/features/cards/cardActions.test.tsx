// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Card } from './api';
import { useCardActions } from './cardActions';

// cardActionMocks 保存卡密动作 Hook 测试使用的 API 替身。
const cardActionMocks = vi.hoisted(/* cardActionMockFactory 创建卡密动作 API 替身。 */ () => ({
  createCard: vi.fn(),
  deleteCard: vi.fn(),
  updateCard: vi.fn(),
  getCardAPIEditor: vi.fn(),
}));

vi.mock('./api', /* cardApiMockFactory 提供卡密动作 API 替身。 */ () => ({
  createCard: cardActionMocks.createCard,
  deleteCard: cardActionMocks.deleteCard,
  updateCard: cardActionMocks.updateCard,
  getCardAPIEditor: cardActionMocks.getCardAPIEditor,
}));

// cardFixture 表示卡密动作 Hook 使用的 data 类型卡密组。
const cardFixture: Card = {
  id: 1,
  name: '库存一',
  type: 'data',
  data_content: 'A\nB',
  description: '测试库存',
  enabled: true,
  delay_seconds: 0,
};

// textCardFixture 表示用于筛选和新增测试的文本类型卡密组。
const textCardFixture: Card = {
  ...cardFixture,
  id: 2,
  name: '文案二',
  type: 'text',
  data_content: undefined,
  text_content: '感谢购买',
};

// createCardHook 创建卡密动作 Hook 并注入库存刷新替身。
const createCardHook = () => {
  // loadCards 是卡密动作完成后的库存刷新替身。
  const loadCards = vi.fn().mockResolvedValue(undefined);
  // hook 是卡密动作 Hook 的渲染结果。
  const hook = renderHook(/* hookFactory 创建卡密动作 Hook。 */ () => useCardActions({ cards: [cardFixture, textCardFixture], loadCards }));
  return { hook, loadCards };
};

describe('useCardActions 卡密动作协调器', /* 当前回调验证卡密筛选、编辑、新增、删除和展示动作。 */ () => {
  beforeEach(/* 当前回调重置卡密 API 和浏览器提示替身。 */ () => {
    vi.clearAllMocks();
    cardActionMocks.getCardAPIEditor.mockResolvedValue({ url: 'https://example.com/claim', method: 'POST', timeout_seconds: 10,
      content_type: 'application/json', headers: '{"Authorization":"Bearer fixture-secret","Idempotency-Key":"{idempotency_key}"}', params: '{"order_id":"{order_id}"}', body: '{"trigger_type":"{trigger_type}"}', response_path: 'data.content', retry_enabled: true });
    cardActionMocks.createCard.mockResolvedValue({ success: true, id: 3 });
    cardActionMocks.deleteCard.mockResolvedValue({ success: true });
    cardActionMocks.updateCard.mockResolvedValue({ success: true });
    vi.spyOn(window, 'alert').mockImplementation(/* alertImplementation 屏蔽卡密动作提示。 */ () => undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  afterEach(/* 当前回调恢复卡密测试浏览器替身。 */ () => {
    vi.restoreAllMocks();
  });

  test('图片编辑拒绝非法来源并显式提交 URL 与路径清空值', /* 当前回调验证读取本地路径和修改来源的提交语义。 */ async () => {
    // hook 是图片卡密编辑动作及刷新替身。
    const { hook } = createCardHook();
    await act(/* 当前回调打开本地图片卡密草稿。 */ async () => { await hook.result.current.handleEdit({ ...cardFixture, type: 'image', image_path: 'goods/a.png' }); });
    expect(hook.result.current.editForm.image_source).toBe('local');
    act(/* 当前回调模拟空本地路径输入。 */ () => hook.result.current.setEditForm(current => ({ ...current, image_path: '' })));
    await act(/* 当前回调验证空本地路径不会调用服务端。 */ async () => hook.result.current.handleSaveEdit());
    expect(cardActionMocks.updateCard).not.toHaveBeenCalled();
    act(/* 当前回调显式选择 URL，隐藏路径不应残留到提交。 */ () => hook.result.current.setEditForm(/* current 是切换图片来源前的草稿。 */ current => ({ ...current, image_source: 'url', image_url: ' https://example.com/a.png ', image_path: 'old.png' })));
    await act(/* 当前回调保存新的 URL 来源。 */ async () => hook.result.current.handleSaveEdit());
    expect(cardActionMocks.updateCard).toHaveBeenCalledWith(1, expect.objectContaining({ image_url: 'https://example.com/a.png', image_path: '' }));
  });

  test.each(['成功', '失败'])('取消卡密保存后旧请求%s不覆盖后续草稿', /* 当前回调验证迟到的保存结果不会关闭重新打开的草稿。 */ async outcome => {
    // finish 控制旧卡密保存返回的时机。
    let finish!: () => void;
    cardActionMocks.updateCard.mockImplementationOnce(/* 当前替身模拟不能撤回的后台变更。 */ () => new Promise<void>(/* resolve 和 reject 分别模拟后台迟到结果。 */ (resolve, reject) => { finish = /* 当前完成器在取消后模拟成功或失败。 */ () => outcome === '成功' ? resolve() : reject(new Error('旧保存失败')); }));
    // hook 和 loadCards 用于确认旧请求只刷新列表，不关闭重新打开的草稿。
    const { hook, loadCards } = createCardHook();
    await act(/* 当前回调编辑本地图片卡密。 */ async () => { await hook.result.current.handleEdit({ ...cardFixture, type: 'image', image_path: 'a.png' }); });
    // pending 是旧草稿的保存过程。
    let pending!: Promise<void>;
    act(/* 当前回调发起尚未完成的保存。 */ () => { pending = hook.result.current.handleSaveEdit(); });
    await act(/* 当前回调关闭旧草稿并打开新的编辑目标。 */ async () => {
      hook.result.current.closeEdit();
      await hook.result.current.handleEdit(textCardFixture);
    });
    await act(/* 当前回调完成旧请求并等待其后续逻辑。 */ async () => { finish(); await pending; });
    expect(hook.result.current.showEditModal).toBe(true);
    expect(hook.result.current.editForm.name).toBe('文案二');
    if (outcome === '失败') {
      expect(window.alert).toHaveBeenCalledWith('旧保存失败');
      expect(loadCards).not.toHaveBeenCalled();
    } else {
      expect(window.alert).not.toHaveBeenCalled();
      expect(loadCards).toHaveBeenCalledTimes(1);
    }
  });

  test('按类型和名称筛选卡密并保存编辑草稿', /* 当前回调验证卡密筛选和编辑提交。 */ async () => {
    // actionContext 保存卡密编辑 Hook 和库存刷新替身。
    const { hook, loadCards } = createCardHook();
    expect(hook.result.current.dataCards).toEqual([cardFixture]);
    act(/* filterAction 筛选文本类型卡密。 */ () => hook.result.current.setTypeFilter('text'));
    expect(hook.result.current.filteredCards).toEqual([textCardFixture]);
    act(/* searchAction 写入卡密名称搜索词。 */ () => hook.result.current.setNameSearch('文案'));
    expect(hook.result.current.filteredCards).toEqual([textCardFixture]);

    await act(/* editAction 打开卡密编辑弹窗。 */ async () => hook.result.current.handleEdit(textCardFixture));
    act(/* editPatchAction 更新卡密编辑名称。 */ () => hook.result.current.setEditForm(current => ({ ...current, name: '新文案' })));
    await act(/* saveEditAction 保存卡密编辑草稿。 */ async () => hook.result.current.handleSaveEdit());
    expect(cardActionMocks.updateCard).toHaveBeenCalledWith(2, expect.objectContaining({ name: '新文案', text_content: '感谢购买' }));
    expect(loadCards).toHaveBeenCalledTimes(1);
  });

  test('新增卡密校验不同类型内容并刷新库存', /* 当前回调验证卡密新增表单校验和 API 载荷。 */ async () => {
    // actionContext 保存卡密新增 Hook 和库存刷新替身。
    const { hook, loadCards } = createCardHook();
    act(/* openAddAction 打开新增卡密弹窗。 */ () => hook.result.current.setShowAddModal(true));
    await act(/* emptyAddAction 提交空表单触发名称校验。 */ async () => hook.result.current.handleAddCard());
    expect(window.alert).toHaveBeenCalledWith('请输入卡密名称');

    act(/* formAction 写入新增文本卡密表单。 */ () => hook.result.current.setAddForm(current => ({ ...current, name: '新文案', type: 'text', content: '欢迎' })));
    await act(/* addAction 提交新增文本卡密。 */ async () => hook.result.current.handleAddCard());
    expect(cardActionMocks.createCard).toHaveBeenCalledWith(expect.objectContaining({ name: '新文案', type: 'text', text_content: '欢迎' }));
    expect(loadCards).toHaveBeenCalledTimes(1);
    expect(hook.result.current.showAddModal).toBe(false);
  });

  test('编辑卡密名称时不提交过期库存和规格快照', /* 当前回调验证普通编辑交给服务端保留并发变化的卡密字段。 */ async () => {
    // skuCard 保存带规格限制的 data 卡密组。
    const skuCard: Card = {
      ...cardFixture,
      is_multi_spec: true,
      spec_name: '套餐',
      spec_value: '年度',
    };
    // loadCards 是规格卡编辑完成后的库存刷新替身。
    const loadCards = vi.fn().mockResolvedValue(undefined);
    // hook 是注入规格卡测试数据的动作 Hook。
    const hook = renderHook(/* hookFactory 渲染规格卡动作 Hook。 */ () => useCardActions({ cards: [skuCard], loadCards }));
    await act(/* editAction 打开规格卡编辑表单。 */ async () => hook.result.current.handleEdit(skuCard));
    act(/* renameAction 只修改规格卡名称。 */ () => hook.result.current.setEditForm(current => ({ ...current, name: '年度套餐' })));
    await act(/* saveAction 保存规格卡名称并等待刷新完成。 */ async () => hook.result.current.handleSaveEdit());
    // mutation 保存名称编辑实际提交的字段，规格和库存快照应由服务端按省略语义保留。
    const mutation = cardActionMocks.updateCard.mock.calls[0]?.[1];
    expect(mutation).not.toHaveProperty('data_content');
    expect(mutation).not.toHaveProperty('is_multi_spec');
    expect(mutation).not.toHaveProperty('spec_name');
    expect(mutation).not.toHaveProperty('spec_value');
  });

  test('编辑 API 卡替换空模板时明确清空而不是保留旧模板', /* 当前回调验证敏感模板替换空值的显式清除语义。 */ async () => {
    // apiCard 保存只含脱敏摘要的 API 卡密组。
    const apiCard: Card = {
      ...cardFixture,
      id: 4,
      name: '接口卡',
      type: 'api',
      api_config: {
        url: 'https://example.test/card',
        method: 'POST',
        timeout_seconds: 10,
        content_type: 'application/json',
        headers_configured: true,
        params_configured: true,
        retry_enabled: false,
        ready: true,
      },
    };
    // loadCards 是 API 卡编辑完成后的刷新替身。
    const loadCards = vi.fn().mockResolvedValue(undefined);
    // hook 是注入 API 卡测试数据的动作 Hook。
    const hook = renderHook(/* hookFactory 渲染注入 API 卡的动作 Hook。 */ () => useCardActions({ cards: [apiCard], loadCards }));
    await act(/* editAction 打开 API 卡编辑表单并等待完整配置。 */ async () => hook.result.current.handleEdit(apiCard));
    act(/* templateAction 将两个敏感模板设置为显式替换。 */ () => hook.result.current.setEditForm(/* formUpdater 清空替换模板的表单值。 */ current => ({
      ...current,
      api_headers_action: 'replace',
      api_params_action: 'replace',
      api_headers: '',
      api_params: '',
    })));
    await act(/* saveAction 提交 API 卡编辑并等待刷新完成。 */ async () => hook.result.current.handleSaveEdit());
    expect(cardActionMocks.updateCard).toHaveBeenCalledWith(4, expect.objectContaining({
      api_config: expect.objectContaining({ headers: {}, params: {} }),
    }));
  });

  test('切换和关闭编辑时丢弃旧配置，旧请求不能覆盖新卡密', /* editorRace 验证先后两次编辑读取的响应顺序。 */ async () => {
    // hook 保存当前编辑协调器。
    const { hook } = createCardHook();
    // finish 保留第一个尚未完成的配置读取。
    let finish!: (value: unknown) => void;
    cardActionMocks.getCardAPIEditor.mockImplementationOnce(/* slowEditor 模拟迟到的配置响应。 */ () => new Promise(/* captureEditor 保留响应完成回调。 */ resolve => { finish = resolve; }));
    // pending 保存第一个编辑请求的完成状态。
    let pending!: Promise<void>;
    act(/* openFirst 打开第一张 API 卡。 */ () => { pending = hook.result.current.handleEdit({ id: 3, name: '旧卡', type: 'api', enabled: true }); });
    // signal 是旧编辑请求的取消信号。
    const signal = cardActionMocks.getCardAPIEditor.mock.calls[0][1].signal as AbortSignal;
    await act(/* openSecond 切换到第二张 API 卡并等待配置。 */ async () => hook.result.current.handleEdit({ id: 4, name: '新卡', type: 'api', enabled: true }));
    expect(signal.aborted).toBe(true);
    await act(/* finishOld 完成迟到响应，不能污染新草稿。 */ async () => { finish({ headers: 'stale-secret' }); await pending; });
    expect(hook.result.current.editForm.name).toBe('新卡');
    expect(hook.result.current.editForm.api_headers).not.toContain('stale-secret');
    act(/* closeEditor 关闭弹窗并销毁敏感草稿。 */ () => hook.result.current.closeEdit());
    expect(hook.result.current.editForm).toEqual({});
    expect(hook.result.current.selectedCard).toBeNull();
  });

  test('删除取消、删除失败和状态切换失败均不制造虚假成功', /* 当前回调验证卡密删除和启停异常分支。 */ async () => {
    // actionContext 保存卡密删除 Hook 和库存刷新替身。
    const { hook, loadCards } = createCardHook();
    vi.mocked(window.confirm).mockReturnValueOnce(false);
    await act(/* cancelDeleteAction 取消卡密删除确认。 */ async () => hook.result.current.handleDelete(1));
    expect(cardActionMocks.deleteCard).not.toHaveBeenCalled();

    cardActionMocks.deleteCard.mockRejectedValueOnce(new Error('删除失败'));
    await act(/* errorDeleteAction 执行失败的卡密删除。 */ async () => hook.result.current.handleDelete(1));
    expect(window.alert).toHaveBeenCalledWith('删除失败');
    expect(loadCards).not.toHaveBeenCalled();

    cardActionMocks.updateCard.mockRejectedValueOnce(new Error('状态失败'));
    await act(/* toggleAction 执行失败的卡密启停切换。 */ async () => hook.result.current.toggleCardStatus(cardFixture));
    expect(window.alert).toHaveBeenCalledWith('状态失败');
    expect(loadCards).not.toHaveBeenCalled();
  });

  test('状态切换不提交可能已经过期的数据卡库存', /* 当前回调验证启停动作与自动发货库存并发隔离。 */ async () => {
    // actionContext 保存 data 卡片状态切换测试的 Hook 和刷新替身。
    const { hook } = createCardHook();
    await act(/* toggleAction 执行 data 卡启停切换。 */ async () => hook.result.current.toggleCardStatus(cardFixture));
    expect(cardActionMocks.updateCard).toHaveBeenCalledWith(1, expect.objectContaining({ enabled: false }));
    // mutation 保存状态切换实际提交的字段，库存字段必须为 undefined 以便 JSON 序列化时省略。
    const mutation = cardActionMocks.updateCard.mock.calls[0]?.[1];
    expect(mutation).toHaveProperty('data_content', undefined);
  });

  test('仅编辑数据卡元数据时不重复提交未修改库存', /* 当前回调验证数据卡普通编辑与并发出库隔离。 */ async () => {
    // actionContext 保存 data 卡编辑动作的 Hook 和刷新替身。
    const { hook } = createCardHook();
    await act(/* editAction 打开 data 卡编辑草稿。 */ async () => hook.result.current.handleEdit(cardFixture));
    act(/* nameAction 只修改 data 卡名称。 */ () => hook.result.current.setEditForm(current => ({ ...current, name: '库存一改名' })));
    await act(/* saveAction 提交不含库存替换的普通编辑。 */ async () => hook.result.current.handleSaveEdit());
    expect(cardActionMocks.updateCard).toHaveBeenCalledWith(1, expect.objectContaining({ name: '库存一改名' }));
    // mutation 保存普通编辑实际提交的字段，未修改库存不可写回服务端。
    const mutation = cardActionMocks.updateCard.mock.calls[0]?.[1];
    expect(mutation).not.toHaveProperty('data_content');
  });

  test('修改数据卡库存时附带打开时的库存快照', /* 当前回调验证库存替换携带并发守卫快照。 */ async () => {
    // actionContext 保存 data 卡库存编辑的 Hook 和刷新替身。
    const { hook, loadCards } = createCardHook();
    await act(/* editAction 打开 data 卡编辑草稿。 */ async () => hook.result.current.handleEdit(cardFixture));
    act(/* stockAction 在原库存后追加一行。 */ () => hook.result.current.setEditForm(current => ({ ...current, data_content: 'A\nB\nC' })));
    await act(/* saveAction 提交库存替换。 */ async () => hook.result.current.handleSaveEdit());
    expect(cardActionMocks.updateCard).toHaveBeenCalledWith(1, expect.objectContaining({ data_content: 'A\nB\nC', expected_data_content: 'A\nB' }));
    expect(loadCards).toHaveBeenCalledTimes(1);
  });

  test('库存在编辑期间被自动发货消费时提示冲突并保留弹窗', /* 当前回调验证服务端 409 冲突不会关闭编辑草稿。 */ async () => {
    // actionContext 保存冲突场景的 Hook 和刷新替身。
    const { hook, loadCards } = createCardHook();
    cardActionMocks.updateCard.mockRejectedValueOnce(new Error('库存已被自动发货或其他操作更新，请刷新后重新编辑'));
    await act(/* editAction 打开 data 卡编辑草稿。 */ async () => hook.result.current.handleEdit(cardFixture));
    act(/* stockAction 修改库存正文。 */ () => hook.result.current.setEditForm(current => ({ ...current, data_content: 'A\nB\nC' })));
    await act(/* saveAction 提交被服务端拒绝的库存替换。 */ async () => hook.result.current.handleSaveEdit());
    expect(window.alert).toHaveBeenCalledWith('库存已被自动发货或其他操作更新，请刷新后重新编辑');
    expect(hook.result.current.showEditModal).toBe(true);
    expect(loadCards).not.toHaveBeenCalled();
  });

  test('复制卡密标识和下载模板均提供浏览器动作', /* 当前回调验证卡密复制和模板下载边界。 */ async () => {
    // actionContext 保存卡密展示动作 Hook 和库存刷新替身。
    const { hook } = createCardHook();
    // writeText 保存剪贴板写入替身。
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    await act(/* copyAction 复制卡密组标识。 */ async () => hook.result.current.copyCardID(1));
    expect(writeText).toHaveBeenCalledWith('1');
    expect(window.alert).toHaveBeenCalledWith('已复制卡密组ID：1');

    // createObjectURL 替换模板文件对象地址创建动作。
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:card-template');
    // revokeObjectURL 替换模板临时地址释放动作。
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(/* revokeAction 释放模板临时地址。 */ () => undefined);
    // click 替换模板下载链接触发动作。
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(/* clickAction 触发模板下载。 */ () => undefined);
    hook.result.current.downloadCardTemplate();
    expect(createObjectURL).toHaveBeenCalled();
    expect(click).toHaveBeenCalled();
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:card-template');
  });
});
