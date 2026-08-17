import React from 'react';

interface SquareQRCodeProps {
  /** src 表示二维码图片地址。 */ src: string;
  /** alt 表示二维码的替代文本。 */ alt: string;
  /** className 表示附加的 CSS 类名。 */ className?: string;
}

// SquareQRCode 渲染方形二维码图片。
export const SquareQRCode: React.FC<SquareQRCodeProps> = ({ src, alt, className = '' }) => (
  <img
    src={src}
    alt={alt}
    className={`block aspect-square h-auto w-full object-contain ${className}`.trim()}
  />
);
