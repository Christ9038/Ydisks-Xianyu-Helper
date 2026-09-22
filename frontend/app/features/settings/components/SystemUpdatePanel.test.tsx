// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';

import { applyLatestStableUpdate,checkSystemUpdate,getSystemUpdateStatus,type SystemUpdateSnapshot } from '../api';
import { SystemUpdatePanel } from './SystemUpdatePanel';

vi.mock('../api', /* apiMockFactory 提供系统更新面板使用的三个可编程请求替身。 */ () => ({
  getSystemUpdateStatus: vi.fn(),
  checkSystemUpdate: vi.fn(),
  applyLatestStableUpdate: vi.fn(),
}));

// getStatusMock 是只读更新状态请求替身。
const getStatusMock = vi.mocked(getSystemUpdateStatus);
// checkUpdateMock 是主动检查正式版请求替身。
const checkUpdateMock = vi.mocked(checkSystemUpdate);
// applyUpdateMock 是提交固定更新命令请求替身。
const applyUpdateMock = vi.mocked(applyLatestStableUpdate);

// updateSnapshot 构造满足前端契约的系统更新状态。
const updateSnapshot = (overrides: Partial<SystemUpdateSnapshot> = {}): SystemUpdateSnapshot => ({
  current: { version: '1.0.0', commit: 'abcdef', build_time: '2026-09-22T00:00:00Z', deployment_kind: 'stable' },
  latest: { version: '1.0.1', tag: 'v1.0.1', release_url: 'https://example.com/releases/v1.0.1', published_at: '2026-09-22T00:00:00Z', manifest_digest: `sha256:${'a'.repeat(64)}` },
  available: true,
  can_update: true,
  blocked_reason: '',
  operation: { status: 'idle', request_id: '', started_at: '', finished_at: '', message: '' },
  ...overrides,
});

describe('SystemUpdatePanel', /* testGroup 验证安全阻断、检查、确认和任务轮询交互。 */ () => {
  beforeEach(/* resetAction 为每个系统更新测试恢复默认成功响应。 */ () => {
    vi.useRealTimers();
    vi.clearAllMocks();
    getStatusMock.mockResolvedValue(updateSnapshot());
    checkUpdateMock.mockResolvedValue(updateSnapshot({ latest: { ...updateSnapshot().latest, version: '1.0.2', tag: 'v1.0.2' } }));
    applyUpdateMock.mockResolvedValue(updateSnapshot({ operation: { status: 'queued', request_id: 'request-1', started_at: '', finished_at: '', message: '任务已排队' } }));
    vi.spyOn(window,'confirm').mockReturnValue(true);
  });

  afterEach(/* cleanupAction 清理 DOM、计时器和确认框替身。 */ () => {
    cleanup();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  test('允许检查并在二次确认后提交固定正式版更新', /* testCase 验证管理员主流程和确认边界。 */ async () => {
    render(<SystemUpdatePanel />);
    expect(await screen.findByText('v1.0.1')).toBeTruthy();

    fireEvent.click(screen.getByRole('button',{ name: '检查更新' }));
    await waitFor(/* checkAssertion 等待检查请求完成并刷新最新版本。 */ () => expect(checkUpdateMock).toHaveBeenCalledTimes(1));
    expect(await screen.findByText('v1.0.2')).toBeTruthy();

    fireEvent.click(screen.getByRole('button',{ name: '更新到 v1.0.2' }));
    await waitFor(/* applyAssertion 等待任务提交并切换为排队状态。 */ () => expect(applyUpdateMock).toHaveBeenCalledTimes(1));
    expect(window.confirm).toHaveBeenCalledTimes(1);
    expect(await screen.findByText('等待执行')).toBeTruthy();
  });

  test('自定义镜像只展示阻断原因且不提供更新按钮', /* testCase 验证 custom 部署不会误切官方镜像。 */ async () => {
    getStatusMock.mockResolvedValue(updateSnapshot({
      current: { ...updateSnapshot().current, deployment_kind: 'custom' },
      can_update: false,
      blocked_reason: 'custom_image',
    }));
    render(<SystemUpdatePanel />);
    expect(await screen.findByText(/当前运行的是自定义镜像/)).toBeTruthy();
    expect(screen.queryByRole('button',{ name: /更新到/ })).toBeNull();
  });

  test('活跃任务会轮询直到完成并在卸载时取消后续请求', /* testCase 验证异步更新状态收敛和计时器清理。 */ async () => {
    vi.useFakeTimers();
    getStatusMock
      .mockResolvedValueOnce(updateSnapshot({ operation: { status: 'queued', request_id: 'request-1', started_at: '', finished_at: '', message: '任务已排队' } }))
      .mockResolvedValueOnce(updateSnapshot({ available: false, can_update: false, operation: { status: 'succeeded', request_id: 'request-1', started_at: '', finished_at: '2026-09-22T00:01:00Z', message: '更新完成' } }));
    // view 保存面板卸载方法，用于验证清理不会继续轮询。
    const view = render(<SystemUpdatePanel />);
    await vi.waitFor(/* queuedAssertion 等待首次请求进入排队状态。 */ () => expect(screen.getByText('等待执行')).toBeTruthy());
    await vi.advanceTimersByTimeAsync(2000);
    await vi.waitFor(/* completedAssertion 等待第二次状态请求展示完成结果。 */ () => expect(screen.getByText('已完成')).toBeTruthy());
    expect(getStatusMock).toHaveBeenCalledTimes(2);
    view.unmount();
    await vi.advanceTimersByTimeAsync(4000);
    expect(getStatusMock).toHaveBeenCalledTimes(2);
  });
});
