import React from 'react';

interface SquareQRCodeProps {
  src: string;
  alt: string;
  className?: string;
}

export const SquareQRCode: React.FC<SquareQRCodeProps> = ({ src, alt, className = '' }) => (
  <img
    src={src}
    alt={alt}
    className={`block aspect-square h-auto w-full object-contain ${className}`.trim()}
  />
);
