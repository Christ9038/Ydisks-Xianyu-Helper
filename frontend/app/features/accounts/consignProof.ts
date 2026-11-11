import type { AccountDetail,ConsignProofMode } from './models';
import type { AccountEditForm } from './types';

// CONSIGN_PROOF_TEXT_MAX 是固定文字的最大字符数，与后端 ConsignProofTextMaxRunes 一致（按 Unicode 字符计）。
export const CONSIGN_PROOF_TEXT_MAX = 200;

// ConsignProofModeOption 是编辑弹窗中一个凭证模式选项的展示信息。
export interface ConsignProofModeOption {
  // value 是提交给后端的模式取值。
  value: ConsignProofMode;
  // label 是选项按钮上的简短名称。
  label: string;
  // hint 说明选中该模式后确认发货时实际提交的内容。
  hint: string;
}

// CONSIGN_PROOF_MODES 是编辑弹窗展示的凭证模式选项，顺序即界面顺序。
export const CONSIGN_PROOF_MODES: ReadonlyArray<ConsignProofModeOption> = [
  { value: 'delivery', label: '卡密/模板全文', hint: '确认发货时提交买家实际收到的卡密或发货模板全文及图片；内容含外链、联系方式等时可能被闲鱼判定为敏感词而确认失败' },
  { value: 'fixed', label: '固定文字', hint: '确认发货时只提交下方填写的固定文字，不提交卡密、模板全文或图片' },
  { value: 'none', label: '不携带', hint: '确认发货时不提交任何发货凭证' },
];

// normalizeConsignProofMode 把接口返回值归一为已知模式；缺失或未知值（旧后端、手工改库）按历史行为视为 delivery。
export const normalizeConsignProofMode = (value: unknown): ConsignProofMode => (value === 'fixed' || value === 'none' ? value : 'delivery');

// ConsignProofEditFields 是编辑表单中参与凭证设置的字段。
export type ConsignProofEditFields = Pick<AccountEditForm, 'consign_proof_mode' | 'consign_proof_text'>;

// consignProofFormError 返回凭证设置的表单错误文案；空字符串表示可以保存。规则与后端校验一致，用于在提交前拦截。
export const consignProofFormError = (form: ConsignProofEditFields): string => {
  if ([...form.consign_proof_text].length > CONSIGN_PROOF_TEXT_MAX) return `固定文字不能超过 ${CONSIGN_PROOF_TEXT_MAX} 个字符`;
  if (form.consign_proof_mode === 'fixed' && form.consign_proof_text.trim() === '') return '选择固定文字时，请填写固定文字';
  return '';
};

// ConsignProofPayload 是提交给账号设置接口的凭证补丁。
export interface ConsignProofPayload {
  // consign_proof_mode 是确认发货凭证模式。
  consign_proof_mode: ConsignProofMode;
  // consign_proof_text 是固定文字模式的凭证文本。
  consign_proof_text: string;
}

// buildConsignProofUpdate 只在模式或文字变化时生成补丁；两者总是一起提交，让后端按同一快照校验最终状态。
export const buildConsignProofUpdate = (
  account: Pick<AccountDetail, 'consign_proof_mode' | 'consign_proof_text'>,
  form: ConsignProofEditFields,
): ConsignProofPayload | null => {
  // modeChanged 表示用户是否切换了凭证模式。
  const modeChanged = form.consign_proof_mode !== normalizeConsignProofMode(account.consign_proof_mode);
  // textChanged 表示用户是否修改了固定文字。
  const textChanged = form.consign_proof_text !== (account.consign_proof_text || '');
  if (!modeChanged && !textChanged) return null;
  return { consign_proof_mode: form.consign_proof_mode, consign_proof_text: form.consign_proof_text };
};
