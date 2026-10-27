// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { downloadBlob } from '../../../../shared/browser/download';
import { exportCard,getCardSaleRecords } from '../api';
import type { Card,CardSalePage } from '../models';
import { CardStockDialog } from './CardStockDialog';

vi.mock('../api', /* stockApiMockFactory 提供库存记录弹窗的确定性 API 替身。 */ () => ({
  getCardSaleRecords: vi.fn(),
  exportCard: vi.fn(),
}));
vi.mock('../../../../shared/browser/download', /* downloadMockFactory 屏蔽真实浏览器下载。 */ () => ({ downloadBlob: vi.fn() }));

// listMock 是售出记录查询替身，exportMock 是导出请求替身，downloadMock 记录浏览器下载。
const listMock = vi.mocked(getCardSaleRecords);
// exportMock 是被模拟的导出接口，用于断言导出范围与失败路径。
const exportMock = vi.mocked(exportCard);
const downloadMock = vi.mocked(downloadBlob);

// dataCard 是带 3 条未售库存、已售 2 条的 data 卡密组。
const dataCard: Card = { id: 5, name: '批量组', type: 'data', enabled: true, data_content: 'K1\r\n\r\nK2\nK3', sold_count: 2 };
// apiCard 是只有售出历史的 API 卡密组。
const apiCard: Card = { id: 6, name: '接口组', type: 'api', enabled: true, sold_count: 1 };

// pageWith 构造包含已售与待核对两条记录的一页结果。
const pageWith = (totalPages = 1): CardSalePage => ({
  records: [
    { id: 2, content: 'SOLD-1', status: 'sold', cardName: '批量组', cookieId: 'acc1', orderId: 'order-1', itemId: 'i1', buyerId: 'b1', soldAtMs: 1_700_000_000_000 },
    { id: 1, content: 'PENDING-1', status: 'reserved', cardName: '批量组', cookieId: 'acc1', orderId: 'order-2', itemId: 'i2', buyerId: 'b2', soldAtMs: 0 },
  ],
  total: 2,
  totalPages,
});

describe('CardStockDialog', /* 当前回调验证库存记录弹窗的页签、导出、筛选和分页。 */ () => {
  beforeEach(/* 重置替身。 */ () => {
    vi.clearAllMocks();
    listMock.mockResolvedValue(pageWith());
  });
  afterEach(/* 卸载渲染树，清理 portal。 */ () => cleanup());

  // 用例：data 卡默认展示未售库存并可导出 TXT。
  test('data 卡默认展示未售库存并可导出 TXT', async () => {
    exportMock.mockResolvedValue({ blob: new Blob(['K1\nK2\nK3\n']), filename: '批量组-未售.txt' });
    render(<CardStockDialog card={dataCard} onClose={vi.fn()} />);
    expect(screen.getByRole('tab', { name: '未售 3' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByText('K2')).toBeTruthy();
    fireEvent.click(screen.getByText('导出未售 TXT'));
    await waitFor(/* 等待下载触发。 */ () => expect(downloadMock).toHaveBeenCalledTimes(1));
    expect(exportMock.mock.calls[0].slice(0, 2)).toEqual([5, 'unsold']);
    expect(downloadMock.mock.calls[0][1]).toBe('批量组-未售.txt');
  });

  // 用例：未售为空时禁用导出，超长库存只预览前 200 条。
  test('未售为空时禁用导出，超长库存只预览前 200 条', () => {
    // unmount 用于卸载渲染结果，避免影响后续用例。
    const { unmount } = render(<CardStockDialog card={{ ...dataCard, data_content: '' }} onClose={vi.fn()} />);
    expect(screen.getByText('暂无未售卡密')).toBeTruthy();
    expect((screen.getByText('导出未售 TXT').closest('button') as HTMLButtonElement).disabled).toBe(true);
    unmount();
    // many 是 205 行的超长库存正文，用于验证预览上限。
    const many = Array.from({ length: 205 }, /* index 是生成卡密的序号。 */ (_, index) => `C${index}`).join('\n');
    render(<CardStockDialog card={{ ...dataCard, data_content: many }} onClose={vi.fn()} />);
    expect(screen.getByText(/仅预览前 200 条/)).toBeTruthy();
    expect(screen.queryByText('C204')).toBeNull();
  });

  // 用例：已售页签展示记录与状态标签，并支持筛选、搜索和翻页。
  test('已售页签展示记录、状态标签并支持筛选、搜索和翻页', async () => {
    listMock.mockResolvedValue(pageWith(2));
    render(<CardStockDialog card={dataCard} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole('tab', { name: '已售 2' }));
    await waitFor(/* 等待记录渲染。 */ () => expect(screen.getByText('SOLD-1')).toBeTruthy());
    expect(screen.getByText('PENDING-1')).toBeTruthy();
    // 筛选下拉也含“待核对”，因此只在表格行内断言状态徽标。
    expect(screen.getByText('PENDING-1').closest('tr')?.textContent).toContain('待核对');
    expect(screen.getByText('SOLD-1').closest('tr')?.textContent).toContain('已售');
    expect(screen.getByText('1 / 2')).toBeTruthy();

    fireEvent.change(screen.getByLabelText('状态筛选'), { target: { value: 'reserved' } });
    await waitFor(/* 等待状态筛选请求。 */ () => expect(listMock.mock.calls.at(-1)?.[1]).toMatchObject({ status: 'reserved', page: 1 }));
    fireEvent.change(screen.getByLabelText('搜索订单号或卡密'), { target: { value: ' order-2 ' } });
    fireEvent.submit(screen.getByLabelText('搜索订单号或卡密').closest('form') as HTMLFormElement);
    await waitFor(/* 等待搜索请求。 */ () => expect(listMock.mock.calls.at(-1)?.[1]).toMatchObject({ search: 'order-2', status: 'reserved' }));
    fireEvent.click(screen.getByText('下一页'));
    await waitFor(/* 等待翻页请求。 */ () => expect(listMock.mock.calls.at(-1)?.[1].page).toBe(2));
    fireEvent.click(screen.getByText('上一页'));
    await waitFor(/* 等待回到第一页。 */ () => expect(listMock.mock.calls.at(-1)?.[1].page).toBe(1));
  });

  // 用例：已售导出 CSV、空列表提示与加载失败重试。
  test('已售导出 CSV、空列表提示与加载失败重试', async () => {
    exportMock.mockResolvedValue({ blob: new Blob(['csv']), filename: '批量组-已售.csv' });
    listMock.mockRejectedValueOnce(new Error('服务暂不可用'));
    render(<CardStockDialog card={apiCard} onClose={vi.fn()} />);
    expect(screen.queryByRole('tab', { name: /未售/ })).toBeNull();
    await waitFor(/* 等待错误提示。 */ () => expect(screen.getByRole('alert').textContent).toContain('服务暂不可用'));

    listMock.mockResolvedValueOnce({ records: [], total: 0, totalPages: 1 });
    fireEvent.click(screen.getByText('重试'));
    await waitFor(/* 等待空列表提示。 */ () => expect(screen.getByText(/暂无售出记录/)).toBeTruthy());

    fireEvent.click(screen.getByText('导出已售 CSV'));
    await waitFor(/* 等待下载触发。 */ () => expect(downloadMock).toHaveBeenCalledTimes(1));
    expect(exportMock.mock.calls[0].slice(0, 2)).toEqual([6, 'sold']);
  });

  // 用例：导出失败展示错误提示，关闭按钮通知父组件。
  test('导出失败展示错误提示，关闭按钮通知父组件', /* 导出失败用例回调。 */ async () => {
    exportMock.mockRejectedValue(new Error('无权导出'));
    // onClose 是关闭回调的替身，用于断言父组件被通知。
    const onClose = vi.fn();
    render(<CardStockDialog card={apiCard} onClose={onClose} />);
    await waitFor(/* 等待首次加载完成。 */ () => expect(screen.getByText('SOLD-1')).toBeTruthy());
    fireEvent.click(screen.getByText('导出已售 CSV'));
    await waitFor(/* 等待导出错误。 */ () => expect(screen.getByRole('alert').textContent).toContain('无权导出'));
    expect(downloadMock).not.toHaveBeenCalled();
    fireEvent.click(screen.getByLabelText('关闭库存记录'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
