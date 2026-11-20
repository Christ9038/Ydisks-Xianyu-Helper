import { useCallback,useEffect,useRef,useState } from 'react';
import type { NotificationAccountOption,NotificationChannel,SystemSettings } from './api';
import { createNotificationChannel,deleteNotificationChannel,getMessageNotifications,getNotificationAccountOptions,getNotificationChannel,getNotificationChannels,getSystemSettings,setChannelAccounts,testNotificationChannel,updateNotificationChannel,updateSystemSettings } from './api';
import { buildNotificationPayload,emptyNotificationForm,isCurrentNotificationRequest,normalizeNotificationForm,notificationErrorMessage,validateNotificationForm } from './state';
import type { NotificationForm,NotificationState } from './types';

// useNotifications 统一管理通知渠道、事件订阅和系统 SMTP 的异步状态。
export const useNotifications = (isAdmin: boolean): NotificationState => {
  // channels 保存当前用户可用的通知渠道。
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  // loading 表示通知渠道列表是否正在加载。
  const [loading, setLoading] = useState(true);
  // showModal 表示渠道新建或编辑弹窗是否打开。
  const [showModal, setShowModal] = useState(false);
  // editing 保存当前正在编辑的渠道。
  const [editing, setEditing] = useState<NotificationChannel | null>(null);
  // saving 表示渠道保存或删除动作是否正在执行。
  const [saving, setSaving] = useState(false);
  // testingId 保存当前正在测试发送的渠道 ID。
  const [testingId, setTestingId] = useState('');
  // toast 保存当前短暂操作提示。
  const [toast, setToast] = useState<NotificationState['toast']>(null);
  // smtp 保存系统级邮件发送配置。
  const [smtp, setSmtp] = useState<SystemSettings>({});
  // smtpSaving 表示系统 SMTP 保存是否正在执行。
  const [smtpSaving, setSmtpSaving] = useState(false);
  // showSmtpPassword 控制系统 SMTP 密码是否明文显示。
  const [showSmtpPassword, setShowSmtpPassword] = useState(false);
  // showChannelSmtpPassword 控制独立 SMTP 密码是否明文显示。
  const [showChannelSmtpPassword, setShowChannelSmtpPassword] = useState(false);
  // form 保存渠道编辑弹窗的当前表单。
  const [form, setForm] = useState<NotificationForm>(emptyNotificationForm);
  // accountOptions 保存可供渠道绑定的账号选项。
  const [accountOptions, setAccountOptions] = useState<NotificationAccountOption[]>([]);
  // accountsStatus 表示账号选项与当前绑定是否加载完成；失败时保存不会改动任何绑定，避免用空选择误清空。
  const [accountsStatus, setAccountsStatus] = useState<'loading' | 'ready' | 'error'>('loading');
  // accountsGeneration 隔离连续打开弹窗或重试时产生的旧账号响应。
  const accountsGeneration = useRef(0);
  // accountsAbort 保存当前账号选项请求的取消控制器。
  const accountsAbort = useRef<AbortController | null>(null);
  // accountsChannelId 记录账号选项所属的渠道（新建为空串），供重试使用。
  const accountsChannelId = useRef('');
  // channelGeneration 隔离渠道加载和刷新产生的旧响应。
  const channelGeneration = useRef(0);
  // channelAbort 保存当前渠道列表请求的取消控制器。
  const channelAbort = useRef<AbortController | null>(null);
  // smtpGeneration 隔离 SMTP 设置切换管理员状态后的旧响应。
  const smtpGeneration = useRef(0);
  // smtpAbort 保存当前 SMTP 设置请求的取消控制器。
  const smtpAbort = useRef<AbortController | null>(null);
  // actionGeneration 隔离保存、删除和测试动作的旧响应。
  const actionGeneration = useRef(0);
  // actionAbort 保存当前渠道变更动作的取消控制器。
  const actionAbort = useRef<AbortController | null>(null);
  // toastTimer 保存提示自动消失的定时器句柄。
  const toastTimer = useRef<number | null>(null);
  // editorGeneration 隔离连续打开不同渠道时产生的旧编辑配置响应。
  const editorGeneration = useRef(0);
  // editorAbort 保存当前编辑配置请求的取消控制器。
  const editorAbort = useRef<AbortController | null>(null);

  // showToast 展示短暂提示并清理上一条提示的定时器。
  const showToast = useCallback(
    // 提示回调写入提示状态并安排自动清理。
    (type: NonNullable<NotificationState['toast']>['type'], text: string) => {
    setToast({ type, text });
    if (toastTimer.current !== null) window.clearTimeout(toastTimer.current);
    toastTimer.current = window.setTimeout(
      // toastCleanup 在提示展示三秒后清理状态。
      () => {
      toastTimer.current = null;
      setToast(null);
      },
      3000,
    );
    },
    [],
  );

  // loadChannels 加载通知渠道并丢弃过期响应。
  const loadChannels = useCallback(
    // 渠道加载回调读取最新通知渠道列表。
    async () => {
    // generation 标记本次渠道查询的代次。
    const generation = ++channelGeneration.current;
    channelAbort.current?.abort();
    // controller 允许刷新或卸载时取消渠道请求。
    const controller = new AbortController();
    channelAbort.current = controller;
    setLoading(true);
    try {
      // result 是当前用户的通知渠道列表响应。
      const result = await getNotificationChannels({ signal: controller.signal });
      if (!isCurrentNotificationRequest(generation, channelGeneration.current)) return;
      setChannels(result.data || []);
    } catch (error: unknown /* 渠道加载异常 */) {
      if (isCurrentNotificationRequest(generation, channelGeneration.current) && !controller.signal.aborted) {
        console.error('加载通知渠道失败', error);
      }
    } finally {
      if (isCurrentNotificationRequest(generation, channelGeneration.current)) setLoading(false);
    }
    },
    [],
  );

  // loadSmtp 加载管理员可见的系统 SMTP 配置并隔离过期响应。
  const loadSmtp = useCallback(
    // SMTP 加载回调读取管理员系统设置。
    async () => {
    // generation 标记本次 SMTP 查询的代次。
    const generation = ++smtpGeneration.current;
    smtpAbort.current?.abort();
    // controller 允许管理员状态变化时取消 SMTP 查询。
    const controller = new AbortController();
    smtpAbort.current = controller;
    try {
      // settings 是系统 SMTP 配置快照。
      const settings = await getSystemSettings({ signal: controller.signal });
      if (isCurrentNotificationRequest(generation, smtpGeneration.current)) setSmtp(settings || {});
    } catch (error: unknown /* SMTP 加载异常 */) {
      if (isCurrentNotificationRequest(generation, smtpGeneration.current) && !controller.signal.aborted) {
        console.error('加载 SMTP 配置失败', error);
      }
    }
    },
    [],
  );

  useEffect(
    // 通知数据副作用并行加载渠道和管理员 SMTP 配置。
    () => {
      void loadChannels();
      if (isAdmin) {
        void loadSmtp();
      } else {
        smtpGeneration.current += 1;
        smtpAbort.current?.abort();
        setSmtp({});
      }
      return (
        // 通知数据清理回调取消渠道和 SMTP 请求。
        () => {
          channelGeneration.current += 1;
          channelAbort.current?.abort();
          smtpGeneration.current += 1;
          smtpAbort.current?.abort();
          editorGeneration.current += 1;
          editorAbort.current?.abort();
          accountsGeneration.current += 1;
          accountsAbort.current?.abort();
        }
      );
    },
    [isAdmin, loadChannels, loadSmtp],
  );

  useEffect(
    // 卸载清理负责终止渠道动作的展示生命周期并释放提示定时器。
    () => (
      // toastCleanup 在页面卸载时取消渠道动作、使旧结果失效并释放定时器。
      () => {
        actionGeneration.current += 1;
        actionAbort.current?.abort();
        if (toastTimer.current !== null) window.clearTimeout(toastTimer.current);
      }
    ),
    [],
  );

  // loadAccounts 并行读取账号选项和（编辑时）该渠道当前绑定的账号，成功后一次性写入选项与表单选择；新建渠道默认勾选全部账号，使新渠道开箱即用。
  const loadAccounts = useCallback(
    // channelId 是被编辑渠道的标识，新建时为空串。
    async (channelId: string) => {
    // generation 标记本次账号加载，隔离重试或切换渠道后的旧响应。
    const generation = ++accountsGeneration.current;
    accountsAbort.current?.abort();
    // controller 允许关闭弹窗或重新加载时取消账号请求。
    const controller = new AbortController();
    accountsAbort.current = controller;
    accountsChannelId.current = channelId;
    setAccountsStatus('loading');
    try {
      // options 是全部账号选项；bindings 是各账号现有的渠道绑定，仅编辑时读取。
      const [options, bindings] = await Promise.all([
        getNotificationAccountOptions({ signal: controller.signal }),
        channelId ? getMessageNotifications() : Promise.resolve(null),
      ]);
      if (generation !== accountsGeneration.current || controller.signal.aborted) return;
      // selected 是表单初始勾选的账号：编辑取该渠道已启用绑定，新建默认全选。
      const selected = bindings
        ? options.filter(/* option 仅保留当前渠道已启用绑定的账号。 */ option => bindings.data.some(/* binding 是一条账号渠道绑定。 */ binding => binding.cookie_id === option.id && String(binding.channel_id) === channelId && binding.enabled)).map(/* option 取账号标识。 */ option => option.id)
        : options.map(/* option 取账号标识。 */ option => option.id);
      setAccountOptions(options);
      setForm(/* current 写入初始账号选择，保留用户已填的其他字段。 */ current => ({ ...current, account_ids: selected }));
      setAccountsStatus('ready');
    } catch (error: unknown /* 账号选项加载失败：保持非 ready，保存时不改动绑定。 */) {
      if (generation === accountsGeneration.current && !controller.signal.aborted) {
        console.error('加载账号绑定选项失败', error);
        setAccountsStatus('error');
      }
    }
    },
    [],
  );

  // reloadAccounts 重试加载当前弹窗对应渠道的账号选项。
  const reloadAccounts = useCallback(
    // 重试回调复用最近一次加载所属的渠道。
    () => loadAccounts(accountsChannelId.current),
    [loadAccounts],
  );

  // openCreate 打开新建渠道表单并清理上一次编辑状态。
  const openCreate = useCallback(
    // 新建回调重置表单并打开渠道弹窗。
    () => {
    editorGeneration.current += 1;
    editorAbort.current?.abort();
    setEditing(null);
    setForm(emptyNotificationForm());
    setShowChannelSmtpPassword(false);
    setShowModal(true);
    void loadAccounts('');
    },
    [loadAccounts],
  );

  // openEdit 先读取脱敏编辑配置再开放表单；关闭、切换或新建使旧请求失效，避免晚到响应覆盖输入。
  const openEdit = useCallback(
    // channel 是本次编辑对象；加载期间关闭旧表单，只有仍属于当前代次的响应能打开新表单。
    async (channel: NotificationChannel) => {
      // generation 标记本次编辑加载，隔离取消与连续打开不同渠道的结果。
      const generation = ++editorGeneration.current;
      editorAbort.current?.abort();
      // controller 控制当前读取的取消，不参与保存请求的生命周期。
      const controller = new AbortController();
      editorAbort.current = controller;
      setShowModal(false);
      try {
        // editor 只含收件地址与模式标记，SMTP 秘密始终留在服务端。
        const editor = await getNotificationChannel(channel.id, { signal: controller.signal });
        if (generation !== editorGeneration.current || controller.signal.aborted) return;
        // initialForm 是读取完成后一次性建立的表单，之后不再由该请求回填。
        const initialForm = normalizeNotificationForm(channel, smtp);
        setEditing(channel);
        setForm(channel.type === 'email' ? {
          ...initialForm, preserveSMTP: true,
          config: { to_email: editor.to_email || '', use_custom_smtp: editor.use_custom_smtp === true },
        } : initialForm);
        setShowChannelSmtpPassword(false);
        setShowModal(true);
        void loadAccounts(channel.id);
      } catch (error: unknown /* 编辑读取失败时仅显示可读错误，不开放缺少配置的表单。 */) {
        if (generation === editorGeneration.current && !controller.signal.aborted) showToast('error', notificationErrorMessage(error, '读取渠道配置失败'));
      }
    },
    [loadAccounts, showToast, smtp],
  );

  // closeModal 关闭渠道弹窗并取消未完成的保存动作。
  const closeModal = useCallback(
    // 关闭回调使保存请求失效并隐藏弹窗。
    () => {
    actionGeneration.current += 1;
    setTestingId('');
    actionAbort.current?.abort();
    editorGeneration.current += 1;
    editorAbort.current?.abort();
    accountsGeneration.current += 1;
    accountsAbort.current?.abort();
    setSaving(false);
    setShowModal(false);
    },
    [],
  );

  // handleSave 校验并保存当前渠道表单，失败时保留表单供重试。
  const handleSave = useCallback(
    // 保存回调执行渠道校验、请求和成功刷新。
    async () => {
    // validationError 是渠道表单预检失败时的用户提示。
    const validationError = validateNotificationForm(form);
    if (validationError) {
      showToast('error', validationError);
      return;
    }
    // generation 标记当前渠道保存动作的代次。
    const generation = ++actionGeneration.current;
    // 替换动作时主动清理被取消测试的忙碌状态；旧 finally 已没有当前代次。
    setTestingId('');
    actionAbort.current?.abort();
    // controller 允许关闭弹窗时取消保存请求。
    const controller = new AbortController();
    actionAbort.current = controller;
    setSaving(true);
    try {
      // payload 是后端渠道接口使用的规范化请求体。
      const payload = buildNotificationPayload(form);
      // channelId 是被保存渠道的标识，用于随后的账号绑定写入。
      let channelId = editing?.id ?? '';
      if (editing) {
        await updateNotificationChannel(editing.id, payload, { signal: controller.signal });
      } else {
        // created 是创建响应，其数值主键转为路径参数字符串。
        const created = await createNotificationChannel({ ...payload, config: payload.config || {} }, { signal: controller.signal });
        channelId = String(created.id);
      }
      if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
      // bindingError 记录渠道已保存但账号绑定写入失败的原因；账号选项未加载成功时不写绑定，避免误清空。
      let bindingError = '';
      if (accountsStatus === 'ready' && channelId) {
        try {
          await setChannelAccounts(channelId, form.account_ids, { signal: controller.signal });
        } catch (error: unknown /* 绑定写入失败：渠道本身已保存，单独提示。 */) {
          bindingError = notificationErrorMessage(error, '账号绑定保存失败');
        }
        if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
      }
      setShowModal(false);
      await loadChannels();
      if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
      if (bindingError) showToast('error', `渠道已保存，但账号绑定失败：${bindingError}`);
      else showToast('success', editing ? '已更新' : '已创建');
    } catch (error: unknown /* 渠道保存异常 */) {
      if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
        showToast('error', notificationErrorMessage(error, '保存失败'));
      }
    } finally {
      if (isCurrentNotificationRequest(generation, actionGeneration.current)) setSaving(false);
    }
    },
    [accountsStatus, editing, form, loadChannels, showToast],
  );

  // handleDelete 删除渠道并在成功后刷新列表。
  const handleDelete = useCallback(
    // 删除回调执行渠道删除请求并刷新列表。
    async (channel: NotificationChannel) => {
    if (!confirm(`确认删除渠道「${channel.name}」吗？已绑定该渠道的账号会自动解绑。`)) return;
    // generation 标记当前删除动作的代次。
    const generation = ++actionGeneration.current;
    // 替换动作时主动清理被取消测试的忙碌状态；旧 finally 已没有当前代次。
    setTestingId('');
    actionAbort.current?.abort();
    // controller 允许刷新页面时取消删除请求。
    const controller = new AbortController();
    actionAbort.current = controller;
    try {
      await deleteNotificationChannel(channel.id, { signal: controller.signal });
      if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
      await loadChannels();
      if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
      showToast('success', '已删除');
    } catch (error: unknown /* 渠道删除异常 */) {
      if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
        showToast('error', notificationErrorMessage(error, '删除失败'));
      }
    }
    },
    [loadChannels, showToast],
  );

  // handleToggleEnabled 更新渠道启用状态并采用函数式局部更新。
  const handleToggleEnabled = useCallback(
    // 启用切换回调更新渠道状态并保留最新响应。
    async (channel: NotificationChannel) => {
    // generation 标记当前启用状态动作的代次。
    const generation = ++actionGeneration.current;
    // 替换动作时主动清理被取消测试的忙碌状态；旧 finally 已没有当前代次。
    setTestingId('');
    actionAbort.current?.abort();
    // controller 允许新的渠道动作取消旧请求。
    const controller = new AbortController();
    actionAbort.current = controller;
    try {
      await updateNotificationChannel(channel.id, { enabled: !channel.enabled }, { signal: controller.signal });
      if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
      setChannels(
        // nextChannelsUpdater 使用函数式更新渠道列表。
        current => current.map(
          // item 是当前待更新的渠道项。
          item => item.id === channel.id ? { ...item, enabled: !item.enabled } : item,
        ),
      );
    } catch (error: unknown /* 渠道状态异常 */) {
      if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
        showToast('error', notificationErrorMessage(error, '切换失败'));
      }
    }
    },
    [showToast],
  );

  // handleTest 发送渠道测试通知并展示结果。
  const handleTest = useCallback(
    // 测试回调发送测试通知并显示结果。
    async (channel: NotificationChannel) => {
    setTestingId(channel.id);
    // generation 标记当前测试通知动作的代次。
    const generation = ++actionGeneration.current;
    actionAbort.current?.abort();
    // controller 允许新动作取消旧的测试发送。
    const controller = new AbortController();
    actionAbort.current = controller;
    try {
      await testNotificationChannel(channel.id, { signal: controller.signal });
      if (isCurrentNotificationRequest(generation, actionGeneration.current)) showToast('success', '测试通知已发送，请检查对应渠道');
    } catch (error: unknown /* 测试通知异常 */) {
      if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
        showToast('error', notificationErrorMessage(error, '发送失败'));
      }
    } finally {
      if (isCurrentNotificationRequest(generation, actionGeneration.current)) setTestingId('');
    }
    },
    [showToast],
  );

  // handleSaveSmtp 保存系统 SMTP 配置并支持取消旧保存请求。
  const handleSaveSmtp = useCallback(
    // SMTP 保存回调提交系统邮件配置。
    async () => {
    // generation 标记当前 SMTP 保存动作的代次。
    const generation = ++smtpGeneration.current;
    smtpAbort.current?.abort();
    // controller 允许重复保存时取消上一轮请求。
    const controller = new AbortController();
    smtpAbort.current = controller;
    setSmtpSaving(true);
    try {
      const payload: SystemSettings = {
        smtp_server: smtp.smtp_server || '', smtp_port: smtp.smtp_port || 587, smtp_user: smtp.smtp_user || '',
        smtp_from_name: smtp.smtp_from_name || '',
        smtp_from_address: smtp.smtp_from_address || smtp.smtp_user || '', smtp_use_tls: smtp.smtp_use_tls !== false,
        smtp_use_ssl: smtp.smtp_use_ssl === true,
      };
      // 仅在管理员主动编辑过密码字段时提交，避免加载配置后意外清空服务端秘密。
      if (Object.prototype.hasOwnProperty.call(smtp, 'smtp_password')) {
        payload.smtp_password = smtp.smtp_password;
      }
      await updateSystemSettings(payload, { signal: controller.signal });
      if (isCurrentNotificationRequest(generation, smtpGeneration.current)) showToast('success', 'SMTP 配置已保存');
    } catch (error: unknown /* SMTP 保存异常 */) {
      if (isCurrentNotificationRequest(generation, smtpGeneration.current) && !controller.signal.aborted) {
        showToast('error', notificationErrorMessage(error, '保存失败'));
      }
    } finally {
      if (isCurrentNotificationRequest(generation, smtpGeneration.current)) setSmtpSaving(false);
    }
    },
    [showToast, smtp],
  );

  return {
    channels, loading, showModal, editing, saving, testingId, toast, smtp, smtpSaving,
    showSmtpPassword, showChannelSmtpPassword, form, accountOptions, accountsStatus, reloadAccounts, setForm, setSmtp, setShowSmtpPassword,
    setShowChannelSmtpPassword, loadChannels, openCreate, openEdit, closeModal, showToast,
    handleSave, handleDelete, handleToggleEnabled, handleTest, handleSaveSmtp,
  };
};
