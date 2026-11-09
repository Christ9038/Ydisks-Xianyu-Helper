// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import * as api from '../api';
import { PublishCategoryField } from './PublishCategoryField';

vi.mock('../api', async importOriginal => ({ ...(await importOriginal<typeof import('../api')>()), recommendPublishCategory: vi.fn() }));

// first、second 是推荐返回的主推荐与候选类目。
const first = { cat_id: '1', cat_name: '电子资料', channel_cat_id: '11' };
// second 是推荐返回的第二候选类目，用于验证改选。
const second = { cat_id: '2', cat_name: '游戏软件平台', channel_cat_id: '22' };

describe('PublishCategoryField', /* 类目推荐字段的成功、失败与取消。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => vi.clearAllMocks());
  afterEach(/* 卸载渲染树。 */ () => cleanup());

  test('按关键词推荐后选中主推荐，并可改选候选与清除', /* 成功路径。 */ async () => {
    vi.mocked(api.recommendPublishCategory).mockResolvedValue({ success: true, category: first, candidates: [first, second] } as never);
    // onChange 记录回传的类目。
    const onChange = vi.fn();
    // view 保存渲染结果，用于受控值更新后重渲染。
    const view = render(<PublishCategoryField cookieId="a1" value={null} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('类目关键词'), { target: { value: '课程' } });
    fireEvent.click(screen.getByText('获取类目'));
    await waitFor(/* 等待回传主推荐。 */ () => expect(onChange).toHaveBeenCalledWith(first));
    expect(api.recommendPublishCategory).toHaveBeenCalledWith('a1', '课程', expect.anything());
    view.rerender(<PublishCategoryField cookieId="a1" value={first} onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: '游戏软件平台' }));
    expect(onChange).toHaveBeenLastCalledWith(second);
    fireEvent.click(screen.getByText('清除，使用自动识别'));
    expect(onChange).toHaveBeenLastCalledWith(null);
  });

  test('缺少账号或关键词时不发请求；推荐失败展示原因', /* 校验与失败路径。 */ async () => {
    // view 保存无账号时的渲染结果。
    const view = render(<PublishCategoryField cookieId="" value={null} onChange={vi.fn()} />);
    expect((screen.getByText('获取类目') as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<PublishCategoryField cookieId="a1" value={null} onChange={vi.fn()} />);
    fireEvent.change(screen.getByLabelText('类目关键词'), { target: { value: '课程' } });
    vi.mocked(api.recommendPublishCategory).mockRejectedValue(new Error('推荐失败'));
    fireEvent.click(screen.getByText('获取类目'));
    await waitFor(/* 等待错误提示。 */ () => expect(screen.getByRole('alert').textContent).toContain('推荐失败'));
  });

  test('卸载后晚到的推荐响应不会回传', /* 取消语义。 */ async () => {
    // resolveLater 在卸载后才完成推荐请求。
    let resolveLater: (value: never) => void = () => undefined;
    vi.mocked(api.recommendPublishCategory).mockReturnValue(new Promise(/* executor 暂存 resolve。 */ resolve => { resolveLater = resolve as never; }));
    // onChange 记录回传。
    const onChange = vi.fn();
    // view 保存渲染结果。
    const view = render(<PublishCategoryField cookieId="a1" value={null} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('类目关键词'), { target: { value: '课程' } });
    fireEvent.click(screen.getByText('获取类目'));
    view.unmount();
    resolveLater({ success: true, category: first } as never);
    await Promise.resolve();
    expect(onChange).not.toHaveBeenCalled();
  });
});
