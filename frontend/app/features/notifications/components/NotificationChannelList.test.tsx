// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,within } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { NotificationChannel } from '../api';
import { NotificationChannelList } from './NotificationChannelList';

// feishuAppChannel 是扫码绑定产生的飞书渠道，列表只允许启停、测试和删除。
const feishuAppChannel: NotificationChannel = { id: 'feishu-app-1', name: '飞书扫码渠道', type: 'feishu_app', config: {}, enabled: true, event_types: [] };
// barkChannel 是普通手动渠道，用于验证编辑按钮只在扫码渠道上隐藏。
const barkChannel: NotificationChannel = { id: 'bark-1', name: 'Bark 渠道', type: 'bark', config: { server_url: 'https://api.day.app', device_key: 'key' }, enabled: true, event_types: ['chat_message_received'] };

describe('NotificationChannelList', /* 当前测试组验证扫码飞书渠道的展示和可执行动作。 */ () => {
  afterEach(/* 当前回调清理组件 DOM，避免后续用例读取到残留按钮。 */ () => {
    cleanup();
  });

  test('扫码飞书渠道展示标签、设置入口并隐藏编辑按钮', /* 当前回调验证秘密配置无法安全回填时只提供分类设置入口。 */ () => {
    // onEdit 保存编辑动作替身，用于确认扫码渠道不会触发普通编辑。
    const onEdit = vi.fn();
    // onOpenFeishuSettings 保存分类设置动作替身。
    const onOpenFeishuSettings = vi.fn();
    render(<NotificationChannelList channels={[feishuAppChannel, barkChannel]} testingId="" onEdit={onEdit} onDelete={vi.fn()} onToggleEnabled={vi.fn()} onTest={vi.fn()} onOpenFeishuSettings={onOpenFeishuSettings} />);

    expect(screen.queryByText('飞书机器人（扫码）')).not.toBeNull();
    expect(screen.queryByText(/全部事件/)).not.toBeNull();
    expect(screen.getAllByTitle('编辑')).toHaveLength(1);
    // feishuCard 是扫码飞书渠道的列表卡片，操作按钮限定在该卡片内查找。
    const feishuCard = screen.getByText('飞书扫码渠道').closest('.ios-card') as HTMLElement;
    expect(within(feishuCard).queryByTitle('编辑')).toBeNull();
    expect(within(feishuCard).queryByTitle('删除')).not.toBeNull();
    expect(within(feishuCard).queryByText('测试')).not.toBeNull();
    fireEvent.click(within(feishuCard).getByRole('button', { name: /通知设置/ }));
    expect(onOpenFeishuSettings).toHaveBeenCalledWith(feishuAppChannel);
    expect(onEdit).not.toHaveBeenCalled();
  });

  test('扫码飞书渠道保留启用、测试、设置和删除动作', /* 当前回调验证分类设置入口不会禁用其他渠道操作。 */ () => {
    // onDelete、onToggleEnabled、onTest 和 onOpenFeishuSettings 是扫码渠道动作回调替身。
    const onDelete = vi.fn();
    // onToggleEnabled 记录启用状态切换动作。
    const onToggleEnabled = vi.fn();
    // onTest 记录测试通知动作。
    const onTest = vi.fn();
    // onOpenFeishuSettings 记录分类设置弹窗动作。
    const onOpenFeishuSettings = vi.fn();
    render(<NotificationChannelList channels={[feishuAppChannel]} testingId="" onEdit={vi.fn()} onDelete={onDelete} onToggleEnabled={onToggleEnabled} onTest={onTest} onOpenFeishuSettings={onOpenFeishuSettings} />);

    // feishuCard 是当前扫码渠道的列表卡片。
    const feishuCard = screen.getByText('飞书扫码渠道').closest('.ios-card') as HTMLElement;
    fireEvent.click(within(feishuCard).getByText('启用中'));
    fireEvent.click(within(feishuCard).getByText('测试'));
    fireEvent.click(within(feishuCard).getByRole('button', { name: /通知设置/ }));
    fireEvent.click(within(feishuCard).getByTitle('删除'));
    expect(onToggleEnabled).toHaveBeenCalledWith(feishuAppChannel);
    expect(onTest).toHaveBeenCalledWith(feishuAppChannel);
    expect(onOpenFeishuSettings).toHaveBeenCalledWith(feishuAppChannel);
    expect(onDelete).toHaveBeenCalledWith(feishuAppChannel);
  });
});
