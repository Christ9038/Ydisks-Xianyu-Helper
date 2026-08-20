// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import { AudioMessage, formatAudioTime, secureAudioURL } from './AudioMessage';

/** recorderHarness 保存可观察的解码器状态和事件回调，使测试不依赖浏览器 AudioContext。 */
const recorderHarness = vi.hoisted(() => ({
  /** initialized 表示测试解码器是否已完成 AMR 初始化。 */
  initialized: false,
  /** playing 表示测试解码器当前是否播放。 */
  playing: false,
  /** paused 表示测试解码器当前是否暂停。 */
  paused: false,
  /** initError 控制下一次媒体初始化是否失败。 */
  initError: null as Error | null,
  /** playEvent、pauseEvent、resumeEvent、stopEvent 和 endedEvent 保存组件注册的生命周期回调。 */
  playEvent: null as (() => void) | null,
  pauseEvent: null as (() => void) | null,
  resumeEvent: null as (() => void) | null,
  stopEvent: null as (() => void) | null,
  endedEvent: null as (() => void) | null,
  /** initWithUrl 记录待解码媒体地址，并按 initError 决定成功或失败。 */
  initWithUrl: vi.fn(async function initWithUrl(): Promise<void> {
    if (recorderHarness.initError) throw recorderHarness.initError;
    recorderHarness.initialized = true;
  }),
  /** play 从头播放并触发组件注册的播放事件。 */
  play: vi.fn(function play(): void {
    recorderHarness.playing = true;
    recorderHarness.paused = false;
    recorderHarness.playEvent?.();
  }),
  /** pause 暂停播放并触发组件注册的暂停事件。 */
  pause: vi.fn(function pause(): void {
    recorderHarness.playing = false;
    recorderHarness.paused = true;
    recorderHarness.pauseEvent?.();
  }),
  /** resume 从暂停位置继续并触发组件注册的恢复事件。 */
  resume: vi.fn(function resume(): void {
    recorderHarness.playing = true;
    recorderHarness.paused = false;
    recorderHarness.resumeEvent?.();
  }),
  /** destroy 记录组件是否释放解码缓存和浏览器音频节点。 */
  destroy: vi.fn(),
  /** isInit 返回测试解码器是否已经完成初始化。 */
  isInit: vi.fn(/* 当前回调返回测试解码器的初始化状态。 */ (): boolean => recorderHarness.initialized),
  /** isPlaying 返回测试解码器当前播放状态。 */
  isPlaying: vi.fn(/* 当前回调返回测试解码器的播放状态。 */ (): boolean => recorderHarness.playing),
  /** isPaused 返回测试解码器当前暂停状态。 */
  isPaused: vi.fn(/* 当前回调返回测试解码器的暂停状态。 */ (): boolean => recorderHarness.paused),
  /** getDuration 返回三秒的确定性语音长度。 */
  getDuration: vi.fn(/* 当前回调提供确定性的三秒总时长。 */ (): number => 3),
  /** getCurrentPosition 返回一秒的确定性播放进度。 */
  getCurrentPosition: vi.fn(/* 当前回调提供确定性的一秒播放进度。 */ (): number => 1),
  /** onPlay、onPause、onResume、onStop 和 onEnded 接收组件注册的解码器事件。 */
  onPlay: vi.fn(/* callback 是组件注册的播放开始回调。 */ (callback: () => void): void => { recorderHarness.playEvent = callback; }),
  onPause: vi.fn(/* callback 是组件注册的播放暂停回调。 */ (callback: () => void): void => { recorderHarness.pauseEvent = callback; }),
  onResume: vi.fn(/* callback 是组件注册的播放恢复回调。 */ (callback: () => void): void => { recorderHarness.resumeEvent = callback; }),
  onStop: vi.fn(/* callback 是组件注册的主动停止回调。 */ (callback: () => void): void => { recorderHarness.stopEvent = callback; }),
  onEnded: vi.fn(/* callback 是组件注册的自然结束回调。 */ (callback: () => void): void => { recorderHarness.endedEvent = callback; }),
}));

vi.mock('benz-amr-recorder', /* 当前工厂回调用确定性替身隔离浏览器音频实现。 */ () => ({
  /** default 模拟聊天路由加载后通过 new 创建的 AMR 解码器。 */
  default: vi.fn(/* MockBenzAMRRecorder 返回当前测试共享的解码器替身。 */ function MockBenzAMRRecorder() { return recorderHarness; }),
}));

/** resetRecorderHarness 清除测试间的播放状态、回调和调用记录。 */
function resetRecorderHarness(): void {
  recorderHarness.initialized = false;
  recorderHarness.playing = false;
  recorderHarness.paused = false;
  recorderHarness.initError = null;
  recorderHarness.playEvent = null;
  recorderHarness.pauseEvent = null;
  recorderHarness.resumeEvent = null;
  recorderHarness.stopEvent = null;
  recorderHarness.endedEvent = null;
  // method 保存当前待清理调用记录的测试替身。
  for (const /* method 是当前待清理调用记录的测试替身。 */ method of [
    recorderHarness.initWithUrl, recorderHarness.play, recorderHarness.pause, recorderHarness.resume,
    recorderHarness.destroy, recorderHarness.isInit, recorderHarness.isPlaying, recorderHarness.isPaused,
    recorderHarness.getDuration, recorderHarness.getCurrentPosition, recorderHarness.onPlay,
    recorderHarness.onPause, recorderHarness.onResume, recorderHarness.onStop, recorderHarness.onEnded,
  ]) method.mockClear();
}

beforeEach(/* 当前回调在每条测试前恢复解码器初始状态。 */ () => resetRecorderHarness());
afterEach(/* 当前回调清理 React 容器及临时全局对象。 */ () => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('AudioMessage', /* 当前测试套件覆盖语音组件的主要交互生命周期。 */ () => {
  test('首次点击延迟解码并支持播放、暂停和继续', /* 当前测试回调验证成功播放路径和资源释放。 */ async () => {
    // view 保存组件渲染句柄，用于验证卸载会释放解码器。
    const view = render(<AudioMessage messageKey="voice-1" src="https://cdn.example/voice.amr" outgoing={false} initialDuration={3} />);
    expect(screen.getByText('0:03')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '播放语音消息' }));
    await waitFor(/* 当前轮询回调等待异步解码完成并进入播放态。 */ () => expect(screen.getByRole('button', { name: '暂停语音消息' })).toBeTruthy());
    expect(recorderHarness.initWithUrl).toHaveBeenCalledWith('https://cdn.example/voice.amr');
    expect(screen.getByText('0:00 / 0:03')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '暂停语音消息' }));
    expect(screen.getByRole('button', { name: '继续播放语音消息' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '继续播放语音消息' }));
    expect(recorderHarness.resume).toHaveBeenCalledTimes(1);
    view.unmount();
    expect(recorderHarness.destroy).toHaveBeenCalledTimes(1);
  });

  test('解码失败后展示可重试状态', /* 当前测试回调验证失败提示与再次初始化。 */ async () => {
    recorderHarness.initError = new Error('decode failed');
    render(<AudioMessage messageKey="voice-error" src="https://cdn.example/broken.amr" outgoing initialDuration={3} />);
    fireEvent.click(screen.getByRole('button', { name: '播放语音消息' }));
    await waitFor(/* 当前轮询回调等待失败状态写入界面。 */ () => expect(screen.getByRole('button', { name: '重新加载语音消息' })).toBeTruthy());
    expect(screen.getByText('加载失败，点击重试')).toBeTruthy();

    recorderHarness.initError = null;
    fireEvent.click(screen.getByRole('button', { name: '重新加载语音消息' }));
    await waitFor(/* 当前轮询回调等待重试成功并开始播放。 */ () => expect(screen.getByRole('button', { name: '暂停语音消息' })).toBeTruthy());
    expect(recorderHarness.initWithUrl).toHaveBeenCalledTimes(2);
  });

  test('另一条语音开始时暂停当前播放器', /* 当前测试回调验证同页语音互斥播放。 */ async () => {
    render(<>
      <AudioMessage messageKey="voice-a" src="https://cdn.example/a.amr" outgoing={false} />
      <AudioMessage messageKey="voice-b" src="https://cdn.example/b.amr" outgoing={false} />
    </>);
    // playButtons 按 DOM 顺序保存两条语音的初始播放按钮。
    const playButtons = screen.getAllByRole('button', { name: '播放语音消息' });
    fireEvent.click(playButtons[0]);
    await waitFor(/* 当前轮询回调等待首条语音进入播放态。 */ () => expect(recorderHarness.play).toHaveBeenCalledTimes(1));
    act(/* 当前回调模拟另一条语音取得播放权。 */ () => window.dispatchEvent(new CustomEvent('ydisks:chat-audio-play', { detail: 'voice-b' })));
    expect(recorderHarness.pause).toHaveBeenCalledTimes(1);
  });
});

describe('audio helpers', /* 当前测试套件覆盖媒体地址和时长格式化边界。 */ () => {
  test('只在 HTTPS 页面升级阿里云 OSS 的语音协议', /* 当前测试回调验证混合内容兼容范围。 */ () => {
    vi.stubGlobal('location', { protocol: 'https:' });
    expect(secureAudioURL('http://voice.oss-cn-hangzhou.aliyuncs.com/a.amr?token=1')).toBe('https://voice.oss-cn-hangzhou.aliyuncs.com/a.amr?token=1');
    expect(secureAudioURL('http://example.com/a.amr')).toBe('http://example.com/a.amr');
  });

  test('把播放器秒数格式化为固定宽度时间', /* 当前测试回调验证正常、跨分钟和非法秒数。 */ () => {
    expect(formatAudioTime(3.8)).toBe('0:03');
    expect(formatAudioTime(65)).toBe('1:05');
    expect(formatAudioTime(Number.NaN)).toBe('0:00');
  });
});
