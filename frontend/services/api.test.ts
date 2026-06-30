import { afterEach, expect, test, vi } from 'vitest';
import { getItems, getOrders, getShippingRules, getValidOrders, updateShippingRule } from './api';

afterEach(() => vi.unstubAllGlobals());

const jsonResponse = (body: unknown) => new Response(JSON.stringify(body), {
  status: 200,
  headers: { 'content-type': 'application/json' },
});

test('getOrders normalizes backend order fields', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
    orders: [{ order_id: 'o1', order_status: 'shipped', quantity: '2' }],
    total: 1,
  })));
  const result = await getOrders(undefined, 'all', 1, 20);
  expect(result.data[0]).toMatchObject({ id: 'o1', status: 'shipped', quantity: 2 });
  expect(result.total).toBe(1);
});

test('getValidOrders accepts wrapped responses', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
    orders: [{ order_id: 'o2', order_status: 'completed', quantity: '3' }],
  })));
  const result = await getValidOrders({ start_date: '2026-01-01', end_date: '2026-01-02' });
  expect(result).toEqual([expect.objectContaining({ id: 'o2', status: 'completed', quantity: 3 })]);
});

test('getItems normalizes multi-spec flags from backend values', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse([{
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    item_title: '普通商品',
    is_multi_spec: '0',
    multi_quantity_delivery: 0,
  }, {
    cookie_id: 'cookie-1',
    item_id: 'item-2',
    item_title: '多规格商品',
    is_multi_spec: '1',
    multi_quantity_delivery: 1,
  }])));

  const items = await getItems();
  expect(items[0]).toMatchObject({
    id: 'cookie-1-item-1',
    is_multi_spec: false,
    is_multi_qty_ship: false,
    multi_quantity_delivery: false,
  });
  expect(items[1]).toMatchObject({
    id: 'cookie-1-item-2',
    is_multi_spec: true,
    is_multi_qty_ship: true,
    multi_quantity_delivery: true,
  });
});

test('getShippingRules exposes buyer reviewed gift rules as automation rules', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse([{
    id: 12,
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    item_title: '测试商品',
    name: '评价后发送赠品 - 测试商品',
    trigger_type: 'buyer_reviewed',
    enabled: true,
    priority: 90,
    config_json: '{}',
    actions: [{
      id: 33,
      action_type: 'send_card',
      card_id: 7,
      card_name: '赠品库存',
      delivery_count: 1,
      config_json: '{"spec_name":"套餐","spec_value":"赠品"}',
      enabled: true,
      sort_order: 1,
    }],
  }])));

  const rules = await getShippingRules();
  expect(rules[0]).toMatchObject({
    id: '12',
    trigger_type: 'buyer_reviewed',
    card_group_id: 7,
    card_group_name: '赠品库存',
  });
  expect(rules[0].variants[0]).toMatchObject({
    spec_name: '套餐',
    spec_value: '赠品',
    card_id: 7,
  });
});

test('updateShippingRule posts buyer reviewed gift payload to automation-rules', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, id: 1 }));
  vi.stubGlobal('fetch', fetchMock);

  await updateShippingRule({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    trigger_type: 'buyer_reviewed',
    enabled: true,
    variants: [{
      spec_name: '',
      spec_value: '',
      card_id: 7,
      delivery_count: 1,
      enabled: true,
    }],
  });

  expect(fetchMock).toHaveBeenCalledWith('/automation-rules', expect.objectContaining({
    method: 'POST',
    credentials: 'include',
  }));
  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body).toMatchObject({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    name: '评价后发送赠品 - item-1',
    trigger_type: 'buyer_reviewed',
  });
  expect(body.actions).toEqual([
    expect.objectContaining({
      action_type: 'send_card',
      card_id: 7,
      sort_order: 1,
    }),
  ]);
});

test('updateShippingRule posts every matching card action before confirm shipment', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, id: 3 }));
  vi.stubGlobal('fetch', fetchMock);

  await updateShippingRule({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    trigger_type: 'order_paid',
    enabled: true,
    variants: [
      {
        spec_name: '套餐',
        spec_value: '30天',
        card_id: 8,
        delivery_count: 1,
        enabled: true,
      },
      {
        spec_name: '套餐',
        spec_value: '30天',
        card_id: 9,
        delivery_count: 2,
        enabled: true,
      },
    ],
  });

  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body.trigger_type).toBe('order_paid');
  expect(body.actions).toEqual([
    expect.objectContaining({
      action_type: 'send_card',
      card_id: 8,
      sort_order: 1,
    }),
    expect.objectContaining({
      action_type: 'send_card',
      card_id: 9,
      delivery_count: 2,
      sort_order: 2,
    }),
    expect.objectContaining({
      action_type: 'confirm_shipment',
      sort_order: 3,
    }),
  ]);
  expect(JSON.parse(body.actions[0].config_json)).toEqual({ spec_name: '套餐', spec_value: '30天' });
  expect(JSON.parse(body.actions[1].config_json)).toEqual({ spec_name: '套餐', spec_value: '30天' });
});

test('updateShippingRule posts review request text action without card requirement', async () => {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ success: true, id: 2 }));
  vi.stubGlobal('fetch', fetchMock);

  await updateShippingRule({
    cookie_id: 'cookie-1',
    item_id: 'item-1',
    trigger_type: 'review_missing_timeout',
    enabled: true,
    config_json: '{"after_shipped_hours":48,"max_attempts":2}',
    actions: [{
      action_type: 'send_text',
      message_template: '亲，方便的话麻烦给个评价～',
      enabled: true,
      sort_order: 1,
    }],
  });

  const body = JSON.parse(fetchMock.mock.calls[0][1].body);
  expect(body.trigger_type).toBe('review_missing_timeout');
  expect(body.actions).toEqual([
    expect.objectContaining({
      action_type: 'send_text',
      card_id: 0,
      message_template: '亲，方便的话麻烦给个评价～',
    }),
  ]);
});
