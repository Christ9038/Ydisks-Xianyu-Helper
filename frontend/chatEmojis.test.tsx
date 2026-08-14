import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, test } from 'vitest';
import { renderXianyuText, xianyuEmojis } from './chatEmojis';

describe('official Xianyu emoji mapping', () => {
  test('contains the complete official panel in official order', () => {
    expect(xianyuEmojis).toHaveLength(126);
    expect(xianyuEmojis[0][0]).toBe('尊嘟假嘟');
    expect(xianyuEmojis.at(-1)?.[0]).toBe('爱心');
    expect(new Set(xianyuEmojis.map(([name]) => name /* 回调函数负责当前业务流程。 */)).size).toBe(126);
  } /* 回调函数负责当前业务流程。 */);

  test('uses official CDN assets and renders bracket codes inline', () => {
    for (const [, url] /* [, url] 表示请求地址。 */ of xianyuEmojis) {
      expect(url).toMatch(/^https:\/\/img\.alicdn\.com\//);
    }
    const html = renderToStaticMarkup(<>{renderXianyuText('你好[捂脸哭][送花]')}</>); /* html 表示html。 */
    expect(html).toContain('你好');
    expect(html).toContain('alt="[捂脸哭]"');
    expect(html).toContain('alt="[送花]"');
  } /* 回调函数负责当前业务流程。 */);
} /* 回调函数负责当前业务流程。 */);
