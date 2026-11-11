import { FileText } from 'lucide-react';
import React from 'react';
import { CONSIGN_PROOF_MODES,CONSIGN_PROOF_TEXT_MAX } from '../consignProof';
import type { ConsignProofMode } from '../models';

// ConsignProofSettingsProps 是确认发货凭证设置区块的输入；状态由父级编辑表单持有，本组件只负责展示与回传。
export interface ConsignProofSettingsProps {
  // mode 是当前选择的凭证模式。
  mode: ConsignProofMode;
  // text 是固定文字草稿。
  text: string;
  // error 是表单校验错误文案；空字符串表示无错误。
  error: string;
  // onModeChange 回传用户选择的新模式。
  onModeChange: (mode: ConsignProofMode) => void;
  // onTextChange 回传用户输入的固定文字。
  onTextChange: (text: string) => void;
}

// ConsignProofSettings 展示“确认发货凭证”三选一与固定文字输入，位于自动确认发货开关下方。
export const ConsignProofSettings: React.FC<ConsignProofSettingsProps> = ({ mode, text, error, onModeChange, onTextChange }) => {
  // activeHint 是当前所选模式的说明文字。
  const activeHint = CONSIGN_PROOF_MODES.find(/* option 是候选模式，按值匹配当前模式。 */ option => option.value === mode)?.hint || '';
  // handleTextChange 把输入框内容回传给父级表单。
  const handleTextChange = (event: React.ChangeEvent<HTMLInputElement>) => onTextChange(event.target.value);

  return (
    <div className="p-4 bg-gray-50 rounded-xl space-y-3">
      <div>
        <div className="font-bold text-gray-900 flex items-center gap-2"><FileText className="w-4 h-4 text-green-500" />确认发货凭证</div>
        <div className="text-xs text-gray-500">调用闲鱼确认发货接口时附带的发货凭证；自动确认发货和手动完整发货都会使用此设置</div>
      </div>
      <div role="radiogroup" aria-label="确认发货凭证" className="grid grid-cols-3 gap-2">
        {CONSIGN_PROOF_MODES.map(/* option 是一个可选的凭证模式。 */ option => (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={mode === option.value}
            onClick={/* 选择该模式。 */ () => onModeChange(option.value)}
            className={`px-2 py-2 rounded-lg text-sm font-bold border transition-colors ${mode === option.value ? 'bg-brand text-white border-brand' : 'bg-white text-gray-700 border-gray-200 hover:border-brand'}`}
          >
            {option.label}
          </button>
        ))}
      </div>
      <p className="text-xs text-gray-500">{activeHint}</p>
      {mode === 'fixed' && (
        <div>
          <input
            type="text"
            value={text}
            onChange={handleTextChange}
            maxLength={CONSIGN_PROOF_TEXT_MAX}
            placeholder="例如：已通过闲鱼聊天发送，请查收"
            aria-label="确认发货固定文字"
            aria-invalid={error !== ''}
            className="w-full ios-input px-4 py-3 rounded-xl"
          />
          <div className="flex justify-between mt-1 text-xs">
            <span className="text-red-500">{error}</span>
            <span className="text-gray-400">{[...text].length}/{CONSIGN_PROOF_TEXT_MAX}</span>
          </div>
        </div>
      )}
    </div>
  );
};
