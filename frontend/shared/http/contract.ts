/** HTTP transport 响应的兼容归一工具；这里只处理 JSON 形状，不承载任何业务规则。 */

/** JSON 对象的最小运行时视图，避免业务 API 适配器依赖 any。 */
export type JsonObject = Record<string, unknown>;

/** 判断未知值是否可以作为 JSON 对象读取。 */
export const isJsonObject = (value: unknown): value is JsonObject =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

/**
 * 从数组、空值或历史包裹对象中提取集合。
 * 空值、缺失字段和非数组字段统一返回新建空数组，避免页面把 null 当作可迭代值。
 */
export const collectionFrom = <T>(payload: unknown, keys: readonly string[] = ['data', 'items', 'results']): T[] => {
  // currentPayload 保存当前待检查的响应层，允许兼容 data 嵌套对象但不跨越无关字段。
  let currentPayload: unknown = payload;
  // depth 防止异常响应对象造成无限递归；服务端契约最多允许两层历史包裹。
  let depth = 0;
  for (; depth < 2; depth += 1) {
    if (Array.isArray(currentPayload)) return currentPayload as T[];
    if (!isJsonObject(currentPayload)) return [];
    // key 表示当前兼容契约允许的集合字段名。
    for (const key /* key 是当前允许读取的集合字段名。 */ of keys) {
      // candidate 表示候选集合字段；null 视为服务端空集合。
      const candidate = currentPayload[key];
      if (Array.isArray(candidate)) return candidate as T[];
      if (candidate === null) return [];
    }
    // nestedPayload 兼容 { data: { items: [...] } } 这一历史包裹形状。
    const nestedPayload = currentPayload.data;
    if (isJsonObject(nestedPayload)) {
      currentPayload = nestedPayload;
      continue;
    }
    return [];
  }
  return [];
};

/**
 * 从直接对象或 data/result 包裹对象中提取对象。
 * 非对象和 null 都返回 undefined，由 feature 适配器决定默认值。
 */
export const objectFrom = <T>(payload: unknown, keys: readonly string[] = ['data', 'result']): T | undefined => {
  // currentPayload 保存当前待检查的响应层，兼容最多两层历史包裹。
  let currentPayload: unknown = payload;
  // depth 限制兼容层深度，防止错误响应触发无限遍历。
  let depth = 0;
  for (; depth < 2; depth += 1) {
    if (!isJsonObject(currentPayload)) return undefined;
    // key 表示当前兼容契约允许的对象字段名。
    for (const key /* key 是当前允许读取的对象字段名。 */ of keys) {
      // candidate 表示候选对象字段；null 表示没有可用对象。
      const candidate = currentPayload[key];
      if (isJsonObject(candidate)) return candidate as T;
    }
    return currentPayload as T;
  }
  return undefined;
};
