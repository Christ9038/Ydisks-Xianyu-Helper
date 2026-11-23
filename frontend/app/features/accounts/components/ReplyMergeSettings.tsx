import { MessageSquareMore } from 'lucide-react';
import React from 'react';
import { REPLY_MERGE_MAX_SECONDS,REPLY_MERGE_MIN_SECONDS } from '../replyMerge';

// ReplyMergeSettingsProps 是“合并等待”设置区块的输入；状态由父级编辑表单持有，本组件只负责展示与回传。
export interface ReplyMergeSettingsProps {
  // seconds 是合并等待秒数草稿。
  seconds: number;
  // error 是表单校验错误文案；空字符串表示无错误。
  error: string;
  // onSecondsChange 回传用户输入的秒数。
  onSecondsChange: (seconds: number) => void;
}

// ReplyMergeSettings 展示“买家分段消息合并等待”秒数输入。
export const ReplyMergeSettings: React.FC<ReplyMergeSettingsProps> = ({ seconds, error, onSecondsChange }) => {
  // handleSecondsChange 把输入框内容转为数字回传；空输入按 NaN 交给表单校验提示，避免悄悄改成别的值。
  const handleSecondsChange = (event: React.ChangeEvent<HTMLInputElement>) => onSecondsChange(event.target.value === '' ? Number.NaN : Number(event.target.value));

  return (
    <div className="p-4 bg-gray-50 rounded-xl space-y-3">
      <div>
        <div className="font-bold text-gray-900 flex items-center gap-2"><MessageSquareMore className="w-4 h-4 text-brand" />买家消息合并等待</div>
        <div className="text-xs text-gray-500">买家常把一句话拆成几条发送。最后一条消息之后安静这么久，才开始生成 AI / Agent 回复；期间收到的消息会合并成一次请求，生成中又收到新消息会中断并合并重来</div>
      </div>
      <div className="flex items-center gap-2">
        <input
          type="number"
          min={REPLY_MERGE_MIN_SECONDS}
          max={REPLY_MERGE_MAX_SECONDS}
          step={1}
          value={Number.isNaN(seconds) ? '' : seconds}
          onChange={handleSecondsChange}
          aria-label="买家消息合并等待秒数"
          aria-invalid={error !== ''}
          className="w-28 ios-input px-4 py-2 rounded-xl"
        />
        <span className="text-sm text-gray-600">秒（{REPLY_MERGE_MIN_SECONDS}–{REPLY_MERGE_MAX_SECONDS}，默认 3）</span>
      </div>
      {error && <p className="text-xs text-red-500">{error}</p>}
    </div>
  );
};
