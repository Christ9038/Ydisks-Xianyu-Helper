// @vitest-environment jsdom
import { act,cleanup,fireEvent,render,screen,within } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item } from '../models';
import ItemMultiSelect from './ItemMultiSelect';

afterEach(/* 当前回调清理商品多选下拉测试 DOM 与假计时器。 */ () => {
  cleanup();
  vi.useRealTimers();
});

beforeEach(/* 当前回调在每个用例前启用假计时器以验证防抖。 */ () => {
  vi.useFakeTimers();
  // 组件使用 autoFocus，jsdom 需要聚焦实现存在。
  if (!HTMLElement.prototype.focus) {
    HTMLElement.prototype.focus = /* 当前回调提供空聚焦实现。 */ () => {};
  }
});

// buildItems 构造本地下拉候选商品列表。
const buildItems = (): Item[] => [
  { id: 1, cookie_id: 'acc1', item_id: 'item-1', item_title: '会员卡' },
  { id: 2, cookie_id: 'acc1', item_id: 'item-2', item_title: '优惠券' },
  { id: 3, cookie_id: 'acc1', item_id: 'item-3', item_title: '充值服务' },
];

// triggerElement 返回多选下拉的触发器节点，避免与面板内按钮重名。
const triggerElement = (): HTMLElement => {
  // trigger 是唯一带 listbox 弹出语义的触发器节点。
  const trigger = document.querySelector('[aria-haspopup="listbox"]');
  if (!trigger) throw new Error('未找到多选下拉触发器');
  return trigger as HTMLElement;
};

// openDropdown 展开下拉面板并返回面板容器。
const openDropdown = (): HTMLElement => {
  fireEvent.click(triggerElement());
  return screen.getByRole('listbox');
};

describe('ItemMultiSelect', /* 当前回调验证关联商品多选下拉的搜索、多选与关闭行为。 */ () => {
  test('多选不自动关闭下拉，点击组件外部才关闭', /* 当前回调验证多选过程中的展开状态保持。 */ () => {
    // onChange 是记录选中集合变化的回调替身。
    const onChange = vi.fn();
    render(<ItemMultiSelect options={buildItems()} value={[]} onChange={onChange} />);

    // listbox 是展开后的商品候选面板。
    const listbox = openDropdown();
    fireEvent.click(within(listbox).getByRole('option', { name: '会员卡' }));
    expect(onChange).toHaveBeenCalledWith(['item-1']);
    // 多选后下拉仍然展开。
    expect(screen.getByRole('listbox')).toBeTruthy();

    // 点击组件外部才收起下拉。
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  test('点击搜索框不关闭下拉', /* 当前回调验证搜索框交互不触发展开状态切换。 */ () => {
    render(<ItemMultiSelect options={buildItems()} value={[]} onChange={vi.fn()} />);

    openDropdown();
    // searchBox 是下拉面板内的本地搜索框。
    const searchBox = screen.getByPlaceholderText('搜索本地商品标题或商品 ID');
    fireEvent.mouseDown(searchBox);
    fireEvent.click(searchBox);

    expect(screen.getByRole('listbox')).toBeTruthy();
  });

  test('搜索按防抖在本地过滤且不发起网络请求', /* 当前回调验证本地搜索的防抖节流与无远程调用。 */ () => {
    // fetchSpy 用于确认本地搜索不会触发任何网络请求。
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    render(<ItemMultiSelect options={buildItems()} value={[]} onChange={vi.fn()} />);

    openDropdown();
    // searchBox 是下拉面板内的本地搜索框。
    const searchBox = screen.getByPlaceholderText('搜索本地商品标题或商品 ID');
    fireEvent.change(searchBox, { target: { value: '优惠' } });

    // 防抖窗口未结束时候选列表保持原样。
    expect(screen.getByRole('option', { name: '会员卡' })).toBeTruthy();
    act(/* 当前回调推进假计时器触发防抖更新。 */ () => vi.advanceTimersByTime(300));
    expect(screen.queryByRole('option', { name: '会员卡' })).toBeNull();
    expect(screen.getByRole('option', { name: '优惠券' })).toBeTruthy();
    expect(fetchSpy).not.toHaveBeenCalled();
    fetchSpy.mockRestore();
  });

  test('搜索无匹配时展示空状态', /* 当前回调验证本地过滤不命中时的占位文案。 */ () => {
    render(<ItemMultiSelect options={buildItems()} value={[]} onChange={vi.fn()} />);

    openDropdown();
    fireEvent.change(screen.getByPlaceholderText('搜索本地商品标题或商品 ID'), { target: { value: '不存在' } });
    act(/* 当前回调推进假计时器触发防抖更新。 */ () => vi.advanceTimersByTime(300));

    expect(screen.getByText('没有匹配的本地商品')).toBeTruthy();
  });

  test('已选商品以标签展示并可单独移除', /* 当前回调验证已选集合的标签渲染与移除行为。 */ () => {
    // onChange 是记录选中集合变化的回调替身。
    const onChange = vi.fn();
    render(<ItemMultiSelect options={buildItems()} value={['item-1', 'item-2']} onChange={onChange} />);

    // 已选项以商品标题展示标签。
    expect(screen.getByText('会员卡')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '移除 会员卡' }));
    expect(onChange).toHaveBeenCalledWith(['item-2']);  });

  test('无可读标题时回退展示商品标识', /* 当前回调验证标题缺失时的标识回退。 */ () => {
    // items 是缺少标题的候选列表。
    const items: Item[] = [{ id: 9, cookie_id: 'acc1', item_id: 'item-9' }];
    render(<ItemMultiSelect options={items} value={['item-9']} onChange={vi.fn()} />);

    expect(screen.getByText('item-9')).toBeTruthy();
  });

  test('只读时不展开且不回传变更', /* 当前回调验证 disabled 状态下控件不可交互。 */ () => {
    // onChange 是记录选中集合变化的回调替身。
    const onChange = vi.fn();
    render(<ItemMultiSelect options={buildItems()} value={['item-1']} onChange={onChange} disabled />);

    fireEvent.click(triggerElement());
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
  });

  test('清空按钮回传空集合', /* 当前回调验证清空已选商品的行为。 */ () => {
    // onChange 是记录选中集合变化的回调替身。
    const onChange = vi.fn();
    render(<ItemMultiSelect options={buildItems()} value={['item-1']} onChange={onChange} />);

    openDropdown();
    fireEvent.click(screen.getByRole('button', { name: '清空' }));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  test('键盘回车可切换下拉展开状态', /* 当前回调验证触发器键盘操作。 */ () => {
    render(<ItemMultiSelect options={buildItems()} value={[]} onChange={vi.fn()} />);

    // trigger 是多选下拉的触发器节点。
    const trigger = screen.getByRole('button', { name: '账号级回复（不限定商品）' });
    fireEvent.keyDown(trigger, { key: 'Enter' });
    expect(screen.getByRole('listbox')).toBeTruthy();
    fireEvent.keyDown(trigger, { key: ' ' });
    expect(screen.queryByRole('listbox')).toBeNull();
  });
});
