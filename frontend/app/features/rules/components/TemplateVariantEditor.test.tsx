// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import TemplateVariantEditor from './TemplateVariantEditor';
import type { Card, DeliveryTemplate, ShippingVariant } from '../types';

// templateFixture 是包含卡密和自定义变量的最小模板摘要。
const templateFixture: DeliveryTemplate = {
  id: 9,
  name: '订单通知',
  enabled: true,
  messages: [{ id: 1, sort_order: 1, content: '{{custom.remark}} {{cards.main}}' }],
  keys: ['main'],
  custom_keys: ['remark'],
  created_at: '',
  updated_at: '',
};

// variantFixture 是模板变量编辑器使用的空白规则变体。
const variantFixture: ShippingVariant = {
  spec_name: '',
  spec_value: '',
  card_id: 0,
  delivery_count: 1,
  enabled: true,
  delivery_mode: 'template',
  delivery_template_id: 9,
  template_bindings: [{ variable_key: 'main', card_id: 0, delivery_count: 1 }],
  custom_variables: {},
};

describe('TemplateVariantEditor 自定义变量配置', /* 当前回调验证模板自定义变量 key/value 编辑行为。 */ () => {
  afterEach(/* 当前回调清理模板变量编辑器测试 DOM。 */ () => cleanup());

  test('按模板 key 显示字符串 value 输入，而不是数组下标输入', /* 当前回调验证自定义变量按模板 key 展示。 */ () => {
    // updateVariant 是验证编辑回调是否接收到键值表更新的替身。
    const updateVariant = vi.fn();
    // cards 是当前规则可选的卡密库存摘要。
    const cards: Card[] = [{ id: 1, name: '主卡', type: 'text', enabled: true }];
    render(<TemplateVariantEditor index={0} variant={variantFixture} cards={cards} deliveryTemplates={[templateFixture]} updateVariant={updateVariant} />);

    expect(screen.getByText('{{custom.remark}}')).toBeTruthy();
    expect(screen.getByPlaceholderText('填写 remark 对应的字符串')).toBeTruthy();
    expect(screen.getByText('{{cards.main}}')).toBeTruthy();
  });
});
