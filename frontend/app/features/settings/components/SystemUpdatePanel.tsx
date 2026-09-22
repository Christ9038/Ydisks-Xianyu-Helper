import { Container,Download,ExternalLink,RefreshCw,ShieldAlert } from 'lucide-react';
import React,{ useEffect,useRef,useState } from 'react';

import { applyLatestStableUpdate,checkSystemUpdate,getSystemUpdateStatus,type SystemUpdateSnapshot } from '../api';

// activeOperationStatuses 保存需要持续轮询的宿主机任务状态。
const activeOperationStatuses = new Set<SystemUpdateSnapshot['operation']['status']>(['checking','queued','running']);

// blockedReasonText 将稳定原因码转换为管理员可执行的说明。
const blockedReasonText: Record<string,string> = {
  updater_unavailable: '当前部署未安装宿主机更新组件，只能查看版本信息。',
  custom_image: '当前运行的是自定义镜像，自动切换到官方正式版可能覆盖定制内容，因此已阻止一键更新。',
  dev_image: '开发版镜像不能通过此入口升级到正式版。',
  development_image: '开发版镜像不能通过此入口升级到正式版。',
  unknown_image: '无法确认当前镜像来源，自动更新已阻止。',
  invalid_current_version: '当前构建不是可比较的正式版本，自动更新已阻止。',
  current_version_newer: '当前版本高于最新正式版，不会执行降级。',
  already_latest: '当前已是最新正式版。',
  unsupported_database: '当前数据库类型尚不支持安全自动更新，请按部署文档手动备份和升级。',
  unsupported_storage: 'SQLite 数据目录不是更新器支持的固定宿主机绑定目录，请按部署文档手动升级。',
  current_unhealthy: '当前应用容器尚未通过健康检查，自动更新已阻止。',
  stable_unpinned: '当前正式版未固定到不可变镜像摘要，请先按部署文档完成安全接管。',
  migration_incompatible: '新版本数据库迁移预检未通过，系统不会修改当前容器或数据库。',
  update_in_progress: '已有更新任务正在执行，请等待当前任务结束。',
};

// operationText 把执行器状态映射为简短中文标签。
const operationText: Record<SystemUpdateSnapshot['operation']['status'],string> = {
  idle: '空闲',
  checking: '检查中',
  queued: '等待执行',
  running: '更新中',
  succeeded: '已完成',
  failed: '失败',
  rolled_back: '已回滚',
};

// emptyText 统一处理执行器尚未返回的可选版本文本。
const emptyText = (value: string): string => value.trim() || '尚未检查';

// SystemUpdatePanel 展示 Docker 正式版检查和受限一键更新入口。
export const SystemUpdatePanel: React.FC = () => {
  // snapshot 保存最近一次从服务端取得的系统更新状态。
  const [snapshot,setSnapshot] = useState<SystemUpdateSnapshot | null>(null);
  // loading 表示首次状态读取尚未结束。
  const [loading,setLoading] = useState(true);
  // checking 表示管理员正在主动刷新最新版本。
  const [checking,setChecking] = useState(false);
  // applying 表示更新任务受理请求正在提交。
  const [applying,setApplying] = useState(false);
  // error 保存当前面板可恢复的请求错误。
  const [error,setError] = useState('');
  // actionController 保存最近一次按钮请求，组件卸载时会主动取消。
  const actionController = useRef<AbortController | null>(null);

  useEffect(/* effect 在面板挂载时读取一次只读更新状态。 */ () => {
    // controller 负责取消页面离开后仍在进行的状态查询。
    const controller = new AbortController();
    getSystemUpdateStatus({ signal: controller.signal })
      .then(/* nextSnapshot 是服务端返回的初始更新状态。 */ nextSnapshot => {
        setSnapshot(nextSnapshot);
        setError('');
      })
      .catch(/* requestError 是初始状态查询失败原因。 */ requestError => {
        if (!controller.signal.aborted) setError(requestError instanceof Error ? requestError.message : '读取更新状态失败');
      })
      .finally(/* 完成回调只在请求未取消时结束首屏加载。 */ () => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return /* cleanup 取消挂载期间未完成的状态查询和按钮请求。 */ () => {
      controller.abort();
      actionController.current?.abort();
    };
  },[]);

  useEffect(/* effect 在更新任务活跃期间每两秒读取一次状态。 */ () => {
    if (!snapshot || !activeOperationStatuses.has(snapshot.operation.status)) return undefined;
    // controller 负责取消本轮周期轮询中正在进行的状态请求。
    const controller = new AbortController();
    // requestRunning 防止慢请求跨越两个周期后形成并发轮询。
    let requestRunning = false;
    // poll 读取任务最新状态；容器重启期间失败后下个周期会继续重试。
    const poll = (): void => {
      if (requestRunning) return;
      requestRunning = true;
      getSystemUpdateStatus({ signal: controller.signal })
        .then(/* nextSnapshot 是任务执行期间的新状态。 */ nextSnapshot => {
          setSnapshot(nextSnapshot);
          setError('');
        })
        .catch(/* requestError 是轮询失败原因，下一次手动操作仍可恢复。 */ requestError => {
          if (!controller.signal.aborted) setError(requestError instanceof Error ? requestError.message : '刷新更新状态失败');
        })
        .finally(/* pollFinished 允许下一轮周期请求继续执行。 */ () => {
          requestRunning = false;
        });
    };
    // timer 每两秒触发一次受并发保护的状态查询。
    const timer = window.setInterval(poll,2000);
    return /* cleanup 清理过期轮询，防止状态更新后并发请求。 */ () => {
      window.clearInterval(timer);
      controller.abort();
    };
  },[snapshot]);

  // handleCheck 主动刷新 GitHub Release 正式版信息。
  const handleCheck = async (): Promise<void> => {
    actionController.current?.abort();
    // controller 隔离本次检查请求和后续按钮操作。
    const controller = new AbortController();
    actionController.current = controller;
    setChecking(true);
    setError('');
    try {
      // nextSnapshot 是执行器重新验证后的最新版本状态。
      const nextSnapshot = await checkSystemUpdate({ signal: controller.signal });
      setSnapshot(nextSnapshot);
    } catch (requestError /* requestError 是正式版检查失败原因。 */) {
      if (!controller.signal.aborted) setError(requestError instanceof Error ? requestError.message : '检查更新失败');
    } finally {
      if (!controller.signal.aborted) setChecking(false);
    }
  };

  // handleApply 在二次确认后提交固定的最新正式版更新命令。
  const handleApply = async (): Promise<void> => {
    if (!window.confirm('更新会先备份和预检数据库，并短暂重启应用容器与账号连接。确认继续更新到最新正式版吗？')) return;
    actionController.current?.abort();
    // controller 隔离本次更新任务受理请求。
    const controller = new AbortController();
    actionController.current = controller;
    setApplying(true);
    setError('');
    try {
      // nextSnapshot 是宿主机接受任务后的排队或执行状态。
      const nextSnapshot = await applyLatestStableUpdate({ signal: controller.signal });
      setSnapshot(nextSnapshot);
    } catch (requestError /* requestError 是更新任务拒绝或受理失败原因。 */) {
      if (!controller.signal.aborted) setError(requestError instanceof Error ? requestError.message : '提交更新任务失败');
    } finally {
      if (!controller.signal.aborted) setApplying(false);
    }
  };

  // operationActive 表示当前应禁止重复检查或提交更新。
  const operationActive = snapshot ? activeOperationStatuses.has(snapshot.operation.status) : false;
  // updateButtonVisible 只在执行器明确允许且确有新版本时展示更新命令。
  const updateButtonVisible = snapshot?.available === true && snapshot.can_update === true;

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
          <span className="p-1.5 rounded-lg bg-emerald-600 text-white"><Container className="w-4 h-4" /></span>
          系统更新
        </h3>
        <button type="button" onClick={/* 点击事件主动检查最新正式版。 */ () => void handleCheck()} disabled={loading || checking || applying || operationActive} className="p-2 text-gray-500 hover:text-gray-800 disabled:opacity-40" title="检查更新" aria-label="检查更新">
          <RefreshCw className={`w-4 h-4 ${checking ? 'animate-spin' : ''}`} />
        </button>
      </div>

      <div className="ios-card rounded-xl bg-white p-6 space-y-5">
        {loading && <p className="text-sm text-gray-500">读取版本信息中...</p>}
        {!loading && snapshot && (
          <>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
              <div><p className="text-xs font-bold text-gray-400">当前版本</p><p className="mt-1 font-extrabold text-gray-900">{emptyText(snapshot.current.version)}</p><p className="mt-1 text-xs text-gray-500">提交 {emptyText(snapshot.current.commit)}</p></div>
              <div><p className="text-xs font-bold text-gray-400">最新正式版</p><p className="mt-1 font-extrabold text-gray-900">{emptyText(snapshot.latest.tag || snapshot.latest.version)}</p>{snapshot.latest.release_url && <a href={snapshot.latest.release_url} target="_blank" rel="noreferrer" className="mt-1 inline-flex items-center gap-1 text-xs font-bold text-brand hover:underline">查看发布说明 <ExternalLink className="w-3 h-3" /></a>}</div>
            </div>

            <div className="border-t border-gray-100 pt-4 flex items-start justify-between gap-4">
              <div><p className="text-xs font-bold text-gray-400">任务状态</p><p className="mt-1 text-sm font-bold text-gray-800">{operationText[snapshot.operation.status]}</p>{snapshot.operation.message && <p className="mt-1 text-xs leading-5 text-gray-500">{snapshot.operation.message}</p>}</div>
              {operationActive && <RefreshCw className="mt-1 h-4 w-4 shrink-0 animate-spin text-brand" />}
            </div>

            {snapshot.blocked_reason && !snapshot.can_update && (
              <div className="flex items-start gap-2 border-t border-gray-100 pt-4 text-sm text-amber-800"><ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" /><p>{blockedReasonText[snapshot.blocked_reason] || '当前部署策略不允许自动更新。'}</p></div>
            )}

            {updateButtonVisible && (
              <div className="border-t border-gray-100 pt-4 space-y-3">
                <p className="text-xs leading-5 text-gray-500">更新器会固定到发布清单中的镜像摘要，先备份并预检数据库；执行时应用容器和账号连接会短暂重启。</p>
                <button type="button" onClick={/* 点击事件提交固定最新正式版更新任务。 */ () => void handleApply()} disabled={applying || operationActive} className="w-full bg-emerald-600 hover:bg-emerald-700 text-white px-5 py-3 rounded-xl font-bold text-sm flex items-center justify-center gap-2 disabled:opacity-40">
                  <Download className="w-4 h-4" />
                  {applying ? '正在提交...' : `更新到 ${emptyText(snapshot.latest.tag || snapshot.latest.version)}`}
                </button>
              </div>
            )}
          </>
        )}
        {error && <p role="alert" className="text-sm text-red-600">{error}</p>}
      </div>
    </section>
  );
};
