// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import React from 'react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { SystemSettings } from '../api';
import { buildNotificationPayload,emptyNotificationForm } from '../state';
import type { NotificationForm } from '../types';
import { NotificationChannelModal } from './NotificationChannelModal';
import type { FeishuBindingPanelProps } from './FeishuBindingPanel';

// mountSpy 记录扫码面板挂载次数，用于确认只有选择扫码授权才加载面板。
const mountSpy = vi.fn();
// unmountSpy 记录扫码面板卸载次数，用于确认切换连接方式后绑定流程被释放。
const unmountSpy = vi.fn();
// lastPanelForm 保存面板最近一次收到的表单快照，用于确认共用字段随会话提交。
let lastPanelForm: FeishuBindingPanelProps['form'] | null = null;
// StubPanel 是扫码绑定面板替身：暴露“绑定成功”和“锁定表单”两个按钮来触发回调，隔离真实二维码轮询。
const StubPanel: React.FC<FeishuBindingPanelProps> = ({ form, onSuccess, onLockChange }) => {
  lastPanelForm = form;
  React.useEffect(/* 当前副作用记录面板挂载，并在卸载时记录释放且解锁（与真实面板一致）。 */ () => {
    mountSpy();
    return /* 当前清理回调在面板卸载时记录释放并解锁共用表单。 */ () => {
      unmountSpy();
      onLockChange?.(false);
    };
  }, [onLockChange]);
  return <><button type="button" onClick={/* 模拟后端返回渠道主键 7。 */ () => onSuccess?.(7)}>模拟绑定成功</button><button type="button" onClick={/* 模拟二维码已生成、会话进行中。 */ () => onLockChange?.(true)}>模拟会话开始</button></>;
};

/** HarnessProps 描述测试容器可调的弹窗状态。 */
interface HarnessProps {
  // editing 表示弹窗处于编辑已有渠道状态。
  editing?: boolean;
  // withPanel 表示是否注入扫码绑定面板。
  withPanel?: boolean;
  // onBound 接收扫码成功后的渠道主键。
  onBound?: (id: number) => void;
  // onSave 是保存按钮动作替身。
  onSave?: () => void;
  // initialType 是表单初始渠道类型，默认飞书。
  initialType?: NotificationForm['type'];
  // showModal 表示弹窗是否打开，默认打开。
  showModal?: boolean;
  // accountsStatus 是账号选项加载状态，默认 ready。
  accountsStatus?: 'loading' | 'ready' | 'error';
  // onRetryAccounts 是账号加载失败后的重试替身。
  onRetryAccounts?: () => void;
  // initialAccountIds 是表单初始绑定账号。
  initialAccountIds?: string[];
}

// Harness 以真实 useState 驱动 form，使类型切换与弹窗联动与页面一致。
const Harness: React.FC<HarnessProps> = ({ editing = false, withPanel = true, onBound, onSave = vi.fn(), initialType = 'feishu', showModal = true, accountsStatus = 'ready', onRetryAccounts, initialAccountIds = [] }) => {
  // form 是弹窗表单值，初始类型由用例指定。
  const [form, setForm] = React.useState<NotificationForm>({ ...emptyNotificationForm(), type: initialType, account_ids: initialAccountIds });
  return <NotificationChannelModal showModal={showModal} editing={editing ? { id: 1 } as never : null} form={form} setForm={setForm} smtp={{} as SystemSettings} showChannelSmtpPassword={false} setShowChannelSmtpPassword={vi.fn()} saving={false} onClose={vi.fn()} onSave={onSave} feishuBindingPanel={withPanel ? StubPanel : undefined} onFeishuBound={onBound} accountOptions={[{ id: 'a', label: '账号A' }, { id: 'b', label: '账号B' }]} accountsStatus={accountsStatus} onRetryAccounts={onRetryAccounts} />;
};

describe('NotificationChannelModal 飞书连接方式', /* 当前测试组验证飞书机器人在新建渠道弹窗内以链接为基本盘、扫码授权为可选连接方式。 */ () => {
  afterEach(/* 当前回调清理渲染并重置替身状态。 */ () => {
    cleanup();
    mountSpy.mockClear();
    unmountSpy.mockClear();
    lastPanelForm = null;
  });

  test('选择飞书默认是 Webhook 链接，且不加载扫码面板', /* 当前回调验证基本盘是原有链接表单。 */ () => {
    render(<Harness />);
    expect(screen.getByRole('tab', { name: 'Webhook 链接' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByPlaceholderText(/open\.feishu\.cn/)).not.toBeNull();
    expect(screen.getByRole('button', { name: '保存' })).not.toBeNull();
    expect(mountSpy).not.toHaveBeenCalled();
  });

  test('扫码授权只替换连接区域，名称、通知内容和启用状态保持共用', /* 当前回调验证两种方式使用同一张表单。 */ () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    expect(mountSpy).toHaveBeenCalledTimes(1);
    expect(screen.queryByPlaceholderText(/open\.feishu\.cn/)).toBeNull();
    expect(screen.queryByRole('button', { name: '保存' })).toBeNull();
    expect(screen.getByText('渠道名称')).not.toBeNull();
    expect(screen.getByText('通知内容')).not.toBeNull();
    expect(screen.getByText('启用此渠道')).not.toBeNull();
    expect(screen.getByRole('button', { name: '取消' })).not.toBeNull();
  });

  test('扫码面板收到的是当前共用表单快照', /* 当前回调验证名称、事件和启用状态会随会话提交。 */ () => {
    render(<Harness />);
    fireEvent.change(screen.getByPlaceholderText(/例如/), { target: { value: '值班飞书' } });
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    expect(lastPanelForm).toEqual({ name: '值班飞书', eventTypes: [], enabled: true, accountIds: [] });
  });

  test('扫码会话进行中锁定共用表单，离开扫码方式后解锁', /* 当前回调验证二维码已生成后表单不可再改，避免与已提交参数不一致。 */ () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    // nameInput 是共用的渠道名称输入框。
    const nameInput = screen.getByPlaceholderText(/例如/) as HTMLInputElement;
    expect(nameInput.matches(':disabled')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: '模拟会话开始' }));
    expect(nameInput.matches(':disabled')).toBe(true);
    fireEvent.click(screen.getByRole('tab', { name: 'Webhook 链接' }));
    expect(unmountSpy).toHaveBeenCalledTimes(1);
    expect((screen.getByPlaceholderText(/例如/) as HTMLInputElement).matches(':disabled')).toBe(false);
  });

  test('切换回链接方式保留已填内容并保留原保存入口', /* 当前回调验证两种方式切换不丢失共用字段。 */ () => {
    // onSave 是保存动作替身，确认链接方式保留原保存入口。
    const onSave = vi.fn();
    render(<Harness onSave={onSave} />);
    fireEvent.change(screen.getByPlaceholderText(/例如/), { target: { value: '备用机器人' } });
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    fireEvent.click(screen.getByRole('tab', { name: 'Webhook 链接' }));
    expect((screen.getByPlaceholderText(/例如/) as HTMLInputElement).value).toBe('备用机器人');
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  test('扫码成功会把渠道主键交给页面回调', /* 当前回调验证绑定成功事件穿过弹窗到达页面刷新逻辑。 */ () => {
    // onBound 记录页面收到的渠道主键。
    const onBound = vi.fn();
    render(<Harness onBound={onBound} />);
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    fireEvent.click(screen.getByRole('button', { name: '模拟绑定成功' }));
    expect(onBound).toHaveBeenCalledWith(7);
  });

  test('编辑已有渠道和非飞书类型不出现连接方式切换', /* 当前回调验证切换只属于新建飞书渠道。 */ () => {
    // first 渲染编辑态，旧飞书 Webhook 渠道只能走原表单。
    const first = render(<Harness editing />);
    expect(screen.queryByRole('tablist')).toBeNull();
    expect(screen.getByPlaceholderText(/open\.feishu\.cn/)).not.toBeNull();
    first.unmount();
    render(<Harness initialType="bark" />);
    expect(screen.queryByRole('tablist')).toBeNull();
    expect(mountSpy).not.toHaveBeenCalled();
  });

  test('编辑扫码授权渠道使用同一张表单且不提交凭据', /* 当前回调验证扫码渠道可像链接渠道一样修改名称、通知内容和启用状态。 */ () => {
    render(<Harness editing initialType="feishu_app" />);
    expect(screen.queryByRole('tablist')).toBeNull();
    expect(screen.getByText('通知内容')).not.toBeNull();
    expect(screen.getByText('启用此渠道')).not.toBeNull();
    expect(screen.getByRole('button', { name: '保存' })).not.toBeNull();
    // payload 是扫码渠道保存时的请求体：不含 config，服务端保留已保存的应用凭据。
    const payload = buildNotificationPayload({ ...emptyNotificationForm(), type: 'feishu_app', name: '飞书应用', event_types: ['account_offline'] });
    expect('config' in payload).toBe(false);
    expect(payload.event_types).toEqual(['account_offline']);
  });

  test('未注入扫码面板时飞书只提供链接方式', /* 当前回调验证面板缺失时不会出现失效的扫码入口。 */ () => {
    render(<Harness withPanel={false} />);
    expect(screen.queryByRole('tablist')).toBeNull();
    expect(screen.getByPlaceholderText(/open\.feishu\.cn/)).not.toBeNull();
  });

  test('弹窗关闭后重新打开回到链接方式', /* 当前回调验证上次选择的扫码方式不会残留到下一次新建。 */ () => {
    // view 提供 rerender 来模拟弹窗关闭再打开。
    const view = render(<Harness />);
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    view.rerender(<Harness showModal={false} />);
    view.rerender(<Harness showModal />);
    expect(screen.getByRole('tab', { name: 'Webhook 链接' }).getAttribute('aria-selected')).toBe('true');
  });
});

describe('NotificationChannelModal 绑定账号', /* 当前测试组验证启用渠道下方的账号绑定入口。 */ () => {
  afterEach(/* 当前回调清理渲染。 */ () => cleanup());

  test('启用开关下方展示账号复选框，勾选状态随表单变化', /* 当前回调验证切换账号会更新选择。 */ () => {
    render(<Harness initialAccountIds={['a']} initialType="bark" />);
    // boxA、boxB 是两个账号的复选框。
    const boxA = screen.getByLabelText('账号A') as HTMLInputElement;
    // boxB 是账号B的复选框，初始未勾选。
    const boxB = screen.getByLabelText('账号B') as HTMLInputElement;
    expect(boxA.checked).toBe(true);
    expect(boxB.checked).toBe(false);
    fireEvent.click(boxB);
    fireEvent.click(boxA);
    expect((screen.getByLabelText('账号A') as HTMLInputElement).checked).toBe(false);
    expect((screen.getByLabelText('账号B') as HTMLInputElement).checked).toBe(true);
  });

  test('加载失败时提示不会改动绑定并可重试，加载中显示占位', /* 当前回调验证非 ready 状态。 */ () => {
    // retry 是重试回调替身。
    const retry = vi.fn();
    render(<Harness accountsStatus="error" onRetryAccounts={retry} initialType="bark" />);
    expect(screen.queryByLabelText('账号A')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(retry).toHaveBeenCalledTimes(1);
    cleanup();
    render(<Harness accountsStatus="loading" initialType="bark" />);
    expect(screen.getByText('正在加载账号')).not.toBeNull();
  });

  test('扫码授权时账号选择随会话提交，账号未加载则不提交', /* 当前回调验证飞书扫码与绑定账号联动。 */ () => {
    render(<Harness initialAccountIds={['b']} />);
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    expect(lastPanelForm?.accountIds).toEqual(['b']);
    cleanup();
    render(<Harness accountsStatus="error" initialAccountIds={['b']} />);
    fireEvent.click(screen.getByRole('tab', { name: '扫码授权' }));
    expect(lastPanelForm?.accountIds).toBeUndefined();
  });
});
