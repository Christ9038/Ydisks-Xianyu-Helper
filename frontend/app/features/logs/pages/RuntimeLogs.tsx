import React,{ useEffect,useRef,useState } from 'react';
import { ChevronDown,ChevronRight,Pause,Play,RefreshCw,ScrollText,Trash2 } from 'lucide-react';
import { useRuntimeLogs } from '../hooks';
import {
  RUNTIME_LOG_LEVEL_OPTIONS,
  RUNTIME_LOG_SOURCE_OPTIONS,
  formatRuntimeLogTime,
  runtimeLogContextItems,
  runtimeLogHeadline,
  runtimeLogLevelBadgeClass,
  runtimeLogLevelLabel,
  runtimeLogLevelRowClass,
  runtimeLogRetentionHint,
  runtimeLogSourceLabel,
} from '../models';
import type { RuntimeLogEntry,RuntimeLogLevel,RuntimeLogSource } from '../types';
import { PageHeader } from '../../../../shared/ui/PageHeader';

/** RuntimeLogRowProps 描述单条运行日志行的渲染输入。 */
interface RuntimeLogRowProps {
  /** entry 是待展示的运行日志。 */
  entry: RuntimeLogEntry;
}

/** RuntimeLogRow 渲染单条运行日志及其可展开的附加属性。 */
const RuntimeLogRow: React.FC<RuntimeLogRowProps> = ({ entry }) => {
  // [expanded, setExpanded] 保存当前日志行附加属性的展开状态。
  const [expanded, setExpanded] = useState(false);
  // contextItems 是当前日志行的键值标签集合。
  const contextItems = runtimeLogContextItems(entry);
  // canExpand 表示当前日志行是否存在可展开的附加属性。
  const canExpand = entry.details.length > 0;
  return (
    <li className={`border-l-4 px-4 py-2.5 ${runtimeLogLevelRowClass(entry.level)}`}>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className={`rounded px-1.5 py-0.5 font-mono font-bold ${runtimeLogLevelBadgeClass(entry.level)}`}>{runtimeLogLevelLabel(entry.level)}</span>
        <span className="font-mono text-slate-400">{formatRuntimeLogTime(entry.time)}</span>
        <span className="rounded bg-slate-100 px-1.5 py-0.5 font-bold text-slate-600">{runtimeLogSourceLabel(entry.source)}</span>
        <span className="min-w-0 flex-1 truncate font-mono font-bold text-slate-800" title={runtimeLogHeadline(entry)}>{runtimeLogHeadline(entry)}</span>
        <span className="text-slate-600">{entry.message}</span>
        {canExpand && (
          <button
            type="button"
            onClick={/* toggleExpanded 切换当前日志行附加属性的展开状态。 */ () => setExpanded(/* previous 是变更前的展开状态。 */ previous => !previous)}
            aria-expanded={expanded}
            aria-label={expanded ? '收起附加属性' : '展开附加属性'}
            className="ml-auto flex items-center gap-1 rounded px-1.5 py-0.5 text-slate-500 hover:bg-slate-100 hover:text-slate-800"
          >
            {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
            {`属性 ${entry.details.length}`}
          </button>
        )}
      </div>
      {contextItems.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-slate-500">
          {contextItems.map(/* item 是当前日志行的一个键值标签。 */ item => (
            <span key={item.label} className="inline-flex items-center gap-1">
              <span className="text-slate-400">{item.label}</span>
              <span className="font-mono text-slate-700">{item.value}</span>
            </span>
          ))}
        </div>
      )}
      {entry.error && <div className="mt-1.5 break-all font-mono text-[11px] text-red-600">{entry.error}</div>}
      {expanded && (
        <dl className="mt-2 space-y-1 rounded-lg bg-slate-50 p-2 text-[11px]">
          {entry.details.map(/* detail 是当前日志行的一条附加属性。 */ detail => (
            <div key={detail.key} className="flex gap-2">
              <dt className="w-32 shrink-0 truncate font-mono text-slate-400" title={detail.key}>{detail.key}</dt>
              <dd className="min-w-0 break-all font-mono text-slate-700">{detail.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </li>
  );
};

/** RuntimeLogs 渲染管理员运行日志页面：过滤、暂停、清屏与增量日志流。 */
const RuntimeLogs: React.FC = () => {
  // runtimeLogs 是运行日志 Hook 暴露的状态与操作。
  const runtimeLogs = useRuntimeLogs();
  // [followLatest, setFollowLatest] 保存是否自动滚动到最新日志。
  const [followLatest, setFollowLatest] = useState(true);
  // listRef 指向日志列表容器，用于跟随最新日志时滚动到底部。
  const listRef = useRef<HTMLUListElement | null>(null);
  // retentionHint 是服务端缓冲区保留提示；为空表示无需提示。
  const retentionHint = runtimeLogRetentionHint(runtimeLogs.capacity, runtimeLogs.dropped);
  // entryCount 是当前已加载的日志条数，仅用于触发跟随滚动。
  const entryCount = runtimeLogs.entries.length;

  useEffect(/* followEffect 在跟随最新且日志数量变化时把列表滚动到底部。 */ () => {
    if (!followLatest) return;
    // container 是当前日志列表容器；尚未挂载时无需滚动。
    const container = listRef.current;
    if (!container) return;
    container.scrollTop = container.scrollHeight;
  }, [followLatest, entryCount]);

  return (
    <div className="min-w-0 space-y-8 animate-fade-in">
      <PageHeader title="运行日志" description="查看内部接口、闲鱼接口与后台子系统的最近运行日志">
        <div className="flex items-center gap-2">
          {runtimeLogs.loading && <span className="text-xs font-bold text-slate-400" role="status">加载中</span>}
          <button
            type="button"
            onClick={/* togglePause 切换自动轮询的暂停状态。 */ () => runtimeLogs.setPaused(!runtimeLogs.paused)}
            className="flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2 font-bold text-gray-700 transition-colors hover:bg-gray-200"
          >
            {runtimeLogs.paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
            {runtimeLogs.paused ? '继续' : '暂停'}
          </button>
          <button
            type="button"
            onClick={/* reloadLogs 从零游标重新加载当前过滤条件下的日志。 */ () => runtimeLogs.reload()}
            className="flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2 font-bold text-gray-700 transition-colors hover:bg-gray-200"
          >
            <RefreshCw className="h-4 w-4" />
            刷新
          </button>
          <button
            type="button"
            onClick={/* clearLogs 清空当前展示的日志，游标保持不变。 */ () => runtimeLogs.clear()}
            className="flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2 font-bold text-gray-700 transition-colors hover:bg-red-50 hover:text-red-600"
          >
            <Trash2 className="h-4 w-4" />
            清屏
          </button>
        </div>
      </PageHeader>

      <div className="ios-card rounded-xl bg-white p-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
          <label className="space-y-1">
            <span className="block text-xs font-bold text-gray-500">最低等级</span>
            <select
              value={runtimeLogs.query.level}
              onChange={/* changeLevel 更新最低等级过滤条件。 */ event => runtimeLogs.setQuery({ level: event.target.value as RuntimeLogLevel | '' })}
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm"
            >
              <option value="">全部等级</option>
              {RUNTIME_LOG_LEVEL_OPTIONS.map(/* option 是一个等级过滤选项。 */ option => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label className="space-y-1">
            <span className="block text-xs font-bold text-gray-500">来源</span>
            <select
              value={runtimeLogs.query.source}
              onChange={/* changeSource 更新来源过滤条件。 */ event => runtimeLogs.setQuery({ source: event.target.value as RuntimeLogSource | '' })}
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm"
            >
              <option value="">全部来源</option>
              {RUNTIME_LOG_SOURCE_OPTIONS.map(/* option 是一个来源过滤选项。 */ option => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label className="space-y-1 sm:col-span-2">
            <span className="block text-xs font-bold text-gray-500">关键字</span>
            <input
              type="search"
              value={runtimeLogs.query.keyword}
              onChange={/* changeKeyword 更新关键字过滤条件。 */ event => runtimeLogs.setQuery({ keyword: event.target.value })}
              placeholder="按接口名、路径、错误文本或属性值筛选"
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm"
            />
          </label>
        </div>
        <div className="mt-3 flex items-center justify-between gap-3 text-xs text-gray-500">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={followLatest}
              onChange={/* toggleFollowLatest 切换是否自动滚动到最新日志。 */ event => setFollowLatest(event.target.checked)}
            />
            自动滚动到最新
          </label>
          <span>已加载 {entryCount} 条{runtimeLogs.capacity > 0 ? ` / 服务端保留 ${runtimeLogs.capacity} 条` : ''}</span>
        </div>
      </div>

      {retentionHint && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">{retentionHint}</div>
      )}
      {runtimeLogs.error && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">
          <span>{runtimeLogs.error}</span>
          <button
            type="button"
            onClick={/* retryLoad 重新发起一次增量读取。 */ () => runtimeLogs.reload()}
            className="font-bold underline"
          >
            重试
          </button>
        </div>
      )}

      <div className="ios-card overflow-hidden rounded-xl bg-white">
        {entryCount === 0 ? (
          <div className="flex min-h-[16rem] flex-col items-center justify-center gap-2 text-sm text-gray-400" role="status">
            <ScrollText className="h-8 w-8 text-gray-300" />
            <span>{runtimeLogs.error ? '暂无法读取运行日志' : '当前过滤条件下还没有运行日志'}</span>
            <span className="text-xs">日志等级设为 info 时只记录失败与警告；需要每次接口调用明细请把系统日志等级调为 debug</span>
          </div>
        ) : (
          <ul ref={listRef} className="max-h-[60vh] divide-y divide-slate-100 overflow-y-auto">
            {runtimeLogs.entries.map(/* entry 是当前待展示的一条运行日志。 */ entry => (
              <RuntimeLogRow key={entry.seq} entry={entry} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
};

export default RuntimeLogs;
