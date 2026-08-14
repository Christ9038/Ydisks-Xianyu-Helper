import { useCallback, useEffect, useRef, useState } from 'react';
import type { AccountTaskSettings, AccountTaskSummary } from '../../../types';
import { getAccountTaskSettings, runAccountTask, updateAccountTaskSettings } from './api';
import { accountTaskErrorMessage, buildAccountTaskDefaults, canStartAccountTask, isAccountTaskAbortError, isCurrentAccountTaskRequest } from './state';
import type { AccountAutomationOptions, AccountAutomationState, AccountTaskType } from './types';

/** AccountAutomation Hook 的完整返回值。 */
export type UseAccountAutomationResult = AccountAutomationState & {
  /** 更新任务设置草稿。 */
  setForm: React.Dispatch<React.SetStateAction<AccountTaskSettings>>;
  /** 保存任务设置。 */
  save: () => Promise<void>;
  /** 立即运行指定任务。 */
  run: (taskType: AccountTaskType) => Promise<void>;
  /** 重试最近一次失败动作。 */
  retry: () => Promise<void>;
};

/** 管理账号自动评价和自动擦亮的设置、执行、取消与重试。 */
export const useAccountAutomation = ({ account, onSaved }: AccountAutomationOptions): UseAccountAutomationResult => {
  // form 保存当前账号任务设置草稿。
  const [form, setForm] = useState<AccountTaskSettings>(() => buildAccountTaskDefaults(account));
  // loading 表示账号任务设置是否正在读取。
  const [loading, setLoading] = useState(true);
  // saving 表示任务设置是否正在保存。
  const [saving, setSaving] = useState(false);
  // running 保存当前执行中的任务类型。
  const [running, setRunning] = useState<'' | AccountTaskType>('');
  // error 保存最近一次任务操作错误。
  const [error, setError] = useState('');
  // summary 保存最近一次任务执行统计。
  const [summary, setSummary] = useState<AccountTaskSummary | null>(null);
  // retryAction 保存最近一次失败动作。
  const [retryAction, setRetryAction] = useState<(() => Promise<void>) | null>(null);
  // requestSequence 隔离账号切换后的旧响应。
  const requestSequence = useRef(0);
  // requestController 保存当前账号任务请求控制器。
  const requestController = useRef<AbortController | null>(null);

  useEffect(() => {
    const sequence = ++requestSequence.current;
    requestController.current?.abort();
    const controller = new AbortController();
    requestController.current = controller;
    setForm(buildAccountTaskDefaults(account));
    setSummary(null);
    setError('');
    setSaving(false);
    setRunning('');
    setRetryAction(null);
    setLoading(true);
    getAccountTaskSettings(account.id, { signal: controller.signal }).then(settings => {
      if (!isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal)) return;
      setForm(settings);
    }).catch(loadError => {
      if (isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal) && !isAccountTaskAbortError(loadError)) setError(accountTaskErrorMessage(loadError, '加载任务设置失败'));
    }).finally(() => {
      if (isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal)) setLoading(false);
    });
    return () => controller.abort();
  }, [account.id]);

  useEffect(() => () => requestController.current?.abort(), []);

  /** 保存任务设置并在成功后同步账号列表。 */
  const save = useCallback(async (): Promise<void> => {
    if (!canStartAccountTask(saving, running)) return;
    const sequence = ++requestSequence.current;
    requestController.current?.abort();
    const controller = new AbortController();
    requestController.current = controller;
    setSaving(true);
    setError('');
    setRetryAction(null);
    try {
      const stored = await updateAccountTaskSettings(account.id, form, { signal: controller.signal });
      if (!isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal)) return;
      setForm(stored);
      onSaved(stored);
    } catch (saveError) {
      if (isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal) && !isAccountTaskAbortError(saveError)) {
        setError(accountTaskErrorMessage(saveError, '保存失败'));
        setRetryAction(() => save);
      }
    } finally {
      if (isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal)) setSaving(false);
    }
  }, [account.id, form, onSaved, running, saving]);

  /** 保存设置后立即执行指定账号任务并刷新结果。 */
  const run = useCallback(async (taskType: AccountTaskType): Promise<void> => {
    if (!canStartAccountTask(saving, running) || !account.enabled) return;
    const sequence = ++requestSequence.current;
    requestController.current?.abort();
    const controller = new AbortController();
    requestController.current = controller;
    setRunning(taskType);
    setError('');
    setSummary(null);
    setRetryAction(null);
    try {
      await updateAccountTaskSettings(account.id, form, { signal: controller.signal });
      const result = await runAccountTask(account.id, taskType, { signal: controller.signal });
      const stored = await getAccountTaskSettings(account.id, { signal: controller.signal });
      if (!isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal)) return;
      setSummary(result.summary);
      setForm(stored);
      onSaved(stored);
    } catch (runError) {
      if (isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal) && !isAccountTaskAbortError(runError)) {
        setError(accountTaskErrorMessage(runError, '任务执行失败'));
        setRetryAction(() => () => run(taskType));
      }
    } finally {
      if (isCurrentAccountTaskRequest(requestSequence.current, sequence, controller.signal)) setRunning('');
    }
  }, [account.enabled, account.id, form, onSaved, running, saving]);

  /** 重试最近一次保存或执行动作。 */
  const retry = useCallback(async (): Promise<void> => {
    if (retryAction) await retryAction();
  }, [retryAction]);

  return { form, loading, saving, running, summary, error, retryAvailable: retryAction !== null, setForm, save, run, retry };
};
