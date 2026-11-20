import React from 'react';

/** FeishuSwitchRowProps 描述飞书设置卡片中单个开关行的展示属性。 */
export interface FeishuSwitchRowProps {
  /** label 是开关行的中文标题。 */
  label: string;
  /** description 是开关控制的通知范围说明。 */
  description: string;
  /** checked 表示开关当前是否开启。 */
  checked: boolean;
  /** disabled 表示读取、保存或流程开始后是否禁止修改。 */
  disabled: boolean;
  /** onChange 在用户点击开关时回传目标状态。 */
  onChange: (value: boolean) => void;
}

/** FeishuSwitchRow 渲染飞书设置卡片中的单行开关，供扫码弹窗和渠道设置弹窗复用。 */
export const FeishuSwitchRow: React.FC<FeishuSwitchRowProps> = ({ label, description, checked, disabled, onChange }) => (
  <div className="flex items-start justify-between gap-4 rounded-xl border border-gray-100 p-3">
    <div className="min-w-0">
      <p className="text-sm font-bold text-gray-800">{label}</p>
      <p className="mt-1 text-xs leading-5 text-gray-500">{description}</p>
    </div>
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={/* toggleAction 把当前开关切换到相反状态。 */ () => onChange(!checked)}
      className={`relative h-7 w-12 flex-shrink-0 rounded-full transition-colors disabled:opacity-50 ${checked ? 'bg-brand' : 'bg-gray-300'}`}
    >
      <span className={`absolute left-1 top-1 h-5 w-5 rounded-full bg-white shadow-sm transition-transform ${checked ? 'translate-x-5' : ''}`} />
    </button>
  </div>
);
