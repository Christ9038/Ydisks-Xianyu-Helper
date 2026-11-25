// @vitest-environment jsdom
import { cleanup,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test } from 'vitest';
import NewVersionBadge from './NewVersionBadge';

describe('NewVersionBadge', /* 当前回调验证新版本徽标的链接与变体。 */ () => {
  afterEach(/* 每个用例后卸载渲染树，避免链接叠加。 */ () => cleanup());
  test('箭头与浮窗都链接到对应版本的 GitHub Release 页面', /* 当前回调验证链接地址与安全属性。 */ () => {
    render(<NewVersionBadge latestVersion="v1.2.3" />);
    // links 是徽标与浮窗中的全部链接。
    const links = screen.getAllByRole('link', { hidden: true });
    expect(links).toHaveLength(2);
    for (const link /* link 是单个 Release 链接 */ of links) {
      expect(link.getAttribute('href')).toBe('https://github.com/Christ9038/Ydisks-Xianyu-Helper/releases/tag/v1.2.3');
      expect(link.getAttribute('target')).toBe('_blank');
      expect(link.getAttribute('rel')).toContain('noopener');
    }
    expect(screen.getByRole('tooltip', { hidden: true }).textContent).toContain('有新版本 v1.2.3');
  });

  test('折叠态红点变体没有链接和浮窗，标签也不会进入链接', /* 当前回调验证 dot 变体只提示。 */ () => {
    render(<NewVersionBadge latestVersion="v1/../x" variant="dot" />);
    expect(screen.queryAllByRole('link', { hidden: true })).toHaveLength(0);
    expect(screen.queryAllByRole('tooltip', { hidden: true })).toHaveLength(0);
    expect(screen.getByRole('img').getAttribute('aria-label')).toBe('有新版本 v1/../x');
  });

  test('箭头链接会转义特殊字符的标签', /* 当前回调验证 URL 转义。 */ () => {
    render(<NewVersionBadge latestVersion="v1/../x" />);
    expect(screen.getAllByRole('link', { hidden: true })[0].getAttribute('href')).toBe('https://github.com/Christ9038/Ydisks-Xianyu-Helper/releases/tag/v1%2F..%2Fx');
  });
});
