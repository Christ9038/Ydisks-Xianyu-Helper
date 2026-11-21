// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item } from '../api';
import { getItemStock,updateItemStock } from '../api';
import { ItemStockModal } from './ItemStockModal';

vi.mock('../api', /* stockApiMockFactory 提供弹窗测试的 API 替身。 */ () => ({
  getItemStock: vi.fn(),
  updateItemStock: vi.fn(),
  isItemStockChangedError: /* changedMock 以消息标记识别库存已变化。 */ (error: unknown) => error instanceof Error && error.message === 'changed',
  itemErrorMessage: /* errorMessageMock 把异常转换为稳定文本。 */ (error: unknown, fallback: string) => (error instanceof Error ? error.message : fallback),
}));

// item 是弹窗测试使用的商品样本。
const item = { cookie_id: 'acc', item_id: 'a', item_title: '测试商品' } as Item;

describe('ItemStockModal', /* modalSuite 验证单规格、多规格与失败展示。 */ () => {
  beforeEach(/* resetMocks 重置替身。 */ () => { vi.resetAllMocks(); });
  afterEach(cleanup);

  test('单规格：修改并保存', /* singleCase 验证输入框与提交。 */ async () => {
    vi.mocked(getItemStock).mockResolvedValue({ itemId: 'a', multiSpec: false, quantity: 5, skus: [] });
    vi.mocked(updateItemStock).mockResolvedValue(undefined);
    render(<ItemStockModal item={item} open onClose={vi.fn()} />);
    // input 是单规格库存输入框。
    const input = await screen.findByLabelText('库存数量');
    fireEvent.change(input, { target: { value: '9' } });
    fireEvent.click(screen.getByRole('button', { name: '保存库存' }));
    expect(await screen.findByText('库存已更新。')).toBeTruthy();
    expect(updateItemStock).toHaveBeenCalledWith('acc', 'a', { quantity: 9, expectedQuantity: 5 }, expect.anything());
  });

  test('多规格：逐行展示并显示库存已变化后的重读入口', /* multiCase 验证多行与 409 提示。 */ async () => {
    vi.mocked(getItemStock).mockResolvedValue({ itemId: 'a', multiSpec: true, quantity: 7, skus: [{ key: 'k1', label: '红色', priceCents: 100, quantity: 3 }, { key: 'k2', label: '蓝色', priceCents: 250, quantity: 4 }] });
    vi.mocked(updateItemStock).mockRejectedValue(new Error('changed'));
    render(<ItemStockModal item={item} open onClose={vi.fn()} />);
    fireEvent.change(await screen.findByLabelText('红色 库存'), { target: { value: '6' } });
    expect(screen.getByText('¥2.50 · 当前 4')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存库存' }));
    expect(await screen.findByRole('button', { name: '重新读取最新库存' })).toBeTruthy();
    expect((screen.getByRole('button', { name: '保存库存' }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('读取失败显示错误与重新加载', /* failCase 验证失败视图。 */ async () => {
    vi.mocked(getItemStock).mockRejectedValue(new Error('不支持网页端编辑'));
    render(<ItemStockModal item={item} open onClose={vi.fn()} />);
    expect(await screen.findByText('不支持网页端编辑')).toBeTruthy();
    expect((screen.getByRole('button', { name: '保存库存' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByRole('button', { name: '重新加载' })).toBeTruthy();
  });

  test('关闭按钮触发 onClose；未打开不渲染', /* closeCase 验证渲染条件。 */ async () => {
    vi.mocked(getItemStock).mockResolvedValue({ itemId: 'a', multiSpec: false, quantity: 1, skus: [] });
    // onClose 是关闭回调。
    const onClose = vi.fn();
    // view 是弹窗渲染句柄。
    const view = render(<ItemStockModal item={item} open onClose={onClose} />);
    await waitFor(/* inputReady 断言库存输入框已出现。 */ () => expect(screen.getByLabelText('库存数量')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: '关闭库存调整' }));
    expect(onClose).toHaveBeenCalled();
    view.rerender(<ItemStockModal item={null} open onClose={onClose} />);
    expect(screen.queryByLabelText('库存数量')).toBeNull();
  });
});
