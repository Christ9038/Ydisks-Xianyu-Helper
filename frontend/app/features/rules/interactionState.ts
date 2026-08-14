// RuleSubmitState 描述规则保存动作的短暂提交状态。
export interface RuleSubmitState {
  // submitting 表示当前是否已有保存请求在执行。
  submitting: boolean;
  // result 表示最近一次保存结果，便于行为测试描述成功和失败。
  result: 'idle' | 'success' | 'failure';
}

// idleRuleSubmitState 创建一个可提交的初始状态。
export const idleRuleSubmitState = (): RuleSubmitState => ({ submitting: false, result: 'idle' });

// startRuleSubmission 只允许第一个保存请求进入执行态，阻断重复提交。
export const startRuleSubmission = (state: RuleSubmitState): RuleSubmitState =>
  state.submitting ? state : { submitting: true, result: 'idle' };

// finishRuleSubmission 将保存请求收口为成功或失败结果。
export const finishRuleSubmission = (state: RuleSubmitState, succeeded: boolean): RuleSubmitState => ({
  submitting: false,
  result: succeeded ? 'success' : 'failure',
});

// nextRequestGeneration 为新的筛选或账号请求生成单调递增的代次。
export const nextRequestGeneration = (generation: number): number => generation + 1;

// isCurrentRequest 判断响应是否仍属于当前筛选条件对应的请求。
export const isCurrentRequest = (requestGeneration: number, currentGeneration: number, requestKey: string, currentKey: string): boolean =>
  requestGeneration === currentGeneration && requestKey === currentKey;

// selectRulesTab 处理页签切换，保证未知值不会污染当前页签状态。
export const selectRulesTab = (tab: string): 'automation' | 'reply' | 'default' =>
  tab === 'reply' || tab === 'default' ? tab : 'automation';
