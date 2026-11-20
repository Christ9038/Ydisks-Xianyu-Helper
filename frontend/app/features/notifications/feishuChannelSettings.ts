import { useCallback,useEffect,useRef,useState } from 'react';
import type { NotificationChannel } from './api';
import { getFeishuChannelCategories,updateFeishuChannelCategories } from './api';
import { notificationErrorMessage } from './state';

/** FeishuChannelSettingsOptions 描述飞书渠道分类设置 Hook 的页面回调。 */
export interface FeishuChannelSettingsOptions {
  /** onSaved 在保存成功后接收渠道标识，供页面刷新渠道列表并展示提示。 */
  onSaved?: (channelID: string) => void | Promise<void>;
}

/** FeishuChannelSettingsState 是飞书渠道分类设置弹窗的状态与动作边界。 */
export interface FeishuChannelSettingsState {
  /** open 表示设置弹窗是否已选择渠道并处于打开状态。 */
  open: boolean;
  /** channelID 是当前设置弹窗对应的通知渠道标识；关闭时为空字符串。 */
  channelID: string;
  /** channelName 是弹窗标题展示的渠道名称。 */
  channelName: string;
  /** loading 表示分类设置是否正在从服务端读取。 */
  loading: boolean;
  /** saving 表示分类设置是否正在保存。 */
  saving: boolean;
  /** warning 是警告类通知开关的当前草稿值。 */
  warning: boolean;
  /** info 是普通提示类通知开关的当前草稿值。 */
  info: boolean;
  /** error 是读取或保存失败的用户可读说明。 */
  error: string;
  /** loadFailed 表示读取失败且当前草稿不可信，弹窗应展示重试入口。 */
  loadFailed: boolean;
  /** openSettings 打开指定飞书渠道的设置弹窗并加载服务端开关。 */
  openSettings: (channel: NotificationChannel) => Promise<void>;
  /** reload 重新读取当前渠道的设置，用于读取失败后的重试。 */
  reload: () => Promise<void>;
  /** close 关闭弹窗并取消进行中的读取或保存请求。 */
  close: () => void;
  /** setWarning 更新警告类通知草稿，保存前不会写入服务端。 */
  setWarning: (value: boolean) => void;
  /** setInfo 更新普通提示类通知草稿，保存前不会写入服务端。 */
  setInfo: (value: boolean) => void;
  /** save 把当前警告类与普通提示类开关保存到服务端。 */
  save: () => Promise<void>;
}

// useFeishuChannelSettings 管理飞书应用渠道分类开关的加载、保存、取消和旧响应隔离。
export const useFeishuChannelSettings = (options: FeishuChannelSettingsOptions = {}): FeishuChannelSettingsState => {
  // channelID 保存当前设置弹窗对应的渠道标识；空字符串表示弹窗关闭。
  const [channelID, setChannelID] = useState('');
  // channelName 保存弹窗标题使用的渠道名称。
  const [channelName, setChannelName] = useState('');
  // loading 保存分类设置的读取状态。
  const [loading, setLoading] = useState(false);
  // saving 保存分类设置的提交状态。
  const [saving, setSaving] = useState(false);
  // warning 保存警告类通知草稿，默认值与后端 default true 一致。
  const [warning, setWarningState] = useState(true);
  // info 保存普通提示类通知草稿，默认值与后端 default true 一致。
  const [info, setInfoState] = useState(true);
  // error 保存当前读取或保存失败说明。
  const [error, setError] = useState('');
  // loadFailed 区分读取失败与保存失败，决定弹窗是否展示重试入口。
  const [loadFailed, setLoadFailed] = useState(false);
  // generationRef 保存设置流程代次；切换渠道或关闭弹窗后旧响应不能写回状态。
  const generationRef = useRef(0);
  // abortRef 保存当前读取或保存请求的取消控制器。
  const abortRef = useRef<AbortController | null>(null);
  // onSavedRef 保存最新的保存成功回调，避免异步动作闭包引用首次渲染的页面回调。
  const onSavedRef = useRef(options.onSaved);

  useEffect(
    // onSavedSync 在页面回调变化时同步最新引用。
    () => {
      onSavedRef.current = options.onSaved;
    },
    [options.onSaved],
  );

  // loadSettings 读取指定渠道的分类开关；切换渠道或关闭后晚到响应直接丢弃。
  const loadSettings = useCallback(
    // loadSettingsCallback 读取目标渠道的分类开关并写入草稿。
    async (
    // targetChannelID 是本次读取的渠道标识。
    targetChannelID: string,
    // targetChannelName 是弹窗标题使用的渠道名称。
    targetChannelName: string,
  ) => {
    generationRef.current += 1;
    // generation 是本次读取的代次，用于拒绝旧渠道响应。
    const generation = generationRef.current;
    abortRef.current?.abort();
    // controller 允许切换渠道或关闭弹窗时中止读取请求。
    const controller = new AbortController();
    abortRef.current = controller;
    setChannelID(targetChannelID);
    setChannelName(targetChannelName);
    setLoading(true);
    setSaving(false);
    setError('');
    setLoadFailed(false);
    try {
      // categories 是服务端保存的警告类与普通提示类开关。
      const categories = await getFeishuChannelCategories(targetChannelID, { signal: controller.signal });
      if (generation !== generationRef.current || controller.signal.aborted) return;
      setWarningState(categories.warning);
      setInfoState(categories.info);
    } catch (readError: unknown /* 读取失败时隐藏不可信的默认草稿，用户名可以通过重试重新读取。 */) {
      if (generation !== generationRef.current || controller.signal.aborted) return;
      setLoadFailed(true);
      setError(notificationErrorMessage(readError, '读取飞书通知设置失败'));
    } finally {
      if (generation === generationRef.current) setLoading(false);
    }
  }, []);

  // openSettings 打开指定飞书渠道的设置弹窗并开始读取分类开关。
  const openSettings = useCallback(
    // openSettingsCallback 以渠道标识和名称加载设置。
    async (channel: NotificationChannel) => {
      await loadSettings(channel.id, channel.name);
    },
    [loadSettings],
  );

  // reload 重新读取当前渠道设置，用于读取失败后的重试。
  const reload = useCallback(
    // reloadCallback 复用当前渠道标识和名称重新读取设置。
    async () => {
      if (!channelID) return;
      await loadSettings(channelID, channelName);
    },
    [channelID, channelName, loadSettings],
  );

  // close 关闭设置弹窗、取消进行中的请求并重置草稿与错误状态。
  const close = useCallback(
    // closeCallback 取消请求并对齐关闭后的初始状态。
    () => {
      generationRef.current += 1;
      abortRef.current?.abort();
      abortRef.current = null;
      setChannelID('');
      setChannelName('');
      setLoading(false);
      setSaving(false);
      setWarningState(true);
      setInfoState(true);
      setError('');
      setLoadFailed(false);
    },
    [],
  );

  // setWarning 更新警告类通知草稿，用户保存前不会影响服务端。
  const setWarning = useCallback(
    // setWarningCallback 写入警告类通知开关。
    (value: boolean) => {
      setWarningState(value);
    },
    [],
  );

  // setInfo 更新普通提示类通知草稿，用户保存前不会影响服务端。
  const setInfo = useCallback(
    // setInfoCallback 写入普通提示类通知开关。
    (value: boolean) => {
      setInfoState(value);
    },
    [],
  );

  // save 保存当前分类开关；成功后关闭弹窗并通知页面刷新渠道列表。
  const save = useCallback(
    // saveCallback 校验弹窗状态并提交当前分类草稿。
    async () => {
    if (!channelID || loading || saving || loadFailed) return;
    // savedChannelID 是保存成功后传给页面回调的渠道标识；关闭弹窗会先清空 channelID 状态。
    const savedChannelID = channelID;
    generationRef.current += 1;
    // generation 是本次保存动作的代次，用于拒绝关闭弹窗后晚到的成功响应。
    const generation = generationRef.current;
    abortRef.current?.abort();
    // controller 允许关闭弹窗时中止保存请求。
    const controller = new AbortController();
    abortRef.current = controller;
    setSaving(true);
    setError('');
    try {
      await updateFeishuChannelCategories(savedChannelID, { warning, info }, { signal: controller.signal });
      if (generation !== generationRef.current || controller.signal.aborted) return;
      setSaving(false);
      setChannelID('');
      setChannelName('');
      setLoading(false);
      setLoadFailed(false);
      setWarningState(true);
      setInfoState(true);
      // savedHandlerResult 是页面刷新回调的返回值；刷新失败不回滚已保存的服务端设置。
      const savedHandlerResult = onSavedRef.current?.(savedChannelID);
      void Promise.resolve(savedHandlerResult).catch(
        // savedHandlerFailure 忽略页面刷新回调错误，避免覆盖成功保存结果。
        () => undefined,
      );
    } catch (saveError: unknown /* 保存失败时保留弹窗和草稿，便于用户重试或取消。 */) {
      if (generation !== generationRef.current || controller.signal.aborted) return;
      setError(notificationErrorMessage(saveError, '保存飞书通知设置失败'));
    } finally {
      if (generation === generationRef.current) setSaving(false);
    }
  }, [channelID, info, loadFailed, loading, saving, warning]);

  useEffect(
    // cleanupEffect 在 Hook 卸载时取消读取或保存请求并令旧响应失效。
    () => {
      generationRef.current += 1;
      abortRef.current?.abort();
    },
    [],
  );

  return {
    open: channelID !== '', channelID, channelName, loading, saving, warning, info, error, loadFailed,
    openSettings, reload, close, setWarning, setInfo, save,
  };
};
