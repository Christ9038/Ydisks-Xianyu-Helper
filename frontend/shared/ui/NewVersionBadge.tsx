import { ArrowUp } from 'lucide-react';
import React from 'react';

/** releasePageBase 是官方仓库的 Release 页面前缀；标签经 encodeURIComponent 拼接，避免外部返回值破坏链接。 */
const releasePageBase = 'https://github.com/Christ9038/Ydisks-Xianyu-Helper/releases/tag/';

/** NewVersionBadgeProps 描述新版本提示徽标的输入。 */
export interface NewVersionBadgeProps {
  /** latestVersion 是 GitHub 上更高的最新正式版本标签，如 v1.2.3。 */
  latestVersion: string;
  /** variant 为 arrow 时显示红色上箭头，为 dot 时只显示一个无浮窗、无链接的红点（侧边栏折叠态）。 */
  variant?: 'arrow' | 'dot';
  /** popoverClassName 控制悬浮窗相对徽标的弹出位置。 */
  popoverClassName?: string;
}

/** NewVersionBadge 展示“有新版本”红色提示；悬停或聚焦时弹出带前往 GitHub Release 页面链接的浮窗，只提示、不执行更新。 */
const NewVersionBadge: React.FC<NewVersionBadgeProps> = ({ latestVersion, variant = 'arrow', popoverClassName = 'bottom-full left-0' }) => {
  // href 是新版本对应的 GitHub Release 页面。
  const href = `${releasePageBase}${encodeURIComponent(latestVersion)}`;
  if (variant === 'dot') return <span className="inline-block h-2 w-2 rounded-full bg-red-500" role="img" aria-label={`有新版本 ${latestVersion}`} />;
  return (
    <span className="group relative inline-flex shrink-0 items-center self-center">
      <a href={href} target="_blank" rel="noopener noreferrer" aria-label={`有新版本 ${latestVersion}，前往 GitHub Release 页面`} className="inline-flex items-center">
        <ArrowUp className="h-3.5 w-3.5 text-red-500" strokeWidth={3} />
      </a>
      {/* 浮窗用 pb/pl 透明内边距衔接徽标，鼠标从徽标移向链接时不会中途消失。 */}
      <span role="tooltip" className={`invisible absolute z-40 pb-2 opacity-0 transition group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100 ${popoverClassName}`}>
        <span className="block whitespace-nowrap rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-semibold normal-case tracking-normal text-slate-700 shadow-lg">
          <span className="block">有新版本 <span className="font-mono text-red-500">{latestVersion}</span></span>
          <a href={href} target="_blank" rel="noopener noreferrer" className="mt-1 block text-sky-600 hover:underline">前往 GitHub Release 页面 →</a>
        </span>
      </span>
    </span>
  );
};

export default NewVersionBadge;
