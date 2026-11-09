// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import { CategoryCandidatePicker } from './CategoryCandidatePicker';

// candidates 是类目推荐返回的候选类目样本，包含需要被用户改选的“游戏软件平台”。
const candidates = [
  { cat_id: '1', cat_name: '电子资料', channel_cat_id: '11' },
  { cat_id: '2', cat_name: '游戏软件平台', channel_cat_id: '22', tb_cat_id: '222' },
];

describe('CategoryCandidatePicker', /* testGroup 验证候选类目列表的展示与改选。 */ () => {
  afterEach(/* cleanupAction 清理每个测试的 DOM。 */ () => cleanup());

  test('展示全部候选并高亮当前选中项，点击后回传完整类目', /* testCase 验证改选行为。 */ () => {
    // onSelect 记录用户点选的候选类目。
    const onSelect = vi.fn();
    render(<CategoryCandidatePicker candidates={candidates} selectedCatId="1" onSelect={onSelect} />);
    expect(screen.getByRole('button', { name: '电子资料' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByRole('button', { name: '游戏软件平台' }).getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: '游戏软件平台' }));
    expect(onSelect).toHaveBeenCalledWith(candidates[1]);
  });

  test('没有选中类目时不渲染，避免展示已被清除的旧候选', /* testCase 验证清除选择后隐藏候选。 */ () => {
    // view 是未选中类目时的渲染结果。
    const view = render(<CategoryCandidatePicker candidates={candidates} onSelect={vi.fn()} />);
    expect(view.container.innerHTML).toBe('');
  });

  test('候选少于两项时不渲染', /* testCase 验证无可改选内容时不占位。 */ () => {
    // view 是只有主推荐时的渲染结果。
    const view = render(<CategoryCandidatePicker candidates={[candidates[0]]} selectedCatId="1" onSelect={vi.fn()} />);
    expect(view.container.innerHTML).toBe('');
  });
});
