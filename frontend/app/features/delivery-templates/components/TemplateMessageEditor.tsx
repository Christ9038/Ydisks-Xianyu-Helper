import { ArrowDown, ArrowUp, Trash2 } from 'lucide-react';
import { ImageSourceEditor } from '../../../../shared/components/ImageSourceEditor';
import { emptyTemplateMessage } from '../templateState';
import type { DeliveryTemplateMessageDraft } from '../types';

/** TemplateMessageEditorProps 限定单条消息的编辑和排序边界，不直接访问请求层。 */
interface TemplateMessageEditorProps {
  /** message 是当前消息草稿。 */
  message: DeliveryTemplateMessageDraft;
  /** index 是当前显示与发送顺序。 */
  index: number;
  /** count 用于禁用越界排序和最后一条删除。 */
  count: number;
  /** onChange 替换当前消息，不修改其他草稿。 */
  onChange: (message: DeliveryTemplateMessageDraft) => void;
  /** onMove 按一个位置向上或向下移动。 */
  onMove: (direction: -1 | 1) => void;
  /** onRemove 删除当前条目。 */
  onRemove: () => void;
}

/** TemplateMessageEditor 每条消息独立选择文本或图片，键盘可操作排序。 */
export function TemplateMessageEditor({ message, index, count, onChange, onMove, onRemove }: TemplateMessageEditorProps) {
  // number 是对用户展示的从一开始的顺序。
  const number = index + 1;
  return <fieldset className="space-y-3 rounded-2xl border border-gray-200 bg-gray-50/70 p-3">
    <legend className="px-1 text-xs font-bold text-gray-600">第 {number} 条消息</legend>
    <div className="flex flex-wrap items-center justify-between gap-2">
      <label className="text-sm font-bold text-gray-700">
        消息类型
        <select aria-label={`第 ${number} 条消息类型`} value={message.type || 'text'} onChange={/* event 切换消息类型并清除原内容，不把图片混入文本变量。 */ event => onChange(emptyTemplateMessage(event.target.value as 'text' | 'image'))} className="ios-input ml-2 rounded-xl px-3 py-2">
          <option value="text">文本</option><option value="image">图片</option>
        </select>
      </label>
      <div className="flex gap-1">
        <button type="button" disabled={index === 0} onClick={/* 当前回调上移一条消息。 */ () => onMove(-1)} aria-label={`上移第 ${number} 条消息`} className="rounded-lg p-2 text-gray-600 hover:bg-white disabled:opacity-30"><ArrowUp className="h-4 w-4" /></button>
        <button type="button" disabled={index === count - 1} onClick={/* 当前回调下移一条消息。 */ () => onMove(1)} aria-label={`下移第 ${number} 条消息`} className="rounded-lg p-2 text-gray-600 hover:bg-white disabled:opacity-30"><ArrowDown className="h-4 w-4" /></button>
        <button type="button" disabled={count === 1} onClick={onRemove} aria-label={`删除第 ${number} 条消息`} className="rounded-lg p-2 text-red-500 hover:bg-red-100 disabled:opacity-30"><Trash2 className="h-4 w-4" /></button>
      </div>
    </div>
    {message.type === 'image'
      ? <ImageSourceEditor value={message} onChange={/* fields 更新来源时保持图片正文为空。 */ fields => onChange({ type: 'image', content: '', ...fields })} />
      : <textarea aria-label={`第 ${number} 条消息正文`} value={message.content} onChange={/* event 仅修改当前文本正文。 */ event => onChange({ ...message, content: event.target.value })} placeholder="例如：感谢购买，您的卡密是 {{cards.main}}" className="ios-input min-h-24 w-full resize-y rounded-xl bg-white px-4 py-3" />}
  </fieldset>;
}
