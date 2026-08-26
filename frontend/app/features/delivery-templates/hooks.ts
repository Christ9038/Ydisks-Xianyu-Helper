import { useCallback, useEffect, useRef, useState } from 'react';
import { createDeliveryTemplate, deleteDeliveryTemplate, listDeliveryTemplates, updateDeliveryTemplate } from './api';
import type { DeliveryTemplate, DeliveryTemplateDraft } from './types';

// DeliveryTemplatesHookResult 描述发货模板页面需要的列表、变更和竞态状态。
export type DeliveryTemplatesHookResult = {
  /** templates 是当前最新列表请求返回的模板。 */
  templates: DeliveryTemplate[];
  /** loading 表示列表请求是否正在等待。 */
  loading: boolean;
  /** saving 表示变更请求是否正在等待。 */
  saving: boolean;
  /** error 是最近一次真实请求的用户可见错误。 */
  error: string;
  /** loadTemplates 取消旧列表请求并加载最新模板。 */
  loadTemplates: () => Promise<void>;
  /** saveTemplate 创建或更新模板并刷新列表。 */
  saveTemplate: (id: number | null, draft: DeliveryTemplateDraft) => Promise<void>;
  /** removeTemplate 删除模板并刷新列表。 */
  removeTemplate: (id: number) => Promise<void>;
};

// isAbortError 判断取消请求错误，避免用户主动取消时弹出失败提示。
const isAbortError = (error: unknown): boolean => error instanceof Error && error.message.includes('取消');

// useDeliveryTemplates 集中管理模板请求的取消、最新代次和变更后唯一刷新。
export const useDeliveryTemplates = (): DeliveryTemplatesHookResult => {
  // templates 保存当前唯一有效列表请求返回的模板数据。
  const [templates, setTemplates] = useState<DeliveryTemplate[]>([]);
  // loading 表示首屏或刷新列表请求正在进行。
  const [loading, setLoading] = useState(true);
  // saving 表示创建、更新或删除操作正在进行，防止重复提交。
  const [saving, setSaving] = useState(false);
  // error 保存最近一次真实请求失败的用户可见错误。
  const [error, setError] = useState('');
  // listControllerRef 保存当前列表请求控制器，开始新列表请求时先取消旧请求。
  const listControllerRef = useRef<AbortController | null>(null);
  // actionControllerRef 保存当前变更请求控制器，卸载时统一取消。
  const actionControllerRef = useRef<AbortController | null>(null);
  // generationRef 标记最新请求代次，旧响应不能覆盖当前页面状态。
  const generationRef = useRef(0);
  // mountedRef 防止卸载后的异步 finally 修改 React 状态。
  const mountedRef = useRef(true);

  // loadTemplates 取消旧列表请求并只允许最新代次写入页面状态。
  const loadTemplates = useCallback(/* loadTemplatesCallback 只允许最新列表请求写入页面状态。 */ async (): Promise<void> => {
    // generation 保存本次列表请求的最新代次。
    const generation = ++generationRef.current;
    listControllerRef.current?.abort();
    // controller 控制本次列表请求的取消生命周期。
    const controller = new AbortController();
    listControllerRef.current = controller;
    if (mountedRef.current) {
      setLoading(true);
      setError('');
    }
    try {
      // result 保存本次列表请求返回的模板数据。
      const result = await listDeliveryTemplates({ signal: controller.signal });
      if (!controller.signal.aborted && mountedRef.current && generation === generationRef.current) setTemplates(result);
    } catch (/* error 是本次列表请求失败原因。 */ error) {
      if (!isAbortError(error) && mountedRef.current && generation === generationRef.current) setError((error as Error).message);
      if (!isAbortError(error) && mountedRef.current && generation === generationRef.current) throw error;
    } finally {
      if (mountedRef.current && generation === generationRef.current) setLoading(false);
    }
  }, []);

  // saveTemplate 串行化模板保存并在成功后刷新一次列表。
  const saveTemplate = useCallback(/* saveTemplateCallback 串行化模板保存并在成功后刷新一次列表。 */ async (id: number | null, draft: DeliveryTemplateDraft): Promise<void> => {
    if (saving) return;
    setSaving(true);
    setError('');
    listControllerRef.current?.abort();
    actionControllerRef.current?.abort();
    // controller 控制本次创建或更新请求的取消生命周期。
    const controller = new AbortController();
    actionControllerRef.current = controller;
    ++generationRef.current;
    try {
      try {
        if (id === null) await createDeliveryTemplate(draft, { signal: controller.signal });
        else await updateDeliveryTemplate(id, draft, { signal: controller.signal });
      } catch (/* error 是本次模板保存请求失败原因。 */ error) {
        if (!isAbortError(error) && mountedRef.current) {
          setError((error as Error).message);
          throw error;
        }
        return;
      }
      if (!controller.signal.aborted && mountedRef.current) {
        try {
          await loadTemplates();
        } catch (/* error 是保存成功后列表刷新失败原因。 */ error) {
          if (!isAbortError(error) && mountedRef.current) setError(`模板已保存，但列表刷新失败：${(error as Error).message}`);
        }
      }
    } finally {
      if (mountedRef.current) setSaving(false);
    }
  }, [loadTemplates, saving]);

  // removeTemplate 串行化删除并在成功后刷新一次列表。
  const removeTemplate = useCallback(/* removeTemplateCallback 串行化删除并在成功后刷新一次列表。 */ async (id: number): Promise<void> => {
    if (saving) return;
    setSaving(true);
    setError('');
    listControllerRef.current?.abort();
    actionControllerRef.current?.abort();
    // controller 控制本次删除请求的取消生命周期。
    const controller = new AbortController();
    actionControllerRef.current = controller;
    ++generationRef.current;
    try {
      try {
        await deleteDeliveryTemplate(id, { signal: controller.signal });
      } catch (/* error 是本次模板删除请求失败原因。 */ error) {
        if (!isAbortError(error) && mountedRef.current) {
          setError((error as Error).message);
          throw error;
        }
        return;
      }
      if (!controller.signal.aborted && mountedRef.current) {
        try {
          await loadTemplates();
        } catch (/* error 是删除成功后列表刷新失败原因。 */ error) {
          if (!isAbortError(error) && mountedRef.current) setError(`模板已删除，但列表刷新失败：${(error as Error).message}`);
        }
      }
    } finally {
      if (mountedRef.current) setSaving(false);
    }
  }, [loadTemplates, saving]);

  useEffect(/* 当前副作用管理组件挂载状态，并在卸载时取消请求和推进代次。 */ () => {
    // mountedRef 在 effect 重新建立时恢复为可写状态，兼容 React StrictMode 的开发期模拟重挂载。
    mountedRef.current = true;
    return /* 当前清理函数释放所有请求控制器并推进代次。 */ () => {
      mountedRef.current = false;
      ++generationRef.current;
      listControllerRef.current?.abort();
      actionControllerRef.current?.abort();
    };
  }, []);

  return { templates, loading, saving, error, loadTemplates, saveTemplate, removeTemplate };
};
