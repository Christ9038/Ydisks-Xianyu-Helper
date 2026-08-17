import type { Dispatch, SetStateAction } from 'react';
import type { AccountDetail, Item, Order, OrderStatus } from '../../../shared/api-contract';

// OrderImportRowResult 描述订单导入接口返回的单行处理结果。
export interface OrderImportRowResult {
  // order_id 是当前导入行对应的订单号。
  order_id: string;
  // success 表示当前订单是否导入成功。
  success: boolean;
  // message 是当前订单的成功或失败说明。
  message: string;
}

// OrderImportResult 描述订单导入接口返回的汇总结果。
export interface OrderImportResult {
  // total 是本次提交的订单总数。
  total: number;
  // success_count 是成功导入的订单数。
  success_count: number;
  // failed_count 是失败订单数。
  failed_count: number;
  // results 保存每一行的处理详情。
  results: OrderImportRowResult[];
}

// OrderQueryState 暴露订单列表的查询数据、筛选条件和展示辅助方法。
export interface OrderQueryState {
  // orders 是当前页的订单列表。
  orders: Order[];
  // items 是用于补充商品标题的商品列表。
  items: Item[];
  // accounts 是订单账号筛选下拉框的数据源。
  accounts: AccountDetail[];
  // filter 是当前订单状态筛选条件。
  filter: string;
  // setFilter 更新订单状态筛选条件。
  setFilter: Dispatch<SetStateAction<string>>;
  // accountFilter 是当前账号筛选条件。
  accountFilter: string;
  // setAccountFilter 更新账号筛选条件。
  setAccountFilter: Dispatch<SetStateAction<string>>;
  // searchText 是搜索框当前输入值。
  searchText: string;
  // setSearchText 更新搜索框当前输入值。
  setSearchText: Dispatch<SetStateAction<string>>;
  // page 是当前分页页码。
  page: number;
  // setPage 更新当前分页页码。
  setPage: Dispatch<SetStateAction<number>>;
  // totalPages 是服务端返回的总页数。
  totalPages: number;
  // loading 表示订单列表或刷新请求是否正在执行。
  loading: boolean;
  // loadOrders 刷新当前筛选条件下的订单列表。
  loadOrders: () => Promise<void>;
  // accountName 将账号 ID 转换为筛选下拉框展示名称。
  accountName: (cookieId: string) => string;
  // accountNickname 将账号 ID 转换为订单行展示名称。
  accountNickname: (cookieId: string) => string;
  // getItemNameById 根据订单信息解析商品展示名称。
  getItemNameById: (cookieId: string, itemId: string, orderItemTitle?: string) => string;
}

// OrderImportState 暴露订单导入弹窗的状态和事件处理函数。
export interface OrderImportState {
  // showImportModal 表示导入弹窗是否打开。
  showImportModal: boolean;
  // importFile 是当前待提交的订单文件。
  importFile: File | null;
  // setImportFile 更新当前待提交的订单文件。
  setImportFile: Dispatch<SetStateAction<File | null>>;
  // importResult 保存最近一次导入的逐行结果。
  importResult: OrderImportResult | null;
  // importing 表示订单导入请求是否正在执行。
  importing: boolean;
  // importError 保存最近一次导入请求的错误说明。
  importError: string;
  // openImportModal 打开弹窗并清理上一次导入状态。
  openImportModal: () => void;
  // closeImportModal 关闭弹窗并取消当前导入请求。
  closeImportModal: () => void;
  // handleImportOrders 提交当前文件并刷新订单列表。
  handleImportOrders: () => Promise<void>;
  // handleRetryImport 重试最近一次失败的导入请求。
  handleRetryImport: () => Promise<void>;
}

// OrderStatusOption 描述订单状态筛选标签的显示配置。
export interface OrderStatusOption {
  // key 是传给订单查询接口的状态值。
  key: string;
  // label 是筛选标签的中文名称。
  label: string;
  // status 是可选的强类型订单状态。
  status?: OrderStatus;
}
