// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { NOTIFICATION_SOUND_PREFERENCE_KEY,playNotificationSound,playNotificationSoundIfEnabled,readNotificationSoundEnabled,resetNotificationSoundForTest,useNotificationSoundPreference,writeNotificationSoundEnabled } from './notificationSound';

// oscillatorStarts 记录测试中被启动的振荡器数量，用于断言提示音确实提交播放。
let oscillatorStarts = 0;
// contextState 模拟 AudioContext 当前状态，用于覆盖 suspended 恢复分支。
let contextState: AudioContextState = 'running';
// resumeMock 记录对被挂起上下文的恢复调用。
const resumeMock = vi.fn(async () => undefined);

/** FakeAudioContext 模拟 Web Audio 的最小节点图。 */
class FakeAudioContext {
  /** currentTime 是模拟的音频时钟。 */
  currentTime = 0;
  /** destination 是模拟的输出节点。 */
  destination = {};
  /** state 返回测试设定的上下文状态。 */
  get state(): AudioContextState { return contextState; }
  /** resume 记录恢复调用。 */
  resume = resumeMock;
  /** createOscillator 创建可记录启动次数的振荡器替身。 */
  createOscillator() {
    return { type: '', frequency: { value: 0 }, connect: vi.fn(), start: () => { oscillatorStarts += 1; }, stop: vi.fn() };
  }
  /** createGain 创建增益节点替身。 */
  createGain() {
    return { gain: { setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() }, connect: vi.fn() };
  }
}

describe('notification sound', /* 当前测试组验证默认提示音偏好与播放行为。 */ () => {
  beforeEach(/* 当前回调重置存储、模块缓存与音频替身。 */ () => {
    window.localStorage.clear();
    resetNotificationSoundForTest();
    oscillatorStarts = 0;
    contextState = 'running';
    resumeMock.mockClear();
    vi.stubGlobal('AudioContext', FakeAudioContext);
    (window as unknown as {
      /** AudioContext 是被测试替换的音频上下文构造器。 */
      AudioContext: unknown;
    }).AudioContext = FakeAudioContext;
  });

  afterEach(/* 当前回调清理全局替身。 */ () => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  test('默认开启并可持久化关闭', /* 当前回调验证缺省值与写入。 */ () => {
    expect(readNotificationSoundEnabled()).toBe(true);
    writeNotificationSoundEnabled(false);
    expect(window.localStorage.getItem(NOTIFICATION_SOUND_PREFERENCE_KEY)).toBe('false');
    expect(readNotificationSoundEnabled()).toBe(false);
  });

  test('关闭时不播放，开启时播放两个音符', /* 当前回调验证开关门禁。 */ () => {
    writeNotificationSoundEnabled(false);
    expect(playNotificationSoundIfEnabled()).toBe(false);
    expect(oscillatorStarts).toBe(0);
    writeNotificationSoundEnabled(true);
    expect(playNotificationSoundIfEnabled()).toBe(true);
    expect(oscillatorStarts).toBe(2);
  });

  test('被挂起的上下文会尝试恢复', /* 当前回调验证自动播放策略处理。 */ () => {
    contextState = 'suspended';
    expect(playNotificationSound()).toBe(true);
    expect(resumeMock).toHaveBeenCalledTimes(1);
  });

  test('浏览器不支持 Web Audio 时静默返回 false', /* 当前回调验证降级。 */ () => {
    (window as unknown as {
      /** AudioContext 被移除以模拟不支持 Web Audio 的浏览器。 */
      AudioContext: unknown;
    }).AudioContext = undefined;
    expect(playNotificationSound()).toBe(false);
  });

  test('存储受限时退回内存偏好', /* 当前回调验证隐私模式。 */ () => {
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(/* 当前回调模拟写入被禁止。 */ () => { throw new Error('blocked'); });
    writeNotificationSoundEnabled(false);
    expect(readNotificationSoundEnabled()).toBe(false);
  });

  test('读取存储失败时默认开启', /* 当前回调验证读取异常。 */ () => {
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(/* 当前回调模拟读取被禁止。 */ () => { throw new Error('blocked'); });
    expect(readNotificationSoundEnabled()).toBe(true);
  });

  test('Hook 开启时试听并同步状态', /* 当前回调验证设置页交互。 */ () => {
    writeNotificationSoundEnabled(false);
    // hook 保存提示音偏好 Hook 的测试实例。
    const hook = renderHook(/* 当前回调创建 Hook。 */ () => useNotificationSoundPreference());
    expect(hook.result.current.enabled).toBe(false);
    act(/* 当前回调模拟用户打开开关。 */ () => hook.result.current.setEnabled(true));
    expect(hook.result.current.enabled).toBe(true);
    expect(oscillatorStarts).toBe(2);
    act(/* 当前回调模拟用户关闭开关。 */ () => hook.result.current.setEnabled(false));
    expect(hook.result.current.enabled).toBe(false);
    expect(oscillatorStarts).toBe(2);
  });
});
