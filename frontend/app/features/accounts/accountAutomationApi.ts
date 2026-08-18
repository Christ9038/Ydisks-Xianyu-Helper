import type { AccountTaskRunResponseEnvelope,AccountTaskSettings,AccountTaskSettingsResponse } from './api';
import { get,post,put,type RequestControlOptions } from '../../../shared/http/client';

/** 获取指定账号的自动评价与自动擦亮设置。 */
export const getAccountTaskSettings = async (accountID: string, options?: RequestControlOptions): Promise<AccountTaskSettingsResponse> => get(`/api/v1/account-tasks/${accountID}`, undefined, options);

/** 保存指定账号的自动评价与自动擦亮设置。 */
export const updateAccountTaskSettings = async (accountID: string, settings: AccountTaskSettings, options?: RequestControlOptions): Promise<AccountTaskSettingsResponse> => put(`/api/v1/account-tasks/${accountID}`, settings, options);

/** 立即发起账号级计划任务，并使用较长超时覆盖平台执行窗口。 */
export const runAccountTask = async (accountID: string, taskType: 'auto_rate' | 'auto_polish', options?: RequestControlOptions): Promise<AccountTaskRunResponseEnvelope> => post(`/api/v1/account-tasks/${accountID}/run`, { task_type: taskType }, { timeoutMs: 120_000, ...options });
