// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import TemplateVariantEditor from './components/TemplateVariantEditor';
import { getCards } from './api';
import { useRulesData } from './hooks';
import type { Card, DeliveryTemplate, ShippingVariant } from './types';
import { canUseCreatedCard, variantPatchForCreatedCard } from './utils';

vi.mock('./api', /* 提供规则数据 Hook 的 API 替身。 */ () => ({
  getAccountDetails: vi.fn(), getAutomationIssues: vi.fn(), getCards: vi.fn(), getDefaultReplies: vi.fn(),
  getDeliveryTemplates: vi.fn(), getItems: vi.fn(), getReplyRules: vi.fn(), getShippingRulesPage: vi.fn(),
}));

// apiConfig 是 API 卡密摘要的公共字段。
const apiConfig = { url: 'https://x.test', method: 'GET' as const, timeout_seconds: 10, retry_enabled: false, headers_configured: false, params_configured: false, content_type: 'application/json' };
// readyApi 是已就绪的 API 卡密。
const readyApi: Card = { id: 1, name: '就绪接口', type: 'api', enabled: true, api_config: { ...apiConfig, ready: true } };
// notReadyApi 是配置未就绪的 API 卡密。
const notReadyApi: Card = { id: 2, name: '未就绪接口', type: 'api', enabled: true, api_config: { ...apiConfig, ready: false } };

describe('规则页新建卡密回填判断', /* 纯函数：可用性与补丁。 */ () => {
  test('canUseCreatedCard 按启用、就绪与图片限制判断', /* 各分支。 */ () => {
    expect(canUseCreatedCard(undefined)).toBe(false);
    expect(canUseCreatedCard(readyApi)).toBe(true);
    expect(canUseCreatedCard(notReadyApi)).toBe(false);
    expect(canUseCreatedCard({ id: 3, name: '停用', type: 'text', enabled: false })).toBe(false);
    // image 是图片卡密：可直接发送，但不能绑定模板变量。
    const image: Card = { id: 4, name: '图', type: 'image', enabled: true };
    expect(canUseCreatedCard(image)).toBe(true);
    expect(canUseCreatedCard(image, 'main')).toBe(false);
  });

  test('variantPatchForCreatedCard 回填库存或指定变量绑定', /* 两种补丁。 */ () => {
    // variant 是含两个绑定的模板规格。
    const variant = { card_id: 0, template_bindings: [{ variable_key: 'a', card_id: 0, delivery_count: 1 }, { variable_key: 'b', card_id: 5, delivery_count: 2 }] } as ShippingVariant;
    expect(variantPatchForCreatedCard(variant, 8)).toEqual({ card_id: 8 });
    expect(variantPatchForCreatedCard(variant, 8, 'a')).toEqual({ template_bindings: [{ variable_key: 'a', card_id: 8, delivery_count: 1 }, { variable_key: 'b', card_id: 5, delivery_count: 2 }] });
    expect(variantPatchForCreatedCard({ card_id: 0 } as ShippingVariant, 8, 'a')).toEqual({ template_bindings: [] });
  });
});

describe('TemplateVariantEditor 新建卡密入口', /* 按注入显示并回传变量键。 */ () => {
  afterEach(/* 清理渲染树。 */ () => cleanup());
  // template 是只含一个卡密变量的发货模板。
  const template: DeliveryTemplate = { id: 9, name: '模板', enabled: true, messages: [], keys: ['main'], custom_keys: [], created_at: '', updated_at: '' };
  // variant 是选择了该模板的规格。
  const variant = { spec_name: '', spec_value: '', card_id: 0, delivery_count: 1, enabled: true, delivery_mode: 'template', delivery_template_id: 9, template_bindings: [{ variable_key: 'main', card_id: 0, delivery_count: 1 }], custom_variables: {} } as ShippingVariant;

  test('注入回调后点击回传变量键，未注入则不显示', /* 两种情形。 */ () => {
    // onCreateCard 是新建入口回调替身。
    const onCreateCard = vi.fn();
    // view 用于切换为未注入回调的渲染。
    const view = render(<TemplateVariantEditor index={0} variant={variant} cards={[]} deliveryTemplates={[template]} updateVariant={vi.fn()} onCreateCard={onCreateCard} />);
    fireEvent.click(screen.getByRole('button', { name: '为 main 新建卡密库存' }));
    expect(onCreateCard).toHaveBeenCalledWith('main');
    view.rerender(<TemplateVariantEditor index={0} variant={variant} cards={[]} deliveryTemplates={[template]} updateVariant={vi.fn()} />);
    expect(screen.queryByRole('button', { name: '为 main 新建卡密库存' })).toBeNull();
  });
});

describe('useRulesData.reloadCards', /* 只刷新卡密并丢弃过期结果。 */ () => {
  beforeEach(/* 重置替身。 */ () => vi.clearAllMocks());

  test('返回最新列表并写入状态；较早的刷新返回 null', /* 代次保护。 */ async () => {
    // hook 是被测规则数据 Hook。
    const hook = renderHook(/* 创建 Hook。 */ () => useRulesData({ activeTab: 'reply', selectedAccountId: '', automationTriggerFilter: '', automationStatusFilter: 'all', debouncedAutomationSearch: '', automationPage: 1, automationPageSize: 10, setSelectedAccountId: vi.fn(), onAutomationPageChange: vi.fn() }));
    // releaseOld 用于最后才结束较早的刷新。
    let releaseOld: (cards: Card[]) => void = () => undefined;
    vi.mocked(getCards).mockReturnValueOnce(new Promise(/* resolver 捕获 resolve。 */ resolve => { releaseOld = resolve; }));
    vi.mocked(getCards).mockResolvedValueOnce([readyApi]);
    // older 是较早刷新的结果。
    let older: Card[] | null | undefined;
    // newer 是较新刷新的结果。
    let newer: Card[] | null | undefined;
    await act(/* 先后发起两次刷新。 */ async () => {
      // first 是较早的刷新。
      const first = hook.result.current.reloadCards();
      newer = await hook.result.current.reloadCards();
      releaseOld([notReadyApi]);
      older = await first;
    });
    expect(newer).toEqual([readyApi]);
    expect(older).toBeNull();
    expect(hook.result.current.cards).toEqual([readyApi]);
  });
});
