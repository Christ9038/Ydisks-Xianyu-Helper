import { useCallback,useEffect,useState } from 'react';

/** BROWSER_NOTIFICATION_PREFERENCE_KEY 保存当前浏览器的系统通知开关，不写入服务端配置。 */
export const BROWSER_NOTIFICATION_PREFERENCE_KEY = 'ydisks.browser_notifications.enabled';

/** BROWSER_NOTIFICATION_CHANGE_EVENT 用于同一标签页内同步聊天页与应用壳的通知偏好。 */
const BROWSER_NOTIFICATION_CHANGE_EVENT = 'ydisks:browser-notification-preference-changed';

/** BrowserNotificationPermission 统一表示浏览器通知权限以及当前环境不支持的状态。 */
export type BrowserNotificationPermission = NotificationPermission | 'unsupported';

/** BrowserNotificationPayload 描述系统通知需要展示的最小内容。 */
export type BrowserNotificationPayload = {
  /** 通知标题，通常包含买家名称或账号名称。 */
  title: string;
  /** 通知正文，不应包含账号凭证等敏感信息。 */
  body: string;
  /** 通知去重标签，用于让浏览器识别同一聊天会话。 */
  tag?: string;
};

/** BrowserNotificationUpdateResult 表示一次通知偏好更新后的真实权限结果。 */
export type BrowserNotificationUpdateResult = {
  /** enabled 表示本地偏好是否最终处于开启状态。 */
  enabled: boolean;
  /** permission 表示浏览器返回的系统通知权限。 */
  permission: BrowserNotificationPermission;
};

/** BrowserNotificationPreferenceState 描述聊天页可直接消费的通知偏好状态。 */
export type BrowserNotificationPreferenceState = {
  /** enabled 表示当前浏览器是否允许应用发出系统通知。 */
  enabled: boolean;
  /** supported 表示当前浏览器是否提供 Notification API。 */
  supported: boolean;
  /** permission 表示当前浏览器的系统通知权限。 */
  permission: BrowserNotificationPermission;
  /** updating 表示正在等待用户完成系统权限选择或本地偏好写入。 */
  updating: boolean;
  /** error 保存开启通知失败时的用户可见说明。 */
  error: string;
  /** setEnabled 响应设置页的通知开关操作，并在首次开启时请求权限。 */
  setEnabled: (enabled: boolean) => Promise<void>;
};

/** getNotificationAPI 读取浏览器通知构造器；服务端渲染或旧浏览器环境返回空值。 */
const getNotificationAPI = (): typeof Notification | null => {
  if (typeof window === 'undefined' || !('Notification' in window)) return null;
  return window.Notification;
};

/** readBrowserNotificationEnabled 读取当前浏览器保存的通知开关，缺省保持关闭。 */
export const readBrowserNotificationEnabled = (): boolean => {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem(BROWSER_NOTIFICATION_PREFERENCE_KEY) === 'true';
  } catch {
    return false;
  }
};

/** getBrowserNotificationPermission 读取系统通知权限，不触发权限申请弹窗。 */
export const getBrowserNotificationPermission = (): BrowserNotificationPermission => {
  // notificationAPI 保存当前浏览器的通知构造器；不存在时由工具统一返回 unsupported。
  const notificationAPI = getNotificationAPI();
  return notificationAPI?.permission || 'unsupported';
};

/** publishBrowserNotificationPreference 通知同页消费者重新读取本地偏好。 */
const publishBrowserNotificationPreference = (): void => {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(BROWSER_NOTIFICATION_CHANGE_EVENT));
};

/** persistBrowserNotificationEnabled 写入浏览器偏好；存储不可用时保持内存行为而不阻断消息流。 */
const persistBrowserNotificationEnabled = (enabled: boolean): void => {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(BROWSER_NOTIFICATION_PREFERENCE_KEY, String(enabled));
  } catch {
    // 隐私模式禁止写入本地存储时，通知权限仍可由本次页面生命周期使用。
  }
};

/** updateBrowserNotificationPreference 更新本地偏好，并在首次开启时由用户手势触发权限申请。 */
export const updateBrowserNotificationPreference = async (enabled: boolean): Promise<BrowserNotificationUpdateResult> => {
  if (!enabled) {
    persistBrowserNotificationEnabled(false);
    publishBrowserNotificationPreference();
    return { enabled: false, permission: getBrowserNotificationPermission() };
  }

  // notificationAPI 保存当前环境可用的通知构造器，权限申请只在支持时继续。
  const notificationAPI = getNotificationAPI();
  if (!notificationAPI) {
    persistBrowserNotificationEnabled(false);
    publishBrowserNotificationPreference();
    return { enabled: false, permission: 'unsupported' };
  }

  // permission 保存浏览器当前通知权限，default 状态需要由用户手势触发申请。
  let permission = notificationAPI.permission;
  if (permission === 'default') permission = await notificationAPI.requestPermission();
  // granted 表示本次权限结果是否允许应用继续持久化开启偏好。
  const granted = permission === 'granted';
  persistBrowserNotificationEnabled(granted);
  publishBrowserNotificationPreference();
  return { enabled: granted, permission };
};

/** showBrowserNotification 在偏好开启且权限允许时创建一条系统通知，返回是否成功创建。 */
export const showBrowserNotification = (payload: BrowserNotificationPayload): boolean => {
  // notificationAPI 保存创建系统通知所需的浏览器 API。
  const notificationAPI = getNotificationAPI();
  if (!notificationAPI || !readBrowserNotificationEnabled() || notificationAPI.permission !== 'granted') return false;
  try {
    // notification 保存当前消息对应的系统通知实例。
    const notification = new notificationAPI(payload.title, {
      body: payload.body,
      tag: payload.tag,
    });
    // focusNotificationWindow 负责用户点击系统通知后的窗口聚焦行为。
    const focusNotificationWindow = (): void => window.focus();
    notification.onclick = focusNotificationWindow;
    return true;
  } catch {
    return false;
  }
};

/** useBrowserNotificationPreference 管理当前浏览器通知状态，并清理跨标签页与同页同步监听。 */
export const useBrowserNotificationPreference = (): BrowserNotificationPreferenceState => {
  /** enabled 保存当前浏览器的通知开关，而非服务端共享配置。 */
  const [enabled, setEnabledState] = useState(() => readBrowserNotificationEnabled());
  /** permission 保存当前浏览器通知权限，权限改变时由设置页重新渲染提示。 */
  const [permission, setPermission] = useState<BrowserNotificationPermission>(() => getBrowserNotificationPermission());
  /** updating 防止一次权限请求尚未结束时重复点击开关。 */
  const [updating, setUpdating] = useState(false);
  /** error 保存通知能力不可用或权限被拒绝时的用户可见说明。 */
  const [error, setError] = useState('');

  useEffect(/* 当前副作用同步同页和跨标签页的通知偏好，并在卸载时移除监听。 */ () => {
    /** syncPreference 从浏览器存储重新读取开关和权限，避免旧标签页覆盖最新选择。 */
    const syncPreference = (): void => {
      setEnabledState(readBrowserNotificationEnabled());
      setPermission(getBrowserNotificationPermission());
    };
    window.addEventListener(BROWSER_NOTIFICATION_CHANGE_EVENT, syncPreference);
    window.addEventListener('storage', syncPreference);
    return /* 当前清理函数释放通知偏好监听，避免卸载后继续写入 React 状态。 */ () => {
      window.removeEventListener(BROWSER_NOTIFICATION_CHANGE_EVENT, syncPreference);
      window.removeEventListener('storage', syncPreference);
    };
  }, []);

  /** setEnabled 响应开关点击，只有用户主动开启时才请求浏览器系统权限。 */
  const setEnabled = useCallback(/* 当前回调响应开关点击并隔离异步权限请求的更新状态。 */ async (nextEnabled: boolean): Promise<void> => {
    setUpdating(true);
    setError('');
    try {
      // result 保存浏览器权限请求和本地偏好写入后的最终状态。
      const result = await updateBrowserNotificationPreference(nextEnabled);
      setEnabledState(result.enabled);
      setPermission(result.permission);
      if (nextEnabled && !result.enabled) {
        setError(result.permission === 'denied' ? '浏览器已拒绝通知，请在地址栏的站点权限中重新允许。' : '当前浏览器不支持系统通知。');
      }
    } catch {
      persistBrowserNotificationEnabled(false);
      publishBrowserNotificationPreference();
      setEnabledState(false);
      setPermission(getBrowserNotificationPermission());
      setError('请求系统通知权限失败，请稍后重试。');
    } finally {
      setUpdating(false);
    }
  }, []);

  return {
    enabled,
    supported: getNotificationAPI() !== null,
    permission,
    updating,
    error,
    setEnabled,
  };
};
