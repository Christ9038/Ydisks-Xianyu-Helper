import React from 'react';
import { Code, CreditCard, FileText, Image as ImageIcon } from 'lucide-react';
import type { Card } from '../../../../types';

// CardIconProps 描述库存图标所需的卡密类型参数。
interface CardIconProps {
  // type 是当前卡密组的交付类型。
  type: Card['type'];
}

// CardIcon 根据卡密类型渲染稳定的库存图标，避免在列表组件内声明子组件。
export const CardIcon: React.FC<CardIconProps> = ({ type }) => {
  switch (type) {
    case 'text':
      return <FileText className="w-5 h-5 text-blue-500" />;
    case 'image':
      return <ImageIcon className="w-5 h-5 text-purple-500" />;
    case 'api':
      return <Code className="w-5 h-5 text-blue-500" />;
    default:
      return <CreditCard className="w-5 h-5 text-gray-500" />;
  }
};
