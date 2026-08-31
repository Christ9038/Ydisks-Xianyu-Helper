// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
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

afterEach(/* 当前回调清理后续模板变量编辑器测试 DOM。 */ () => cleanup());

test('模板变量绑定展示启用的 text、data 和就绪 API 卡券', /* 当前回调验证模板执行器支持范围与选择器保持一致。 */ () => {
  // cards 是包含所有卡券类型和启用状态的筛选样例。
  const cards: Card[] = [
    { id: 1, name: '文本卡', type: 'text', enabled: true },
    { id: 2, name: '批量卡', type: 'data', enabled: true },
    { id: 3, name: '就绪 API 卡', type: 'api', enabled: true, api_config: { url: 'https://example.test', method: 'GET', timeout_seconds: 10, retry_enabled: true, headers_configured: true, params_configured: true, ready: true } },
    { id: 4, name: '图片卡', type: 'image', enabled: true },
    { id: 5, name: '停用文本卡', type: 'text', enabled: false },
    { id: 6, name: '停用批量卡', type: 'data', enabled: false },
    { id: 7, name: '未就绪 API 卡', type: 'api', enabled: true, api_config: { url: 'https://example.test', method: 'GET', timeout_seconds: 10, retry_enabled: true, headers_configured: false, params_configured: false, ready: false } },
  ];
  // updateVariant 是验证选择器变更回传的编辑回调替身。
  const updateVariant = vi.fn();
  render(<TemplateVariantEditor index={0} variant={variantFixture} cards={cards} deliveryTemplates={[templateFixture]} updateVariant={updateVariant} />);

  expect(screen.getByRole('option', { name: '文本卡' })).toBeTruthy();
  expect(screen.getByRole('option', { name: '批量卡' })).toBeTruthy();
  expect(screen.getByRole('option', { name: '就绪 API 卡' })).toBeTruthy();
  expect(screen.queryByRole('option', { name: '图片卡' })).toBeNull();
  expect(screen.queryByRole('option', { name: '停用文本卡' })).toBeNull();
  expect(screen.queryByRole('option', { name: '停用批量卡' })).toBeNull();
  expect(screen.queryByRole('option', { name: '未就绪 API 卡' })).toBeNull();
});

test('已有 text/data 绑定仍可显示并修改数量', /* 当前回调验证合法历史绑定在过滤后仍保留可编辑能力。 */ () => {
  // cards 是已有绑定和另一个可选库存的最小列表。
  const cards: Card[] = [
    { id: 1, name: '已绑定文本卡', type: 'text', enabled: true },
    { id: 2, name: '批量卡', type: 'data', enabled: true },
  ];
  // updateVariant 是验证数量编辑结果的回调替身。
  const updateVariant = vi.fn();
  render(<TemplateVariantEditor index={0} variant={{ ...variantFixture, template_bindings: [{ variable_key: 'main', card_id: 1, delivery_count: 1 }] }} cards={cards} deliveryTemplates={[templateFixture]} updateVariant={updateVariant} />);

  expect(screen.getByRole('option', { name: '已绑定文本卡' })).toBeTruthy();
  // countInputs 保存模板绑定数量输入框；同一编辑器还会渲染自定义变量输入，故按 aria-label 取第一个绑定控件。
  const countInputs = screen.getAllByRole('spinbutton', { name: 'main 每件份数' });
  // countInput 保存当前用例要修改的第一个模板绑定数量输入框。
  const countInput = countInputs[0];
  fireEvent.change(countInput, { target: { value: '2' } });
  expect(updateVariant).toHaveBeenCalledWith(0, { template_bindings: [{ variable_key: 'main', card_id: 1, delivery_count: 2 }] });
});
