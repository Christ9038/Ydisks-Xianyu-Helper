import { describe, expect, it } from 'vitest';
import { imageToken, parseImageToken } from './messageImage';

describe('messageImage', /* 当前回调验证图片令牌的序列化与解析。 */ () => {
  it('令牌往返一致，且只识别整条消息', /* 当前回调验证往返与空白容忍。 */ () => {
    expect(imageToken(12)).toBe('{{library_image:12}}');
    expect(parseImageToken(imageToken(12))).toBe(12);
    expect(parseImageToken(`  ${imageToken(3)}  `)).toBe(3);
  });

  it('普通文本、混排文本和非法主键都不是图片消息', /* 当前回调验证非图片消息均解析为 0。 */ () => {
    for (const /* content 是待解析的非图片消息正文。 */ content of ['', '你好', `前缀${imageToken(1)}`, '{{library_image:0}}', '{{library_image:abc}}', '{{library_image:01}}']) {
      expect(parseImageToken(content)).toBe(0);
    }
  });
});
