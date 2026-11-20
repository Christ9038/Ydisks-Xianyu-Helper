import React from 'react';
import { Monitor, Moon, Sun } from 'lucide-react';
import type { ThemePreference } from '../browser/themePreference';

// themeLabels 是各偏好对应的中文名称，用于按钮文字、提示与读屏。
const themeLabels: Record<ThemePreference, string> = { system: '跟随系统', light: '浅色模式', dark: '深色模式' };
// themeIcons 是各偏好对应的 lucide 图标。
const themeIcons = { system: Monitor, light: Sun, dark: Moon } as const;

// ThemeToggleProps 描述主题切换按钮的状态与展示形态。
interface ThemeToggleProps {
  /** preference 是当前主题偏好。 */ preference: ThemePreference;
  /** onCycle 在点击时循环切换到下一个偏好。 */ onCycle: () => void;
  /** compact 为 true 时只显示图标（侧边栏折叠态）。 */ compact?: boolean;
  /** className 允许调用方覆盖布局相关样式。 */ className?: string;
}

// ThemeToggle 渲染一个循环切换主题的按钮，同时用于桌面侧边栏和移动端“更多”抽屉。
const ThemeToggle: React.FC<ThemeToggleProps> = ({ preference, onCycle, compact = false, className = '' }) => {
  // Icon 是当前偏好对应的图标组件。
  const Icon = themeIcons[preference];
  // label 是当前偏好的中文名称。
  const label = themeLabels[preference];
  return (
    <button
      type="button"
      onClick={onCycle}
      title={`主题：${label}（点击切换）`}
      aria-label={`主题：${label}，点击切换`}
      className={className}
    >
      <Icon className="h-5 w-5" aria-hidden="true" />
      {!compact && <span className="text-sm font-bold">{label}</span>}
    </button>
  );
};

export default ThemeToggle;
