import { Hand } from 'lucide-react';
import React from 'react';
import { MANUAL_TAKEOVER_MAX_MINUTES,MANUAL_TAKEOVER_SCOPES } from '../manualTakeover';
import type { ManualTakeoverScope } from '../models';

// ManualTakeoverSettingsProps 是人工介入暂停设置区块的输入；状态由父级编辑表单持有，本组件只负责展示与回传。
export interface ManualTakeoverSettingsProps {
  // minutes 是暂停分钟数草稿，0 表示关闭。
  minutes: number;
  // scope 是暂停范围草稿。
  scope: ManualTakeoverScope;
  // error 是表单校验错误文案；空字符串表示无错误。
  error: string;
  // onMinutesChange 回传用户输入的分钟数。
  onMinutesChange: (minutes: number) => void;
  // onScopeChange 回传用户选择的暂停范围。
  onScopeChange: (scope: ManualTakeoverScope) => void;
}

// ManualTakeoverSettings 展示“人工介入暂停”分钟数输入与暂停范围二选一。
export const ManualTakeoverSettings: React.FC<ManualTakeoverSettingsProps> = ({ minutes, scope, error, onMinutesChange, onScopeChange }) => {
  // activeHint 是当前所选范围的说明文字。
  const activeHint = MANUAL_TAKEOVER_SCOPES.find(/* option 是候选范围，按值匹配当前范围。 */ option => option.value === scope)?.hint || '';
  // handleMinutesChange 把输入框内容转为数字回传；空输入视为 0（关闭）。
  const handleMinutesChange = (event: React.ChangeEvent<HTMLInputElement>) => onMinutesChange(event.target.value === '' ? 0 : Number(event.target.value));

  return (
    <div className="p-4 bg-gray-50 rounded-xl space-y-3">
      <div>
        <div className="font-bold text-gray-900 flex items-center gap-2"><Hand className="w-4 h-4 text-orange-500" />人工介入暂停</div>
        <div className="text-xs text-gray-500">你在闲鱼 App、网页版或本系统聊天页回复买家后，该会话暂停自动回复的分钟数；0 表示关闭。服务重启后暂停状态清空</div>
      </div>
      <div className="flex items-center gap-2">
        <input
          type="number"
          min={0}
          max={MANUAL_TAKEOVER_MAX_MINUTES}
          step={1}
          value={minutes}
          onChange={handleMinutesChange}
          aria-label="人工介入暂停分钟数"
          aria-invalid={error !== ''}
          className="w-28 ios-input px-4 py-2 rounded-xl"
        />
        <span className="text-sm text-gray-600">分钟</span>
      </div>
      {error && <p className="text-xs text-red-500">{error}</p>}
      <div role="radiogroup" aria-label="人工介入暂停范围" className="grid grid-cols-2 gap-2">
        {MANUAL_TAKEOVER_SCOPES.map(/* option 是一个可选的暂停范围。 */ option => (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={scope === option.value}
            onClick={/* 选择该范围。 */ () => onScopeChange(option.value)}
            className={`px-2 py-2 rounded-lg text-sm font-bold border transition-colors ${scope === option.value ? 'bg-brand text-white border-brand' : 'bg-white text-gray-700 border-gray-200 hover:border-brand'}`}
          >
            {option.label}
          </button>
        ))}
      </div>
      <p className="text-xs text-gray-500">{activeHint}</p>
    </div>
  );
};
