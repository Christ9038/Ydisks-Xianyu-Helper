import { describe,expect,test } from 'vitest';
import {
finishRuleSubmission,
idleRuleSubmitState,
isCurrentRequest,
nextRequestGeneration,
selectRulesTab,
startRuleSubmission,
} from './interactionState';

// 规则交互行为测试覆盖保存、失败恢复、取消旧请求和页签切换。
describe('Rules feature interaction state',
  // 测试组回调集中验证 Rules feature 的交互状态机。
  () => {
  // 成功保存后应清除提交锁并记录成功结果。
  test('records a successful save',
    // 成功场景回调验证保存状态闭环。
    () => {
    // submitting 是已进入执行态的保存状态。
    const submitting = startRuleSubmission(idleRuleSubmitState());
    expect(finishRuleSubmission(submitting, true)).toEqual({ submitting: false, result: 'success' });
    });

  // 保存失败后应允许用户再次提交，而不是永久停留在提交中。
  test('recovers after a failed save',
    // 失败场景回调验证提交锁能够释放。
    () => {
    // submitting 是已进入执行态的保存状态。
    const submitting = startRuleSubmission(idleRuleSubmitState());
    expect(finishRuleSubmission(submitting, false)).toEqual({ submitting: false, result: 'failure' });
    });

  // 已有保存请求时重复点击必须保持原状态，避免产生第二个请求。
  test('blocks repeated submission while a save is in flight',
    // 重复提交场景回调验证第二次点击不会创建新状态。
    () => {
    // submitting 是第一次点击创建的执行态。
    const submitting = startRuleSubmission(idleRuleSubmitState());
    expect(startRuleSubmission(submitting)).toBe(submitting);
    });

  // 新请求代次产生后，旧请求响应应视为已取消并被忽略。
  test('ignores an expired response after a newer request starts',
    // 过期响应场景回调验证请求代次门禁。
    () => {
    // currentGeneration 是第二次请求产生的最新代次。
    const currentGeneration = nextRequestGeneration(nextRequestGeneration(0));
    expect(isCurrentRequest(1, currentGeneration, 'account-a', 'account-b')).toBe(false);
    expect(isCurrentRequest(currentGeneration, currentGeneration, 'account-a', 'account-a')).toBe(true);
    });

  // 页签切换只允许规则页支持的三个值进入状态。
  test('keeps tab switching inside the Rules feature',
    // 切换场景回调验证页签值不会越过 feature 边界。
    () => {
    expect(selectRulesTab('reply')).toBe('reply');
    expect(selectRulesTab('default')).toBe('default');
    expect(selectRulesTab('unknown')).toBe('automation');
    });
  });
