import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { expect, test } from 'vitest';
import { SquareQRCode } from './SquareQRCode';

test('login QR code preserves a square aspect ratio without stretching', () => {
  const html = renderToStaticMarkup(<SquareQRCode src="data:image/png;base64,abc" alt="登录二维码" className="p-2" />);
  expect(html).toContain('aspect-square');
  expect(html).toContain('h-auto');
  expect(html).toContain('object-contain');
  expect(html).toContain('p-2');
});
