import React from 'react';
import { Search, User as UserIcon } from 'lucide-react';
import type { AccountDetail } from '../../../../types';
import { orderStatusOptions } from '../state';

// OrderFilterBarProps 描述订单状态、账号和文本筛选所需的页面状态。
export interface OrderFilterBarProps {
  // filter 是当前订单状态筛选值。
  filter: string;
  // onFilterChange 响应订单状态筛选切换。
  onFilterChange: (value: string) => void;
  // accountFilter 是当前账号筛选值。
  accountFilter: string;
  // onAccountFilterChange 响应账号筛选切换。
  onAccountFilterChange: (value: string) => void;
  // accounts 是账号下拉框的数据源。
  accounts: AccountDetail[];
  // accountName 将账号 ID 转换为展示名称。
  accountName: (cookieId: string) => string;
  // searchText 是搜索框当前输入值。
  searchText: string;
  // onSearchChange 响应订单搜索输入。
  onSearchChange: (value: string) => void;
}

// OrderFilterBar 渲染订单状态、账号和关键词筛选工具栏。
export const OrderFilterBar: React.FC<OrderFilterBarProps> = ({
  filter,
  onFilterChange,
  accountFilter,
  onAccountFilterChange,
  accounts,
  accountName,
  searchText,
  onSearchChange,
}) => {
  // handleStatusClick 将用户选择的状态传回订单页面。
  const handleStatusClick = (event: React.MouseEvent<HTMLButtonElement>) => onFilterChange(event.currentTarget.dataset.status || 'all');
  // handleAccountChange 将用户选择的账号传回订单页面。
  const handleAccountChange = (event: React.ChangeEvent<HTMLSelectElement>) => onAccountFilterChange(event.target.value);
  // handleSearchChange 将用户输入传回订单页面。
  const handleSearchChange = (event: React.ChangeEvent<HTMLInputElement>) => onSearchChange(event.target.value);

  return (
    <div className="p-4 border-b border-gray-50 flex flex-col md:flex-row gap-4 justify-between items-center bg-surface-muted">
      <div className="flex gap-1 p-1 bg-gray-200/50 rounded-xl overflow-x-auto max-w-full">
        {orderStatusOptions.map(
          // option 是当前订单状态筛选标签配置。
          option => (
            <button
              key={option.key}
              data-status={option.key}
              onClick={handleStatusClick}
              className={`px-5 py-2 rounded-lg text-sm font-bold transition-all whitespace-nowrap ${filter === option.key ? 'bg-white text-black shadow-sm' : 'text-gray-500 hover:text-gray-700'}`}
            >
              {option.label}
            </button>
          ),
        )}
      </div>
      <div className="flex w-full md:w-auto flex-col sm:flex-row gap-3">
        <div className="relative">
          <UserIcon className="w-4 h-4 absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none" />
          <select
            aria-label="按账号筛选订单"
            value={accountFilter}
            onChange={handleAccountChange}
            className="ios-input pl-10 pr-9 py-2.5 rounded-xl w-full sm:w-56 bg-white border-none shadow-sm"
          >
            <option value="">全部账号</option>
            {accounts.map(
              // account 是当前账号筛选下拉项。
              account => <option key={account.id} value={account.id}>{accountName(account.id)}</option>,
            )}
          </select>
        </div>
        <div className="relative group">
          <Search className="w-4 h-4 absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 group-focus-within:text-brand transition-colors" />
          <input
            type="text"
            placeholder="搜索订单号/商品/买家..."
            value={searchText}
            onChange={handleSearchChange}
            className="ios-input pl-10 pr-4 py-2.5 rounded-xl w-64 bg-white border-none shadow-sm focus:ring-0"
          />
        </div>
      </div>
    </div>
  );
};
