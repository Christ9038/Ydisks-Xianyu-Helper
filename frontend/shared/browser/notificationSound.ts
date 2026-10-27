import { useCallback,useEffect,useState } from 'react';

/** NOTIFICATION_SOUND_PREFERENCE_KEY 保存当前浏览器的新消息提示音开关，不写入服务端配置。 */
export const NOTIFICATION_SOUND_PREFERENCE_KEY = 'ydisks.browser_notifications.sound';

/** NOTIFICATION_SOUND_CHANGE_EVENT 用于同一标签页内同步设置页与消息入口的提示音偏好。 */
const NOTIFICATION_SOUND_CHANGE_EVENT = 'ydisks:notification-sound-preference-changed';

/** NOTIFICATION_SOUND_NOTES 是默认提示音的两个音符：频率（Hz）、起始偏移（秒）和持续时长（秒）。 */
const NOTIFICATION_SOUND_NOTES: ReadonlyArray<{
  /** frequency 是音符频率，单位 Hz。 */
  frequency: number;
  /** offset 是相对提示音起点的起始偏移，单位秒。 */
  offset: number;
  /** duration 是音符持续时长，单位秒。 */
  duration: number;
}> = [
  { frequency: 880, offset: 0, duration: 0.14 },
  { frequency: 1318.5, offset: 0.12, duration: 0.2 },
];

/** NOTIFICATION_SOUND_VOLUME 是提示音峰值增益，保持较低避免惊扰。 */
const NOTIFICATION_SOUND_VOLUME = 0.18;

/** notificationSoundContext 缓存页面级 AudioContext，避免每条消息都创建新的音频上下文。 */
let notificationSoundContext: AudioContext | null = null;

/** notificationSoundMemoryPreference 保存存储受限时本页面生命周期内的最后一次用户选择。 */
let notificationSoundMemoryPreference: boolean | null = null;

/** readNotificationSoundEnabled 读取提示音开关，从未设置过时默认开启。 */
export const readNotificationSoundEnabled = (): boolean => {
  if (typeof window === 'undefined') return false;
  if (notificationSoundMemoryPreference !== null) return notificationSoundMemoryPreference;
  try {
    // stored 保存浏览器本地存储中的原始开关文本。
    const stored = window.localStorage.getItem(NOTIFICATION_SOUND_PREFERENCE_KEY);
    return stored === null ? true : stored === 'true';
  } catch {
    return true;
  }
};

/** writeNotificationSoundEnabled 写入提示音开关并通知同页消费者；存储不可用时退回内存偏好。 */
export const writeNotificationSoundEnabled = (enabled: boolean): void => {
  notificationSoundMemoryPreference = enabled;
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(NOTIFICATION_SOUND_PREFERENCE_KEY, String(enabled));
    // 本地存储写入成功后以存储为准，释放内存覆盖值。
    notificationSoundMemoryPreference = null;
  } catch {
    // 隐私模式禁止写入本地存储时，本次页面生命周期继续使用内存偏好。
  }
  window.dispatchEvent(new Event(NOTIFICATION_SOUND_CHANGE_EVENT));
};

/** getNotificationSoundContext 懒创建并返回页面级 AudioContext；浏览器不支持 Web Audio 时返回空值。 */
const getNotificationSoundContext = (): AudioContext | null => {
  if (typeof window === 'undefined') return null;
  if (notificationSoundContext) return notificationSoundContext;
  // audioContextCtor 保存当前浏览器的 AudioContext 构造器，旧 WebKit 使用带前缀名称。
  const audioContextCtor = window.AudioContext ?? (window as unknown as {
    /** webkitAudioContext 是旧版 WebKit 的带前缀音频上下文构造器。 */
    webkitAudioContext?: typeof AudioContext;
  }).webkitAudioContext;
  if (!audioContextCtor) return null;
  try {
    notificationSoundContext = new audioContextCtor();
  } catch {
    return null;
  }
  return notificationSoundContext;
};

/** playNotificationSound 合成并播放默认提示音，返回是否已提交播放；自动播放策略拦截时静默失败。 */
export const playNotificationSound = (): boolean => {
  // context 保存用于合成提示音的页面级音频上下文。
  const context = getNotificationSoundContext();
  if (!context) return false;
  try {
    // 未经用户手势创建的上下文处于 suspended，尝试恢复；被策略拒绝时由 catch 兜底。
    if (context.state === 'suspended') void context.resume().catch(/* 当前回调忽略自动播放策略导致的恢复失败。 */ () => undefined);
    // startAt 是提示音整体的起始时刻，略微延后以便 resume 生效。
    const startAt = context.currentTime + 0.02;
    for (const /* note 是当前要合成的音符参数。 */ note of NOTIFICATION_SOUND_NOTES) {
      // oscillator 产生当前音符的正弦波；gain 负责淡入淡出避免爆音。
      const oscillator = context.createOscillator();
      // gain 是当前音符的音量包络节点。
      const gain = context.createGain();
      oscillator.type = 'sine';
      oscillator.frequency.value = note.frequency;
      gain.gain.setValueAtTime(0.0001, startAt + note.offset);
      gain.gain.exponentialRampToValueAtTime(NOTIFICATION_SOUND_VOLUME, startAt + note.offset + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, startAt + note.offset + note.duration);
      oscillator.connect(gain);
      gain.connect(context.destination);
      oscillator.start(startAt + note.offset);
      oscillator.stop(startAt + note.offset + note.duration + 0.02);
    }
    return true;
  } catch {
    return false;
  }
};

/** playNotificationSoundIfEnabled 仅在用户开启提示音时播放，供通知入口调用。 */
export const playNotificationSoundIfEnabled = (): boolean => (readNotificationSoundEnabled() ? playNotificationSound() : false);

/** NotificationSoundPreferenceState 描述设置页可直接消费的提示音偏好状态。 */
export type NotificationSoundPreferenceState = {
  /** enabled 表示当前浏览器是否播放新消息提示音。 */
  enabled: boolean;
  /** setEnabled 更新提示音开关；开启时立即试听一次，同时满足浏览器的用户手势要求。 */
  setEnabled: (enabled: boolean) => void;
  /** preview 由用户手势触发，试听当前提示音。 */
  preview: () => void;
};

/** useNotificationSoundPreference 管理提示音开关，并同步同页和跨标签页的变更。 */
export const useNotificationSoundPreference = (): NotificationSoundPreferenceState => {
  /** enabled 保存当前浏览器的提示音开关。 */
  const [enabled, setEnabledState] = useState(readNotificationSoundEnabled);

  useEffect(/* 当前副作用同步同页和跨标签页的提示音偏好，并在卸载时移除监听。 */ () => {
    /** syncPreference 重新读取开关，避免旧标签页覆盖最新选择。 */
    const syncPreference = (): void => setEnabledState(readNotificationSoundEnabled());
    window.addEventListener(NOTIFICATION_SOUND_CHANGE_EVENT, syncPreference);
    window.addEventListener('storage', syncPreference);
    return /* 当前清理函数释放提示音偏好监听。 */ () => {
      window.removeEventListener(NOTIFICATION_SOUND_CHANGE_EVENT, syncPreference);
      window.removeEventListener('storage', syncPreference);
    };
  }, []);

  /** setEnabled 响应开关点击，开启时立刻试听。 */
  const setEnabled = useCallback(/* 当前回调写入偏好并在开启时试听。 */ (nextEnabled: boolean): void => {
    writeNotificationSoundEnabled(nextEnabled);
    if (nextEnabled) playNotificationSound();
  }, []);

  return { enabled, setEnabled, preview: playNotificationSound };
};

/** resetNotificationSoundForTest 清空模块级缓存，仅供测试隔离用例。 */
export const resetNotificationSoundForTest = (): void => {
  notificationSoundContext = null;
  notificationSoundMemoryPreference = null;
};
