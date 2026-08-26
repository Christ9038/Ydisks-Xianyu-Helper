// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { createDeliveryTemplate, deleteDeliveryTemplate, listDeliveryTemplates, updateDeliveryTemplate } from './api';
import { useDeliveryTemplates } from './hooks';

vi.mock('./api', () => ({
  createDeliveryTemplate: vi.fn(),
  deleteDeliveryTemplate: vi.fn(),
  listDeliveryTemplates: vi.fn(),
  updateDeliveryTemplate: vi.fn(),
}));

// listMock 是模板列表请求的可控测试替身。
const listMock = vi.mocked(listDeliveryTemplates);
// createMock 是模板创建请求的可控测试替身。
const createMock = vi.mocked(createDeliveryTemplate);
// updateMock 是模板更新请求的可控测试替身。
const updateMock = vi.mocked(updateDeliveryTemplate);
// deleteMock 是模板删除请求的可控测试替身。
const deleteMock = vi.mocked(deleteDeliveryTemplate);
// draft 是保存测试使用的最小合法模板草稿。
const draft = { name: '模板', enabled: true, messages: [{ content: '正文' }] };

describe('useDeliveryTemplates', /* 当前测试组验证模板变更与列表刷新错误的分层语义。 */ () => {
  beforeEach(/* 当前回调重置模板 API 替身状态。 */ () => {
    vi.clearAllMocks();
    listMock.mockResolvedValue([]);
    createMock.mockResolvedValue();
    updateMock.mockResolvedValue();
    deleteMock.mockResolvedValue();
  });

  test('创建成功但刷新失败时保持保存成功语义', /* 当前回调验证创建成功不会被刷新错误覆盖。 */ async () => {
    listMock.mockRejectedValueOnce(new Error('刷新失败'));
    // hook 保存模板管理 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建模板管理 Hook 测试实例。 */ () => useDeliveryTemplates());
    await act(/* saveAction 执行一次模板创建并等待列表刷新。 */ async () => {
      await hook.result.current.saveTemplate(null, draft);
    });
    expect(createMock).toHaveBeenCalledTimes(1);
    expect(listMock).toHaveBeenCalledTimes(1);
    expect(hook.result.current.error).toContain('模板已保存');
    hook.unmount();
  });

  test('删除成功但刷新失败时不会重复删除', /* 当前回调验证删除成功不会因刷新错误重复提交。 */ async () => {
    listMock.mockRejectedValueOnce(new Error('刷新失败'));
    // hook 保存模板管理 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建模板管理 Hook 测试实例。 */ () => useDeliveryTemplates());
    await act(/* deleteAction 执行一次模板删除并等待列表刷新。 */ async () => {
      await hook.result.current.removeTemplate(7);
    });
    expect(deleteMock).toHaveBeenCalledTimes(1);
    expect(hook.result.current.error).toContain('模板已删除');
    hook.unmount();
  });

  test('变更请求失败时不刷新列表并继续抛出错误', /* 当前回调验证原始变更失败仍向页面抛出。 */ async () => {
    createMock.mockRejectedValueOnce(new Error('保存失败'));
    // hook 保存模板管理 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建模板管理 Hook 测试实例。 */ () => useDeliveryTemplates());
    await expect(act(/* saveAction 执行一次失败的模板创建。 */ async () => hook.result.current.saveTemplate(null, draft))).rejects.toThrow('保存失败');
    expect(listMock).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('更新成功路径只执行一次更新和刷新', /* 当前回调验证更新成功只触发一次变更和刷新。 */ async () => {
    // hook 保存模板管理 Hook 的当前测试状态。
    const hook = renderHook(/* hookFactory 创建模板管理 Hook 测试实例。 */ () => useDeliveryTemplates());
    await act(/* updateAction 执行一次模板更新并等待列表刷新。 */ async () => {
      await hook.result.current.saveTemplate(9, draft);
    });
    expect(updateMock).toHaveBeenCalledTimes(1);
    expect(listMock).toHaveBeenCalledTimes(1);
    hook.unmount();
  });
});
