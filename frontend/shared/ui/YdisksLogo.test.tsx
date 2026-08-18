import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, test } from 'vitest';
import YdisksLogo, { YdisksBrandIcon } from './YdisksLogo';

describe('YdisksLogo', /* 当前回调处理用户交互或异步状态变化。 */ () => {
  test('uses the complete 256px brand coordinate system', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    // html 渲染后的 HTML。
    const html = renderToStaticMarkup(<YdisksLogo />);
    expect(html).toContain('viewBox="0 0 256 256"');
    expect(html).toContain('M121.73,57.0003');
    expect(html).toContain('fill="currentColor"');
  });

  test('renders the complete gradient squircle brand wrapper', /* 当前回调处理用户交互或异步状态变化。 */ () => {
    // html 渲染后的 HTML。
    const html = renderToStaticMarkup(<YdisksBrandIcon />);
    expect(html).toContain('viewBox="0 0 120 120"');
    expect(html).toContain('id="login-squircle-gradient"');
    expect(html).toContain('M 114.00 60.00');
    expect(html).toContain('w-12 h-12');
  });
});
