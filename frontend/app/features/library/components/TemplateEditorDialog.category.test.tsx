// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import type { AutomationOptions } from '../hooks';
import type { LibraryCategoryFieldProps } from '../types';
import { TemplateEditorDialog } from './TemplateEditorDialog';

// options 是含两个账号的自动化可选项，类目推荐账号默认取第一个。
const options: AutomationOptions = { cards: [], deliveryTemplates: [], accounts: [{ id: 'a1', label: '店铺1' }, { id: 'a2', label: '店铺2' }], error: '' };

// seen 记录注入的类目字段最近一次收到的属性。
let seen: LibraryCategoryFieldProps | null = null;

/** FakeCategoryField 模拟注入的类目字段：记录属性，点击按钮选中一个完整类目，点击清除回传空值。 */
const FakeCategoryField = (props: LibraryCategoryFieldProps) => {
  seen = props;
  return <><button type="button" onClick={/* 选中完整类目。 */ () => props.onChange({ cat_id: '9', cat_name: '游戏软件平台', channel_cat_id: '99' })}>选类目</button><button type="button" onClick={/* 清除类目。 */ () => props.onChange(null)}>清类目</button></>;
};

describe('TemplateEditorDialog 类目', /* 类目由注入字段选择，推荐账号可切换。 */ () => {
  afterEach(/* 卸载渲染树并清理记录。 */ () => { cleanup(); seen = null; });

  test('新建模板默认带电子资料类目，推荐账号默认取第一个并可切换', /* 初始值与账号。 */ () => {
    render(<TemplateEditorDialog template={null} busy={false} options={options} categoryField={FakeCategoryField} onSave={vi.fn()} onClose={vi.fn()} />);
    expect(seen?.cookieId).toBe('a1');
    expect(seen?.value?.cat_name).toBe('电子资料');
    fireEvent.change(screen.getByLabelText('类目推荐账号'), { target: { value: 'a2' } });
    expect(seen?.cookieId).toBe('a2');
  });

  test('改选写回完整类目；清除后提示自动识别', /* 选择与清除。 */ () => {
    render(<TemplateEditorDialog template={null} busy={false} options={options} categoryField={FakeCategoryField} onSave={vi.fn()} onClose={vi.fn()} />);
    fireEvent.click(screen.getByText('选类目'));
    expect(seen?.value).toEqual({ cat_id: '9', cat_name: '游戏软件平台', channel_cat_id: '99', tb_cat_id: '' });
    fireEvent.click(screen.getByText('清类目'));
    expect(seen?.value).toBeNull();
    expect(screen.getByText(/发布时由平台根据标题和图片自动识别/)).toBeTruthy();
  });

  test('没有注入类目字段时只展示当前类目', /* 降级展示。 */ () => {
    render(<TemplateEditorDialog template={null} busy={false} options={options} onSave={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByText(/类目：电子资料/)).toBeTruthy();
  });
});
