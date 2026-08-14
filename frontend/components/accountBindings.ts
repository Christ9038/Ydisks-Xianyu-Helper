// shouldSaveNotificationBindings 判断通知绑定是否需要保存。
export const shouldSaveNotificationBindings = (loaded: boolean, dirty: boolean): boolean => loaded && dirty;
