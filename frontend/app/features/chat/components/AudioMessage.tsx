import { Loader2, Pause, Play, RotateCcw } from 'lucide-react';
import React from 'react';

import BenzAMRRecorder from 'benz-amr-recorder';

/** AudioMessageProps 描述一条已归一化闲鱼语音消息的播放输入。 */
interface AudioMessageProps {
  /** messageKey 是消息稳定键，用于在多条语音之间协调互斥播放。 */
  messageKey: string;
  /** src 是平台返回的 AMR 媒体地址，不包含账号 Cookie 等登录凭证。 */
  src: string;
  /** outgoing 控制语音气泡使用本人或对方的视觉方向。 */
  outgoing: boolean;
  /** initialDuration 是平台载荷提供的秒级时长，首次点击前用于直接展示语音长度。 */
  initialDuration?: number;
}

/** AudioPlaybackState 描述语音从未加载、异步解码到播放结束的短暂 UI 状态。 */
type AudioPlaybackState = 'idle' | 'loading' | 'playing' | 'paused' | 'error';

/** audioPlaybackEventName 是同页语音播放器之间广播互斥播放请求的事件名称。 */
const audioPlaybackEventName = 'ydisks:chat-audio-play';

/** waveformBars 保存紧凑语音波形的相对高度，避免用随机布局造成每次渲染跳动。 */
const waveformBars = [6, 10, 15, 8, 17, 12, 7, 16, 11, 6, 14, 9];

/**
 * secureAudioURL 在 HTTPS 页面中只升级阿里云 OSS 的 HTTP 媒体地址，避免混合内容拦截。
 * source 是平台返回的原始媒体地址；返回值保持查询参数不变，无法解析时原样返回。
 */
export function secureAudioURL(source: string): string {
  // trimmedURL 去除协议载荷外围空白，防止合法 URL 因格式噪声加载失败。
  const trimmedURL = source.trim();
  if (typeof location === 'undefined' || location.protocol !== 'https:') return trimmedURL;
  try {
    // mediaURL 用标准 URL 解析器保留签名查询参数，仅允许改写已验证支持 TLS 的 OSS 域名。
    const mediaURL = new URL(trimmedURL);
    if (mediaURL.protocol === 'http:' && mediaURL.hostname.endsWith('.aliyuncs.com')) {
      mediaURL.protocol = 'https:';
    }
    return mediaURL.toString();
  } catch (parseError /* parseError 仅说明平台媒体地址不是有效绝对 URL，不向界面暴露其内容。 */) {
    return trimmedURL;
  }
}

/**
 * formatAudioTime 把播放器提供的秒数格式化为 mm:ss。
 * seconds 是当前进度或总时长；非有限值按零处理，返回固定宽度时间文本。
 */
export function formatAudioTime(seconds: number): string {
  // safeSeconds 是已取整且不小于零的可展示秒数。
  const safeSeconds = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  // minutes 是完整分钟部分。
  const minutes = Math.floor(safeSeconds / 60);
  // remainderSeconds 是不足一分钟的秒数部分。
  const remainderSeconds = safeSeconds % 60;
  return `${minutes}:${remainderSeconds.toString().padStart(2, '0')}`;
}

/** announceAudioPlayback 通知同一聊天页的其他语音暂停，messageKey 标识新取得播放权的消息。 */
function announceAudioPlayback(messageKey: string): void {
  window.dispatchEvent(new CustomEvent<string>(audioPlaybackEventName, { detail: messageKey }));
}

/** AudioMessage 提供 AMR 延迟解码、播放/暂停、进度反馈、失败重试及组件卸载清理。 */
export const AudioMessage: React.FC<AudioMessageProps> = ({ messageKey, src, outgoing, initialDuration = 0 }) => {
  /** playerRef 拥有当前消息的 AMR 解码器；组件卸载或地址变化时负责 destroy。 */
  const playerRef = React.useRef<BenzAMRRecorder | null>(null);
  /** generationRef 标识最近一次异步初始化，晚到的媒体下载或解码结果不得覆盖新消息状态。 */
  const generationRef = React.useRef(0);
  /** playbackState 和 setPlaybackState 保存仅供当前气泡展示的播放生命周期状态。 */
  const [playbackState, setPlaybackState] = React.useState<AudioPlaybackState>('idle');
  /** duration 和 setDuration 保存平台载荷或解码器确认的总时长，单位为秒，不属于服务端消息状态。 */
  const [duration, setDuration] = React.useState(initialDuration);
  /** progress 和 setProgress 保存播放进度，单位为秒，由播放期间的短定时器更新。 */
  const [progress, setProgress] = React.useState(0);

  /**
   * 地址变化或组件卸载时销毁解码器并使未完成初始化失效；底层 XHR 无取消接口，代次检查负责隔离晚到结果。
   */
  React.useEffect(/* 当前回调登记媒体地址对应的解码器释放责任。 */ () => {
    return /* 当前清理回调在地址变化或组件卸载时销毁当前解码器。 */ () => {
      generationRef.current += 1;
      playerRef.current?.destroy();
      playerRef.current = null;
    };
  }, [src]);

  /** 服务端刷新同一条语音的时长时同步初始展示值，播放后的解码结果仍可覆盖该值。 */
  React.useEffect(/* 当前回调同步服务端提供的秒级语音时长。 */ () => {
    setDuration(Math.max(0, initialDuration));
  }, [initialDuration]);

  /** 监听同页其他语音取得播放权的事件，并在清理阶段移除全局监听器。 */
  React.useEffect(/* 当前回调同步同页语音播放器的互斥事件监听。 */ () => {
    /** pauseForOtherMessage 在另一条消息开始播放时暂停当前解码器，避免多个声音重叠。 */
    const pauseForOtherMessage = (event: Event): void => {
      // activeMessageKey 是新取得播放权的消息键；类型转换仅发生在本模块自定义事件边界。
      const activeMessageKey = (event as CustomEvent<string>).detail;
      // player 是当前组件拥有的解码器；未初始化或已暂停时无需动作。
      const player = playerRef.current;
      if (activeMessageKey !== messageKey && player?.isPlaying()) player.pause();
    };
    window.addEventListener(audioPlaybackEventName, pauseForOtherMessage);
    return /* 当前清理回调移除播放器互斥事件监听。 */ () => window.removeEventListener(audioPlaybackEventName, pauseForOtherMessage);
  }, [messageKey]);

  /** 播放期间轮询解码器进度；暂停、结束或卸载时立即清理定时器。 */
  React.useEffect(/* 当前回调只在播放态创建进度轮询定时器。 */ () => {
    if (playbackState !== 'playing') return undefined;
    // progressTimer 每 100 毫秒同步一次可见进度，不参与音频时序控制。
    const progressTimer = window.setInterval(/* 当前定时回调从解码器读取最新播放位置。 */ () => {
      // player 是本次轮询读取的当前解码器，销毁后不再更新 UI。
      const player = playerRef.current;
      if (player) setProgress(player.getCurrentPosition());
    }, 100);
    return /* 当前清理回调在播放态结束时释放进度定时器。 */ () => window.clearInterval(progressTimer);
  }, [playbackState]);

  /** bindPlayerEvents 把解码器事件映射为当前代次的 React 状态，避免陈旧实例回写。 */
  const bindPlayerEvents = (player: BenzAMRRecorder, generation: number): void => {
    /** isCurrentGeneration 判断回调所属解码器是否仍由当前气泡持有。 */
    const isCurrentGeneration = (): boolean => generationRef.current === generation && playerRef.current === player;
    player.onPlay(/* 当前回调在音频从头开始时切换播放态。 */ () => {
      if (isCurrentGeneration()) setPlaybackState('playing');
    });
    player.onResume(/* 当前回调在暂停后继续时恢复播放态。 */ () => {
      if (isCurrentGeneration()) setPlaybackState('playing');
    });
    player.onPause(/* 当前回调在用户或互斥规则暂停时保留当前进度。 */ () => {
      if (isCurrentGeneration()) setPlaybackState('paused');
    });
    player.onStop(/* 当前回调在主动停止时复位到语音开头。 */ () => {
      if (!isCurrentGeneration()) return;
      setPlaybackState('idle');
      setProgress(0);
    });
    player.onEnded(/* 当前回调在自然播放结束时恢复可重新播放状态。 */ () => {
      if (!isCurrentGeneration()) return;
      setPlaybackState('idle');
      setProgress(0);
    });
  };

  /** handleToggle 响应播放按钮：已播放时暂停，已暂停时继续，首次点击才下载并解码 AMR 数据。 */
  const handleToggle = async (): Promise<void> => {
    // currentPlayer 是按钮点击时已经存在的解码器实例。
    const currentPlayer = playerRef.current;
    if (currentPlayer?.isPlaying()) {
      currentPlayer.pause();
      return;
    }
    if (currentPlayer?.isPaused()) {
      announceAudioPlayback(messageKey);
      currentPlayer.resume();
      return;
    }
    if (currentPlayer?.isInit()) {
      announceAudioPlayback(messageKey);
      currentPlayer.play();
      return;
    }
    if (playbackState === 'loading') return;

    // generation 是本次动态导入和网络解码的代次，地址切换或卸载会令其失效。
    const generation = generationRef.current + 1;
    generationRef.current = generation;
    setPlaybackState('loading');
    setProgress(0);
    try {
      // player 拥有这条消息的解码缓存；解码器随聊天路由加载，媒体数据仍只在首次点击时下载。
      const player = new BenzAMRRecorder();
      playerRef.current = player;
      bindPlayerEvents(player, generation);
      await player.initWithUrl(secureAudioURL(src));
      if (generationRef.current !== generation || playerRef.current !== player) {
        player.destroy();
        return;
      }
      setDuration(player.getDuration() || Math.max(0, initialDuration));
      announceAudioPlayback(messageKey);
      player.play();
    } catch (loadError /* loadError 可能来自跨域下载或 AMR 解码，界面仅显示可重试提示。 */) {
      if (generationRef.current !== generation) return;
      playerRef.current?.destroy();
      playerRef.current = null;
      setPlaybackState('error');
      setProgress(0);
    }
  };

  // visibleSeconds 在播放或暂停时显示当前进度，其余状态显示平台或解码器提供的总时长。
  const visibleSeconds = playbackState === 'playing' || playbackState === 'paused' ? progress : duration;
  // timeLabel 在静止状态突出总时长，播放时再补充当前进度，避免占用单行语音气泡的垂直空间。
  const timeLabel = duration > 0 ? (playbackState === 'playing' || playbackState === 'paused' ? `${formatAudioTime(visibleSeconds)} / ${formatAudioTime(duration)}` : formatAudioTime(duration)) : '--:--';
  // statusLabel 为播放器状态提供不依赖图标和颜色的可读说明。
  const statusLabel = playbackState === 'loading' ? '正在解码'
    : playbackState === 'playing' ? '正在播放'
      : playbackState === 'paused' ? '已暂停'
        : playbackState === 'error' ? '加载失败，点击重试'
          : '语音消息';
  // buttonLabel 为播放按钮提供完整的辅助技术操作名称。
  const buttonLabel = playbackState === 'playing' ? '暂停语音消息'
    : playbackState === 'paused' ? '继续播放语音消息'
      : playbackState === 'error' ? '重新加载语音消息'
        : playbackState === 'loading' ? '正在加载语音消息'
          : '播放语音消息';

  return (
    <div className={`flex h-11 min-w-56 items-center gap-2 rounded-2xl border px-2 shadow-sm ${outgoing ? 'rounded-br-md border-sky-400 bg-sky-500 text-white' : 'rounded-bl-md border-slate-200 bg-white text-slate-800'}`}>
      <button
        type="button"
        aria-label={buttonLabel}
        title={buttonLabel}
        disabled={playbackState === 'loading'}
        onClick={/* 当前回调把用户点击交给可重试的异步播放流程。 */ () => void handleToggle()}
        className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full transition focus:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 disabled:cursor-wait ${outgoing ? 'bg-white text-sky-600 hover:bg-sky-50 focus-visible:ring-white' : 'bg-sky-50 text-sky-600 hover:bg-sky-100 focus-visible:ring-sky-400'}`}
      >
        {playbackState === 'loading' ? <Loader2 className="h-4 w-4 animate-spin" />
          : playbackState === 'playing' ? <Pause className="h-4 w-4 fill-current" />
            : playbackState === 'error' ? <RotateCcw className="h-4 w-4" />
              : <Play className="ml-px h-4 w-4 fill-current" />}
      </button>
      <div className="min-w-0 flex flex-1 items-center gap-2">
        <div className="flex h-5 flex-1 items-center gap-0.5" aria-hidden="true">
          {waveformBars.map(/* barHeight 和 barIndex 分别决定稳定波形柱的像素高度与 React 键。 */ (barHeight, barIndex) => (
            <span
              key={`${messageKey}-${barIndex}`}
              className={`w-0.5 rounded-full ${playbackState === 'playing' ? 'animate-pulse' : ''} ${outgoing ? 'bg-sky-100' : playbackState === 'error' ? 'bg-red-300' : 'bg-sky-400'}`}
              style={{ height: `${barHeight}px`, animationDelay: `${barIndex * 45}ms` }}
            />
          ))}
        </div>
        <span className={`shrink-0 font-mono text-[11px] font-semibold tabular-nums ${outgoing ? 'text-sky-100' : playbackState === 'error' ? 'text-red-500' : 'text-slate-500'}`}>{timeLabel}</span>
        <span className="sr-only">{statusLabel}</span>
      </div>
    </div>
  );
};
