// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { ReplyRule } from '../types';
import ReplyRuleEditor from './ReplyRuleEditor';

/** EditorHarnessProps 描述测试承载组件的初始草稿和被观察的父级动作。 */
interface EditorHarnessProps {
  /** initial 是编辑器首次渲染时的关键词回复草稿。 */
  initial: Partial<ReplyRule>;
  /** onSave 是父级保存动作替身，用于确认编辑器只转发而不自行提交。 */
  onSave: () => void;
  /** onClose 是父级关闭动作替身，用于确认取消按钮的转发。 */
  onClose: () => void;
  /** onDraft 在每次渲染时接收最新草稿，使测试能断言函数式更新的结果。 */
  onDraft: (draft: Partial<ReplyRule> | null) => void;
}

// EditorHarness 用真实 React 状态承载受控草稿，使编辑器的函数式更新可以被端到端观察。
const EditorHarness = ({ initial,onSave,onClose,onDraft }: EditorHarnessProps) => {
  // draft 是编辑器当前展示并修改的关键词回复草稿。
  const [draft, setDraft] = useState<Partial<ReplyRule> | null>(initial);
  onDraft(draft);
  return draft ? <ReplyRuleEditor rule={draft} setRule={setDraft} items={[]} onClose={onClose} onSave={onSave} /> : null;
};

describe('ReplyRuleEditor', /* 当前回调验证关键词回复编辑器的表达式增删、匹配模式与历史规则回填。 */ () => {
  afterEach(/* 当前回调清理 Portal 和测试 DOM。 */ () => cleanup());

  test('新建规则默认一行表达式，可增加并在非最后一行时删除', /* 当前回调验证表达式行的增删边界。 */ () => {
    // latest 记录最近一次渲染得到的草稿。
    let latest: Partial<ReplyRule> | null = null;
    render(<EditorHarness initial={{ keyword: '', expressions: [''], match_type: 'contains', type: 'text', reply_content: '' }} onSave={vi.fn()} onClose={vi.fn()} onDraft={/* draft 是最新草稿。 */ draft => { latest = draft; }} />);
    expect(screen.getByText('新增回复规则')).toBeTruthy();
    // 只有一行时删除按钮必须禁用，保证至少保留一个表达式输入。
    expect((screen.getByLabelText('删除第 1 个表达式') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByText('添加表达式'));
    expect(screen.getAllByPlaceholderText('例如：在吗 | 在不在 | 有人吗')).toHaveLength(2);
    // 编辑第 2 行后，首项兼容字段 keyword 仍等于第 1 行。
    fireEvent.change(screen.getAllByPlaceholderText('例如：在吗 | 在不在 | 有人吗')[1], { target: { value: '发货' } });
    fireEvent.change(screen.getAllByPlaceholderText('例如：在吗 | 在不在 | 有人吗')[0], { target: { value: '在吗' } });
    expect(latest).toMatchObject({ keyword: '在吗', expressions: ['在吗', '发货'] });
    // 两行时可以删除，删到只剩一行后又禁用。
    fireEvent.click(screen.getByLabelText('删除第 1 个表达式'));
    expect(latest).toMatchObject({ keyword: '发货', expressions: ['发货'] });
    expect((screen.getByLabelText('删除第 1 个表达式') as HTMLButtonElement).disabled).toBe(true);
  });

  test('切换匹配模式会更新占位符和提示，且不改动已填写的表达式', /* 当前回调验证包含匹配与正则模式的输入提示差异。 */ () => {
    // latest 记录最近一次渲染得到的草稿。
    let latest: Partial<ReplyRule> | null = null;
    render(<EditorHarness initial={{ keyword: '在吗', expressions: ['在吗'], match_type: 'contains', type: 'text', reply_content: '您好' }} onSave={vi.fn()} onClose={vi.fn()} onDraft={/* draft 是最新草稿。 */ draft => { latest = draft; }} />);
    expect(screen.getByPlaceholderText('例如：在吗 | 在不在 | 有人吗')).toBeTruthy();
    expect(screen.getByText(/包含匹配下每个表达式还可用 \|、中英文逗号、顿号或换行分隔多个候选词/)).toBeTruthy();
    fireEvent.change(screen.getByDisplayValue('包含匹配'), { target: { value: 'regexp' } });
    expect(screen.getByPlaceholderText('正则表达式，例如：在吗|在不在')).toBeTruthy();
    expect(latest).toMatchObject({ match_type: 'regexp', expressions: ['在吗'] });
  });

  test('历史规则只有 keyword 时回填为一行表达式，编辑规则显示编辑标题并转发保存与关闭', /* 当前回调验证历史数据回填与父级动作转发。 */ () => {
    // onSave、onClose 是父级保存与关闭动作替身。
    const onSave = vi.fn();
    // onClose 是父级关闭动作替身。
    const onClose = vi.fn();
    render(<EditorHarness initial={{ id: '7', keyword: '历史词', match_type: 'regexp', type: 'text', reply_content: '回复' }} onSave={onSave} onClose={onClose} onDraft={vi.fn()} />);
    expect(screen.getByText('编辑回复规则')).toBeTruthy();
    expect(screen.getByDisplayValue('历史词')).toBeTruthy();
    fireEvent.click(screen.getByText('保存规则'));
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('取消'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test('“命中后通知人工”勾选只切换通知标记，不改动其他字段', /* 当前回调验证转人工勾选的函数式更新。 */ () => {
    // latest 记录最近一次渲染得到的草稿。
    let latest: Partial<ReplyRule> | null = null;
    render(<EditorHarness initial={{ keyword: '人工', expressions: ['人工'], match_type: 'contains', type: 'text', reply_content: '稍等' }} onSave={vi.fn()} onClose={vi.fn()} onDraft={/* draft 是最新草稿。 */ draft => { latest = draft; }} />);
    // checkbox 是通知人工勾选框，旧规则缺省为未勾选。
    const checkbox = screen.getByLabelText('命中后通知人工') as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
    fireEvent.click(checkbox);
    expect(latest).toMatchObject({ notify_manual: true, reply_content: '稍等', expressions: ['人工'] });
    fireEvent.click(checkbox);
    expect(latest).toMatchObject({ notify_manual: false });
  });
});
