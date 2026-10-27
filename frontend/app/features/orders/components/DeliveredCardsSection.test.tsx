// @vitest-environment jsdom
import { act,cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { getOrderDeliveredCards } from '../api';
import type { OrderDeliveredCard } from '../models';
import { DeliveredCardsSection } from './DeliveredCardsSection';

vi.mock('../api', /* deliveredApiMockFactory 提供已发卡密区块的确定性 API 替身。 */ () => ({ getOrderDeliveredCards: vi.fn() }));

// listMock 是订单已发卡密查询替身。
const listMock = vi.mocked(getOrderDeliveredCards);

// cardsFixture 是一条已售和一条待核对的已发卡密。
const cardsFixture: OrderDeliveredCard[] = [
  { id: 1, content: 'KEY-AAA', status: 'sold', cardName: '批量组', deliveredAtMs: 1_700_000_000_000 },
  { id: 2, content: 'KEY-BBB', status: 'reserved', cardName: '批量组', deliveredAtMs: 1_700_000_100_000 },
];

// deferred 创建可由测试手动完成的 Promise，用于构造乱序响应。
const deferred = <T,>() => {
  // resolve 在测试中手动触发。
  let resolve!: (value: T) => void;
  // promise 是挂起的请求。
  const promise = new Promise<T>(/* settle 捕获 Promise 的完成函数。 */ res => { resolve = res; });
  return { promise, resolve };
};

describe('DeliveredCardsSection', /* 当前回调验证订单已发卡密区块的加载、展示、复制、失败重试和取消。 */ () => {
  beforeEach(/* 重置替身。 */ () => {
    vi.clearAllMocks();
  });
  afterEach(/* 卸载渲染树。 */ () => cleanup());

  // 用例：明文展示已发卡密并标注待核对，支持复制。
  test('明文展示已发卡密并标注待核对，支持复制', async () => {
    listMock.mockResolvedValue(cardsFixture);
    // writeText 替换剪贴板写入。
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    render(<DeliveredCardsSection orderId="order-1" />);
    await waitFor(/* 等待卡密渲染。 */ () => expect(screen.getByText('KEY-AAA')).toBeTruthy());
    expect(screen.getByText('KEY-BBB').closest('li')?.textContent).toContain('待核对');
    expect(screen.getByText('KEY-AAA').closest('li')?.textContent).not.toContain('待核对');
    expect(listMock.mock.calls[0][0]).toBe('order-1');

    fireEvent.click(screen.getByLabelText('复制卡密 KEY-AAA'));
    await waitFor(/* 等待复制成功提示。 */ () => expect(screen.getByText('已复制')).toBeTruthy());
    expect(writeText).toHaveBeenCalledWith('KEY-AAA');
  });

  // 用例：剪贴板不可用时不报错也不显示已复制。
  test('剪贴板不可用时不报错也不显示已复制', async () => {
    listMock.mockResolvedValue(cardsFixture);
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    render(<DeliveredCardsSection orderId="order-1" />);
    await waitFor(/* 等待卡密渲染。 */ () => expect(screen.getByText('KEY-AAA')).toBeTruthy());
    await act(/* 点击复制并等待拒绝。 */ async () => { fireEvent.click(screen.getByLabelText('复制卡密 KEY-AAA')); });
    expect(screen.queryByText('已复制')).toBeNull();
  });

  // 用例：没有记录时提示升级前订单不含记录。
  test('没有记录时提示升级前订单不含记录', async () => {
    listMock.mockResolvedValue([]);
    render(<DeliveredCardsSection orderId="order-old" />);
    await waitFor(/* 等待空状态提示。 */ () => expect(screen.getByText(/暂无自动发货卡密记录/)).toBeTruthy());
  });

  // 用例：加载失败展示错误并可重试。
  test('加载失败展示错误并可重试', async () => {
    listMock.mockRejectedValueOnce(new Error('无权操作此订单')).mockResolvedValueOnce(cardsFixture);
    render(<DeliveredCardsSection orderId="order-1" />);
    await waitFor(/* 等待错误提示。 */ () => expect(screen.getByRole('alert').textContent).toContain('无权操作此订单'));
    fireEvent.click(screen.getByText('重试'));
    await waitFor(/* 等待重试成功。 */ () => expect(screen.getByText('KEY-AAA')).toBeTruthy());
    expect(screen.queryByRole('alert')).toBeNull();

    listMock.mockRejectedValueOnce('boom');
    cleanup();
    render(<DeliveredCardsSection orderId="order-2" />);
    await waitFor(/* 等待兜底错误文案。 */ () => expect(screen.getByRole('alert').textContent).toContain('加载已发卡密失败'));
  });

  // 用例：卸载会取消请求，晚到响应不再更新界面。
  test('卸载会取消请求，晚到响应不再更新界面', /* 卸载取消请求用例回调。 */ async () => {
    // pending 是卸载时仍在进行的请求。
    const pending = deferred<OrderDeliveredCard[]>();
    listMock.mockReturnValueOnce(pending.promise);
    const { unmount } = render(<DeliveredCardsSection orderId="order-1" />);
    // signal 是该请求的取消信号。
    const signal = listMock.mock.calls[0][1]?.signal;
    expect(screen.getByRole('status').textContent).toContain('加载中');
    unmount();
    expect(signal?.aborted).toBe(true);
    await act(/* 请求在卸载后才完成，必须被忽略而不抛出。 */ async () => { pending.resolve(cardsFixture); });
  });
});
