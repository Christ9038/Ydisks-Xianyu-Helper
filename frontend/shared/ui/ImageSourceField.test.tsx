// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import { ImageSourceField } from './ImageSourceField';
import type { LibraryImageChooserProps } from './imageSource';

/** FakeChooser 是测试用图片库选择器：打开时渲染一个按钮，点击即选中主键 11。 */
const FakeChooser = ({ open, onClose, onSelect }: LibraryImageChooserProps) => open
  ? <div><button type="button" onClick={/* 选中固定图片。 */ () => onSelect({ id: 11, filename: 'a.png' })}>选中图片</button><button type="button" onClick={onClose}>关闭选择器</button></div>
  : null;

describe('ImageSourceField', () => {
  afterEach(/* 每个用例后卸载渲染，避免 DOM 在用例间累积。 */ cleanup);

  test('输入外链时回传外链并清空图片库引用', /* 当前回调验证外链输入的互斥语义。 */ () => {
    // onChange 记录控件回传的新图片来源。
    const onChange = vi.fn();
    render(<ImageSourceField value={{ url: '', libraryImageId: 0 }} onChange={onChange} />);
    fireEvent.change(screen.getByPlaceholderText('https://example.com/image.jpg'), { target: { value: 'https://a/b.png' } });
    expect(onChange).toHaveBeenCalledWith({ url: 'https://a/b.png', libraryImageId: 0 });
    expect(screen.queryByText('从图片库选择')).toBeNull();
  });

  test('从图片库选择后清空外链，更换与清除可用', /* 当前回调验证选择器注入后的完整交互。 */ () => {
    // onChange 记录控件回传的新图片来源。
    const onChange = vi.fn();
    // view 提供 rerender，用于模拟父级写回新值。
    const view = render(<ImageSourceField value={{ url: 'https://old/x.png', libraryImageId: 0 }} onChange={onChange} chooser={FakeChooser} />);
    fireEvent.click(screen.getByText('从图片库选择'));
    fireEvent.click(screen.getByText('选中图片'));
    expect(onChange).toHaveBeenLastCalledWith({ url: '', libraryImageId: 11 });
    expect(screen.queryByText('选中图片')).toBeNull();
    view.rerender(<ImageSourceField value={{ url: '', libraryImageId: 11 }} onChange={onChange} chooser={FakeChooser} />);
    expect(screen.getByAltText('已选图片库图片').getAttribute('src')).toBe('/api/v1/library/images/11/content');
    fireEvent.click(screen.getByText('清除'));
    expect(onChange).toHaveBeenLastCalledWith({ url: '', libraryImageId: 0 });
    fireEvent.click(screen.getByText('更换'));
    fireEvent.click(screen.getByText('关闭选择器'));
    expect(screen.queryByText('选中图片')).toBeNull();
  });

  test('只允许图片库时不显示外链输入', /* 当前回调验证 allowUrl=false 的发货模板场景。 */ () => {
    render(<ImageSourceField value={{ url: '', libraryImageId: 0 }} onChange={vi.fn()} chooser={FakeChooser} allowUrl={false} />);
    expect(screen.queryByPlaceholderText('https://example.com/image.jpg')).toBeNull();
    expect(screen.getByText('从图片库选择')).toBeTruthy();
  });
});
