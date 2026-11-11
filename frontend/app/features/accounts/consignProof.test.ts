import { describe,expect,test } from 'vitest';
import { buildConsignProofUpdate,CONSIGN_PROOF_MODES,CONSIGN_PROOF_TEXT_MAX,consignProofFormError,normalizeConsignProofMode } from './consignProof';

describe('consignProof', /* 当前回调验证确认发货凭证的归一、校验与补丁生成。 */ () => {
  test('缺失或未知模式按卡密/模板全文处理', /* 当前回调验证旧后端与异常值兼容。 */ () => {
    expect(normalizeConsignProofMode(undefined)).toBe('delivery');
    expect(normalizeConsignProofMode('garbage')).toBe('delivery');
    expect(normalizeConsignProofMode('fixed')).toBe('fixed');
    expect(normalizeConsignProofMode('none')).toBe('none');
    expect(CONSIGN_PROOF_MODES.map(/* option 是界面选项，只取值比较顺序。 */ option => option.value)).toEqual(['delivery', 'fixed', 'none']);
  });

  test('固定文字为空或超长时给出错误，其他模式允许空文字', /* 当前回调验证提交前校验与后端规则一致。 */ () => {
    expect(consignProofFormError({ consign_proof_mode: 'fixed', consign_proof_text: ' ' })).toContain('请填写固定文字');
    expect(consignProofFormError({ consign_proof_mode: 'fixed', consign_proof_text: '已发送' })).toBe('');
    expect(consignProofFormError({ consign_proof_mode: 'none', consign_proof_text: '' })).toBe('');
    expect(consignProofFormError({ consign_proof_mode: 'delivery', consign_proof_text: '' })).toBe('');
    // 按 Unicode 字符计数：200 个汉字合法，201 个超长。
    expect(consignProofFormError({ consign_proof_mode: 'fixed', consign_proof_text: '字'.repeat(CONSIGN_PROOF_TEXT_MAX) })).toBe('');
    expect(consignProofFormError({ consign_proof_mode: 'fixed', consign_proof_text: '字'.repeat(CONSIGN_PROOF_TEXT_MAX + 1) })).toContain('不能超过');
  });

  test('只在模式或文字变化时生成补丁，且两者一起提交', /* 当前回调验证最小补丁语义。 */ () => {
    expect(buildConsignProofUpdate({}, { consign_proof_mode: 'delivery', consign_proof_text: '' })).toBeNull();
    expect(buildConsignProofUpdate({ consign_proof_mode: 'fixed', consign_proof_text: 'A' }, { consign_proof_mode: 'fixed', consign_proof_text: 'A' })).toBeNull();
    expect(buildConsignProofUpdate({}, { consign_proof_mode: 'fixed', consign_proof_text: 'A' })).toEqual({ consign_proof_mode: 'fixed', consign_proof_text: 'A' });
    expect(buildConsignProofUpdate({ consign_proof_mode: 'fixed', consign_proof_text: 'A' }, { consign_proof_mode: 'fixed', consign_proof_text: 'B' })).toEqual({ consign_proof_mode: 'fixed', consign_proof_text: 'B' });
  });
});
