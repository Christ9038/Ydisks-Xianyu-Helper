// 旧目录保留兼容入口，实际登录字段构造逻辑已归入 accounts feature。
export {
  buildAccountLoginInfoUpdate,
} from '../app/features/accounts/state';
export type {
  AccountLoginEditFields as AccountLoginEditForm,
  AccountLoginInfoPayload,
} from '../app/features/accounts/state';
