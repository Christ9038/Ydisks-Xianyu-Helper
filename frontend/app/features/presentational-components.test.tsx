import ReactDOMServer from 'react-dom/server';
import { describe,expect,test } from 'vitest';
import type { NotificationChannel } from './notifications/api';
import type { SystemSettings } from './settings/api';
import { CardIcon } from './cards/components/CardIcon';
import { BatchPhaseIndicator } from './items/components/BatchPhaseIndicator';
import { NotificationChannelList } from './notifications/components/NotificationChannelList';
import { NotificationEventSelector } from './notifications/components/NotificationEventSelector';
import { NotificationSmtpSettings } from './notifications/components/NotificationSmtpSettings';
import { OrderFilterBar } from './orders/components/OrderFilterBar';
import type { AutomationRunIssue,DeferredAutomationIssue } from './rules/api';
import { AutomationIssuePanel } from './rules/components/AutomationIssuePanel';

// render 将 React 展示组件转换为静态 HTML，验证不依赖浏览器 DOM 的渲染分支。
const render = (element: React.ReactElement): string => ReactDOMServer.renderToStaticMarkup(element);

// noopEventToggle 是静态渲染测试使用的通知事件回调占位实现。
const noopEventToggle = (): void => undefined;

// noopFilterChange 是静态渲染测试使用的订单状态回调占位实现。
const noopFilterChange = (): void => undefined;

// resolveAccountName 是静态渲染测试使用的账号展示名称解析器。
const resolveAccountName = (id: string): string => id === 'account-1' ? '测试账号' : id;

// noopSearchChange 是静态渲染测试使用的订单搜索回调占位实现。
const noopSearchChange = (): void => undefined;

// noopChannelAction 是静态渲染测试使用的通知渠道操作占位实现。
const noopChannelAction = (): void => undefined;

// noopRunResolution 是静态渲染测试使用的自动化运行处理占位实现。
const noopRunResolution = (): void => undefined;

// noopDeferredResolution 是静态渲染测试使用的延迟任务处理占位实现。
const noopDeferredResolution = (): void => undefined;

// noopSmtpUpdate 是静态渲染测试使用的 SMTP 配置更新占位实现。
const noopSmtpUpdate = (): void => undefined;

// noopPasswordToggle 是静态渲染测试使用的密码显隐更新占位实现。
const noopPasswordToggle = (): void => undefined;

// noopSmtpSave 是静态渲染测试使用的 SMTP 保存占位实现。
const noopSmtpSave = (): void => undefined;

describe('前端纯展示组件', /* 当前回调处理无浏览器依赖的展示组件断言。 */ () => {
  test('卡密图标覆盖各交付类型', /* 当前回调处理库存图标类型分支。 */ () => {
    expect(render(<CardIcon type="text" />)).toContain('text-blue-500');
    expect(render(<CardIcon type="image" />)).toContain('text-purple-500');
    expect(render(<CardIcon type="api" />)).toContain('text-blue-500');
    expect(render(<CardIcon type={'unknown' as never} />)).toContain('text-gray-500');
  });

  test('批量阶段指示器只高亮当前阶段', /* 当前回调处理批量发布步骤样式分支。 */ () => {
    // markup 是预检阶段批量指示器生成的静态 HTML。
    const markup = render(<BatchPhaseIndicator phase="preview" />);
    expect(markup).toContain('1 上传');
    expect(markup).toContain('2 预检');
    expect(markup).toContain('bg-blue-600 text-white');
    expect(markup.match(/bg-blue-600 text-white/g)).toHaveLength(1);
  });

  test('通知事件选择器渲染已选与未选事件', /* 当前回调处理通知事件列表的选择状态。 */ () => {
    // selected 是包含一个已选事件的通知选择器静态 HTML。
    const selected = render(<NotificationEventSelector selectedEvents={['account_offline']} onToggleEvent={noopEventToggle} />);
    expect(selected).toContain('掉线通知');
    expect(selected).toContain('border-brand bg-blue-50');
    expect(selected).toContain('系统错误');
    expect(selected).toContain('border-gray-100 hover:border-gray-300');
  });

  test('通知渠道列表覆盖空列表、停用和测试中状态', /* 当前回调处理通知渠道摘要和操作状态。 */ () => {
    // empty 是没有配置渠道时的空状态静态 HTML。
    const empty = render(<NotificationChannelList channels={[]} testingId="" onEdit={noopChannelAction} onDelete={noopChannelAction} onToggleEnabled={noopChannelAction} onTest={noopChannelAction} />);
    expect(empty).toContain('还没有配置任何通知渠道');
    // channel 是覆盖配置摘要和停用展示的最小渠道对象。
    const channel: NotificationChannel = { id: 'channel-1', name: '测试 Bark', type: 'bark', config: { server_url: 'https://api.day.app', device_key: 'device-key' }, event_types: ['system_error'], enabled: false };
    // list 是渠道列表生成的静态 HTML。
    const list = render(<NotificationChannelList channels={[channel]} testingId="channel-1" onEdit={noopChannelAction} onDelete={noopChannelAction} onToggleEnabled={noopChannelAction} onTest={noopChannelAction} />);
    expect(list).toContain('测试 Bark');
    expect(list).toContain('已停用');
    expect(list).toContain('系统错误');
    expect(list).toContain('animate-spin');
  });

  test('SMTP 设置面板覆盖密码显隐和保存中状态', /* 当前回调处理系统邮件配置的展示分支。 */ () => {
    // smtp 是覆盖 TLS/SSL 和发件人回填的系统邮件配置。
    const smtp: SystemSettings = { smtp_server: 'smtp.example.com', smtp_port: 465, smtp_user: 'sender@example.com', smtp_password: 'secret', smtp_use_tls: false, smtp_use_ssl: true };
    // hiddenMarkup 是密码隐藏且保存按钮可用时的静态 HTML。
    const hiddenMarkup = render(<NotificationSmtpSettings smtp={smtp} setSmtp={noopSmtpUpdate} smtpSaving={false} showPassword={false} setShowPassword={noopPasswordToggle} onSave={noopSmtpSave} />);
    expect(hiddenMarkup).toContain('type="password"');
    expect(hiddenMarkup).toContain('保存 SMTP 配置');
    expect(hiddenMarkup).toContain('SMTP 邮件配置');
    // savingMarkup 是密码显示且请求执行中的静态 HTML。
    const savingMarkup = render(<NotificationSmtpSettings smtp={smtp} setSmtp={noopSmtpUpdate} smtpSaving showPassword setShowPassword={noopPasswordToggle} onSave={noopSmtpSave} />);
    expect(savingMarkup).toContain('type="text"');
    expect(savingMarkup).toContain('保存中...');
    expect(savingMarkup).toContain('disabled');
  });

  test('自动化异常面板渲染运行异常和延迟任务操作', /* 当前回调处理人工核对面板的异常分支。 */ () => {
    // runIssue 是允许继续、重试和终止的运行异常。
    const runIssue: AutomationRunIssue = { id: 1, cookie_id: 'account-1', order_id: 'order-1', trigger_type: 'order_paid', error_message: '发送结果未知', issue_kind: 'external_result_unknown', allowed_resolutions: ['continue', 'retry', 'cancel'], action_cursor: 1, sent_count: 1, updated_at: '2026-08-15T00:00:00Z' };
    // deferredIssue 是达到重试上限的延迟任务异常。
    const deferredIssue: DeferredAutomationIssue = { id: 2, cookie_id: 'account-1', trigger_type: 'review_missing_timeout', error_message: '任务重试失败', attempt_count: 3, updated_at: '2026-08-15T00:00:00Z' };
    // panel 是自动化异常面板生成的静态 HTML。
    const panel = render(<AutomationIssuePanel runs={[runIssue]} pendingTasks={[deferredIssue]} onResolveRun={noopRunResolution} onResolveDeferredTask={noopDeferredResolution} />);
    expect(panel).toContain('需要人工处理的自动化任务');
    expect(panel).toContain('外部动作结果未知');
    expect(panel).toContain('已执行，继续下一步');
    expect(panel).toContain('未执行，安全重试');
    expect(panel).toContain('终止');
    expect(panel).toContain('重新入队');
    expect(panel).toContain('忽略');
  });

  test('订单筛选栏渲染状态、账号和关键词输入', /* 当前回调处理订单筛选工具栏的静态结构。 */ () => {
    // markup 是订单筛选栏生成的静态 HTML。
    const markup = render(<OrderFilterBar
      filter="paid"
      onFilterChange={noopFilterChange}
      accountFilter="account-1"
      onAccountFilterChange={noopFilterChange}
      accounts={[{ id: 'account-1', nickname: '测试账号', enabled: true, auto_confirm: false }]}
      accountName={resolveAccountName}
      searchText="订单"
      onSearchChange={noopSearchChange}
    />);
    expect(markup).toContain('已发货');
    expect(markup).toContain('测试账号');
    expect(markup).toContain('订单号/商品/买家');
    expect(markup).toContain('aria-label="按账号筛选订单"');
  });
});
