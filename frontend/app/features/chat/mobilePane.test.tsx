// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { describe,expect,test } from 'vitest';
import { useChatMobilePane } from './mobilePane';

/** PaneProps 描述移动端单栏 Hook 测试可切换的账号与会话上下文。 */
interface PaneProps {
  /** accountID 是当前测试使用的账号标识。 */
  accountID: string;
  /** chatID 是当前测试打开的会话标识；空字符串表示没有打开会话。 */
  chatID: string;
}

describe('useChatMobilePane', /* 当前测试组覆盖移动端单栏初始状态、切换动作和上下文重置。 */ () => {
  test('初始显示会话列表，openChat 与 showList 切换面板', /* 当前回调验证移动端主从面板的显式切换动作。 */ () => {
    // hook 是移动端单栏 Hook 的渲染结果。
    const hook = renderHook(/* paneHookFactory 创建固定账号和会话的移动端单栏 Hook。 */ ({ accountID, chatID }: PaneProps) => useChatMobilePane(accountID, chatID), { initialProps: { accountID: 'account-1', chatID: 'chat-1' } });
    expect(hook.result.current.mobilePane).toBe('list');
    act(/* openChatAction 模拟用户选中会话后进入消息详情。 */ () => hook.result.current.openChat());
    expect(hook.result.current.mobilePane).toBe('chat');
    act(/* showListAction 模拟用户点击返回按钮回到会话列表。 */ () => hook.result.current.showList());
    expect(hook.result.current.mobilePane).toBe('list');
    act(/* openChatAction 在切换会话前重新打开消息详情。 */ () => hook.result.current.openChat());
    act(/* chatSwitchAction 模拟同一账号内切换到另一条非空会话，面板不应被重置逻辑强制收回。 */ () => hook.rerender({ accountID: 'account-1', chatID: 'chat-2' }));
    expect(hook.result.current.mobilePane).toBe('chat');
  });

  test('切换账号会回到会话列表', /* 当前回调验证旧账号的消息详情不会带到新账号。 */ () => {
    // hook 是可以切换账号上下文的移动端单栏 Hook。
    const hook = renderHook(/* paneHookFactory 创建可切换账号的移动端单栏 Hook。 */ ({ accountID, chatID }: PaneProps) => useChatMobilePane(accountID, chatID), { initialProps: { accountID: 'account-1', chatID: 'chat-1' } });
    act(/* openChatAction 在旧账号打开消息详情。 */ () => hook.result.current.openChat());
    expect(hook.result.current.mobilePane).toBe('chat');
    act(/* accountSwitchAction 切换到另一个账号及其会话。 */ () => hook.rerender({ accountID: 'account-2', chatID: 'chat-2' }));
    expect(hook.result.current.mobilePane).toBe('list');
  });

  test('当前会话被清空会回到会话列表', /* 当前回调验证删除会话或清空选择后的移动端空态。 */ () => {
    // hook 是可以清空当前会话的移动端单栏 Hook。
    const hook = renderHook(/* paneHookFactory 创建可清空会话的移动端单栏 Hook。 */ ({ accountID, chatID }: PaneProps) => useChatMobilePane(accountID, chatID), { initialProps: { accountID: 'account-1', chatID: 'chat-1' } });
    act(/* openChatAction 在当前会话打开消息详情。 */ () => hook.result.current.openChat());
    expect(hook.result.current.mobilePane).toBe('chat');
    act(/* clearChatAction 模拟删除当前会话后选中会话被清空。 */ () => hook.rerender({ accountID: 'account-1', chatID: '' }));
    expect(hook.result.current.mobilePane).toBe('list');
  });
});
