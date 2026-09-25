import { Plus,Send,Trash2,X } from 'lucide-react';
import React,{ type Dispatch,type SetStateAction } from 'react';
import { createPortal } from 'react-dom';
import ItemMultiSelect from './ItemMultiSelect';
import type { Item,ReplyRule } from '../types';

/** ReplyRuleEditorProps 描述关键词回复编辑器所需的草稿、商品候选和保存动作。 */
interface ReplyRuleEditorProps {
  /** rule 保存当前关键词回复表单状态；组件只修改表单字段，不直接触发网络请求。 */
  rule: Partial<ReplyRule>;
  /** setRule 以函数式状态更新方式写回关键词回复表单，避免输入事件覆盖并发状态。 */
  setRule: Dispatch<SetStateAction<Partial<ReplyRule> | null>>;
  /** items 保存本地已同步、可绑定到关键词规则的商品候选。 */
  items: Item[];
  /** onClose 关闭编辑器并保留由父级拥有的草稿生命周期。 */
  onClose: () => void;
  /** onSave 触发父级的校验、提交和刷新流程。 */
  onSave: () => void;
}

/** ReplyRuleEditor 展示关键词多表达式、匹配模式、商品范围和回复内容编辑器。 */
const ReplyRuleEditor: React.FC<ReplyRuleEditorProps> = ({ rule,setRule,items,onClose,onSave }) => {
  // expressions 是当前编辑器展示的表达式行，历史单 keyword 规则回退为一行。
  const expressions = rule.expressions?.length ? rule.expressions : [rule.keyword || ''];
  // itemIDs 是当前规则绑定的商品标识集合，兼容历史单值字段。
  const itemIDs = rule.item_ids?.length ? rule.item_ids : (rule.item_id ? [rule.item_id] : []);

  return createPortal(
    <div className="modal-overlay">
      <div className="modal-container">
        <div className="modal-header">
          <div className="flex items-center justify-between w-full">
            <h3 className="text-2xl font-extrabold text-gray-900">
              {rule.id ? '编辑回复规则' : '新增回复规则'}
            </h3>
            <button
              onClick={/* 当前回调关闭关键词回复编辑器。 */ onClose}
              className="p-2 bg-gray-100 rounded-full hover:bg-gray-200 transition-colors"
            >
              <X className="w-5 h-5 text-gray-600" />
            </button>
          </div>
        </div>

        <div className="modal-body space-y-5">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div>
              <label className="block text-sm font-bold text-gray-700 mb-2">关联商品</label>
              <ItemMultiSelect
                options={items}
                value={itemIDs}
                onChange={/* selectedIDs 是用户选择后的商品集合；current 是待合并的关键词草稿。 */ selectedIDs => setRule(/* current 表示当前关键词回复草稿。 */ current => current ? { ...current, item_ids: selectedIDs, item_id: selectedIDs[0] || '' } : current)}
              />
              <p className="mt-2 text-xs text-gray-400">可多选；不选表示账号级回复。候选来自本地已同步商品，支持搜索。</p>
            </div>
            <div>
              <label className="block text-sm font-bold text-gray-700 mb-2">匹配模式</label>
              <select
                value={rule.match_type === 'regexp' ? 'regexp' : 'contains'}
                onChange={/* 当前回调处理用户切换关键词匹配模式。 */ event => {
                  // matchType 保存用户选择的当前匹配模式。
                  const matchType = event.target.value as 'contains' | 'regexp';
                  setRule(/* current 表示当前关键词回复草稿；函数式更新保持表单字段不被晚到输入覆盖。 */ current => current ? { ...current, match_type: matchType } : current);
                }}
                className="w-full ios-input px-4 py-3 rounded-xl"
              >
                <option value="contains">包含匹配</option>
                <option value="regexp">正则表达式</option>
              </select>
            </div>
            <div>
              <label className="block text-sm font-bold text-gray-700 mb-2">回复类型</label>
              <select
                value={rule.type || 'text'}
                onChange={/* 当前回调处理用户切换回复类型并清理不适用内容。 */ event => {
                  // type 保存用户选择的文字或图片回复类型。
                  const type = event.target.value as 'text' | 'image';
                  setRule(/* current 表示当前关键词回复草稿；函数式更新同步回复类型切换后的字段。 */ current => current ? {
                    ...current,
                    type,
                    reply_content: type === 'text' ? current.reply_content : '',
                    image_url: type === 'image' ? current.image_url : '',
                  } : current);
                }}
                className="w-full ios-input px-4 py-3 rounded-xl"
              >
                <option value="text">文字</option>
                <option value="image">图片</option>
              </select>
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between gap-3 mb-2">
              <label className="block text-sm font-bold text-gray-700">关键词表达式</label>
              <button
                type="button"
                onClick={/* 当前回调追加一行可选的关键词表达式；current 是当前关键词回复草稿。 */ () => setRule(/* current 表示当前关键词回复草稿。 */ current => current ? { ...current, expressions: [...expressions, ''] } : current)}
                className="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-gray-100 text-gray-700 text-xs font-bold hover:bg-gray-200"
              >
                <Plus className="w-3.5 h-3.5" />
                添加表达式
              </button>
            </div>
            <div className="space-y-3">
              {expressions.map(/* expression 是当前编辑行的表达式；expressionIndex 是其行号。 */ (expression, expressionIndex) => (
                <div key={expressionIndex} className="flex items-center gap-2">
                  <input
                    type="text"
                    value={expression}
                    onChange={/* 当前回调更新用户正在编辑的表达式行。 */ event => {
                      // nextExpressions 保存更新当前行后的完整表达式集合。
                      const nextExpressions = expressions.map(/* currentExpression 是当前行文本；currentIndex 是当前行号。 */ (currentExpression, currentIndex) => currentIndex === expressionIndex ? event.target.value : currentExpression);
                      setRule(/* current 表示当前关键词回复草稿；函数式更新同步表达式和首项兼容字段。 */ current => current ? { ...current, expressions: nextExpressions, keyword: nextExpressions[0] || '' } : current);
                    }}
                    placeholder="关键词或正则表达式"
                    className="flex-1 ios-input px-4 py-3 rounded-xl"
                  />
                  <button
                    type="button"
                    disabled={expressions.length <= 1}
                    onClick={/* 当前回调删除一行表达式并至少保留一个空输入行。 */ () => {
                      // nextExpressions 保存删除当前行后的表达式集合。
                      const nextExpressions = expressions.filter(/* currentIndex 是待保留表达式的行号。 */ (_, currentIndex) => currentIndex !== expressionIndex);
                      // safeExpressions 保证删除最后一行时仍保留一个可编辑表达式输入。
                      const safeExpressions = nextExpressions.length ? nextExpressions : [''];
                      setRule(/* current 表示当前关键词回复草稿；函数式更新同步删除后的表达式集合。 */ current => current ? { ...current, expressions: safeExpressions, keyword: safeExpressions[0] || '' } : current);
                    }}
                    className="p-2 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-lg disabled:opacity-30 disabled:cursor-not-allowed"
                    aria-label={`删除第 ${expressionIndex + 1} 个表达式`}
                    title="删除表达式"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              ))}
            </div>
            <p className="mt-2 text-xs text-gray-400">多个表达式按“或”（OR）匹配：任一表达式命中即触发回复；正则语法会先在浏览器校验，后端仍会按 Go/RE2 最终校验。</p>
          </div>

          {rule.type === 'image' ? (
            <div>
              <label className="block text-sm font-bold text-gray-700 mb-2">图片 URL</label>
              <input
                value={rule.image_url || ''}
                onChange={/* 当前回调处理用户输入的图片地址；current 是当前关键词回复草稿。 */ event => setRule(/* current 表示当前关键词回复草稿。 */ current => current ? { ...current, image_url: event.target.value } : current)}
                placeholder="https://..."
                className="w-full ios-input px-4 py-3 rounded-xl"
              />
            </div>
          ) : (
            <div>
              <label className="block text-sm font-bold text-gray-700 mb-2">回复内容</label>
              <textarea
                value={rule.reply_content || ''}
                onChange={/* 当前回调处理用户输入的文字回复；current 是当前关键词回复草稿。 */ event => setRule(/* current 表示当前关键词回复草稿。 */ current => current ? { ...current, reply_content: event.target.value } : current)}
                placeholder="自动回复的内容"
                className="w-full ios-input px-4 py-3 rounded-xl h-32 resize-none"
              />
            </div>
          )}

          <div className="flex gap-3 pt-4">
            <button
              onClick={/* 当前回调取消关键词回复编辑并关闭弹窗。 */ onClose}
              className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors"
            >
              取消
            </button>
            <button
              onClick={/* 当前回调提交关键词回复规则。 */ onSave}
              className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2"
            >
              <Send className="w-4 h-4" />
              保存规则
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
};

export default ReplyRuleEditor;
