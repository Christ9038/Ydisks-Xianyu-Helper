import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { expect, test } from 'vitest';
import { RiskVerificationPanel } from './RiskVerificationPanel';

test('risk verification panel explains automatic refresh without manual controls', () => {
  const html = renderToStaticMarkup(
    <RiskVerificationPanel faceQrUrl="data:image/png;base64,abc" />,
  );
  expect(html).toContain('需要完成安全风控验证');
  expect(html).toContain('系统会自动检测并刷新登录状态');
  expect(html).toContain('max-h-[min(64vh,28rem)]');
  expect(html).not.toContain('<button');
  expect(html).not.toContain('我已');
  expect(html).not.toContain('重试');
});
