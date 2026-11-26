// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach,describe,expect,test } from 'vitest';
import type { ReviewPlan } from '../reviewPlan';
import { defaultReviewCopyPlan } from '../reviewPlan';
import ReviewPlanEditor from './ReviewPlanEditor';

/** initialPlan 是测试起点：引导语启用，其余停用。 */
const initialPlan: ReviewPlan = {
  lead: { enabled: true, text: '你好' },
  copy: defaultReviewCopyPlan(),
  tail: { enabled: false, text: '提示' },
  intervalMin: 5,
  intervalMax: 10,
};

/** latest 记录编辑器最近一次回调的完整模型，用于断言受控更新结果。 */
let latest: ReviewPlan = initialPlan;

/** Harness 用真实状态承载受控编辑器。 */
const Harness = () => {
  // plan 是当前编辑模型。
  const [plan, setPlan] = useState<ReviewPlan>(initialPlan);
  latest = plan;
  return <ReviewPlanEditor plan={plan} onChange={setPlan} />;
};

describe('ReviewPlanEditor', /* 当前回调验证三段消息编辑器的交互。 */ () => {
  afterEach(/* 当前回调清理 DOM。 */ () => cleanup());

  test('启用好评文案后可增删排序模板并切换来源', /* 当前回调验证模板池交互。 */ () => {
    render(<Harness />);
    expect(screen.queryByText('添加模板')).toBeNull();
    fireEvent.click(screen.getByLabelText(/消息 2/));
    fireEvent.click(screen.getByText('添加模板'));
    fireEvent.click(screen.getByText('添加模板'));
    fireEvent.change(screen.getByLabelText('好评模板 1'), { target: { value: '甲' } });
    fireEvent.change(screen.getByLabelText('好评模板 2'), { target: { value: '乙' } });
    fireEvent.click(screen.getByLabelText('下移模板 1'));
    expect(latest.copy.pool).toEqual(['乙', '甲']);
    fireEvent.click(screen.getByLabelText('上移模板 1'));
    expect(latest.copy.pool).toEqual(['乙', '甲']);
    fireEvent.click(screen.getByLabelText('删除模板 1'));
    expect(latest.copy.pool).toEqual(['甲']);
    fireEvent.click(screen.getByLabelText(/随机选取/));
    expect(latest.copy.random).toBe(false);

    fireEvent.click(screen.getByLabelText('混合'));
    expect(latest.copy.source).toBe('mixed');
    fireEvent.change(screen.getByLabelText('AI 使用比例'), { target: { value: '70' } });
    fireEvent.change(screen.getByLabelText('AI 评价风格'), { target: { value: 'warm' } });
    fireEvent.change(screen.getByLabelText('AI 最少字数'), { target: { value: '30' } });
    fireEvent.change(screen.getByLabelText('AI 最多字数'), { target: { value: '80' } });
    fireEvent.change(screen.getByLabelText('AI 附加要求'), { target: { value: '提包装' } });
    expect(latest.copy).toMatchObject({ aiRatio: 70, aiStyle: 'warm', aiMinChars: 30, aiMaxChars: 80, aiPrompt: '提包装' });

    fireEvent.click(screen.getByLabelText('AI 生成'));
    expect(screen.queryByText('添加模板')).toBeNull();
    fireEvent.click(screen.getByLabelText('固定模板'));
    expect(screen.getByText('添加模板')).toBeTruthy();
  });

  test('引导语、复制提示与间隔输入', /* 当前回调验证普通文本与间隔联动。 */ () => {
    render(<Harness />);
    fireEvent.change(screen.getByLabelText('引导语'), { target: { value: '亲，麻烦评价' } });
    expect(latest.lead.text).toBe('亲，麻烦评价');
    fireEvent.click(screen.getByLabelText(/消息 3/));
    fireEvent.change(screen.getByLabelText('复制提示'), { target: { value: '复制上面的' } });
    expect(latest.tail).toEqual({ enabled: true, text: '复制上面的' });
    fireEvent.click(screen.getByLabelText(/消息 1/));
    expect(latest.lead.enabled).toBe(false);

    fireEvent.change(screen.getByLabelText('消息间隔最小秒数'), { target: { value: '20' } });
    expect([latest.intervalMin, latest.intervalMax]).toEqual([20, 20]);
    fireEvent.change(screen.getByLabelText('消息间隔最大秒数'), { target: { value: '30' } });
    expect(latest.intervalMax).toBe(30);
    fireEvent.change(screen.getByLabelText('消息间隔最大秒数'), { target: { value: 'abc' } });
    expect(latest.intervalMax).toBe(0);
  });
});
