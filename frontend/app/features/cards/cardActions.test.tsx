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
}));

vi.mock('./api', /* cardApiMockFactory 提供卡密动作 API 替身。 */ () => ({
  createCard: cardActionMocks.createCard,
  deleteCard: cardActionMocks.deleteCard,
  updateCard: cardActionMocks.updateCard,
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
    cardActionMocks.createCard.mockResolvedValue({ success: true, id: 3 });
    cardActionMocks.deleteCard.mockResolvedValue({ success: true });
    cardActionMocks.updateCard.mockResolvedValue({ success: true });
    vi.spyOn(window, 'alert').mockImplementation(/* alertImplementation 屏蔽卡密动作提示。 */ () => undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  afterEach(/* 当前回调恢复卡密测试浏览器替身。 */ () => {
    vi.restoreAllMocks();
  });

  test('按类型和名称筛选卡密并保存编辑草稿', /* 当前回调验证卡密筛选和编辑提交。 */ async () => {
    // actionContext 保存卡密编辑 Hook 和库存刷新替身。
    const { hook, loadCards } = createCardHook();
    expect(hook.result.current.dataCards).toEqual([cardFixture]);
    act(/* filterAction 筛选文本类型卡密。 */ () => hook.result.current.setTypeFilter('text'));
    expect(hook.result.current.filteredCards).toEqual([textCardFixture]);
    act(/* searchAction 写入卡密名称搜索词。 */ () => hook.result.current.setNameSearch('文案'));
    expect(hook.result.current.filteredCards).toEqual([textCardFixture]);

    act(/* editAction 打开卡密编辑弹窗。 */ () => hook.result.current.handleEdit(textCardFixture));
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
