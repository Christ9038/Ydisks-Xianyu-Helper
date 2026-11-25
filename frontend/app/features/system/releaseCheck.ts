import { useEffect, useState } from 'react';
import { getLatestReleaseTag } from './api';

/** cacheKey 是浏览器本地缓存键，避免每次刷新都消耗 GitHub 匿名限额（每 IP 每小时 60 次）。 */
const cacheKey = 'ydisks.latestRelease.v1';
/** cacheTTLms 是缓存有效期：6 小时。 */
const cacheTTLms = 6 * 60 * 60 * 1000;

/** parseVersion 把 `v1.2.3` / `1.2.3` 解析为三段数字；非标准版本（如 dev、带后缀）返回 undefined。 */
export const parseVersion = (raw: string): [number, number, number] | undefined => {
  // match 只接受严格三段数字版本，其余构建标识不参与比较。
  const match = /^v?(\d+)\.(\d+)\.(\d+)$/.exec(raw.trim());
  return match ? [Number(match[1]), Number(match[2]), Number(match[3])] : undefined;
};

/** isNewerVersion 判断 latest 是否严格高于 current；任一方不是标准版本时返回 false。 */
export const isNewerVersion = (latest: string, current: string): boolean => {
  // a 与 b 分别是最新版本和当前版本的三段数字。
  const a = parseVersion(latest);
  // b 是当前运行版本的三段数字。
  const b = parseVersion(current);
  if (!a || !b) return false;
  // diff 是首个不相等版本段（主、次、修订依次比较）的差值，全部相等时为 0。
  const diff = a.map(/* 当前回调逐段计算最新版本减当前版本。 */ (part, index) => part - b[index]).find(/* 当前回调取首个非零差值。 */ delta => delta !== 0) ?? 0;
  return diff > 0;
};

/** readCachedTag 读取未过期的缓存版本标签；存储不可用或数据损坏时返回 undefined。 */
const readCachedTag = (now: number): string | undefined => {
  try {
    // cached 是缓存的 JSON 文本。
    const cached = window.localStorage.getItem(cacheKey);
    if (!cached) return undefined;
    // parsed 是缓存结构。
    const parsed = JSON.parse(cached) as { /** tag 是缓存的版本标签。 */ tag?: unknown; /** at 是写入时的毫秒时间戳。 */ at?: unknown };
    if (typeof parsed.tag === 'string' && typeof parsed.at === 'number' && now - parsed.at < cacheTTLms) return parsed.tag;
  } catch { /* 存储不可用或损坏时按无缓存处理。 */ }
  return undefined;
};

/** useLatestRelease 返回比当前运行版本更高的 GitHub Release 标签；无更新、非标准版本或请求失败时为 undefined，仅用于提示。 */
export const useLatestRelease = (currentVersion: string): string | undefined => {
  // latestTag 保存已确认更高的最新版本标签。
  const [latestTag, setLatestTag] = useState<string | undefined>();

  useEffect(/* 当前版本确定后检查一次；卸载时取消请求。 */ () => {
    if (!parseVersion(currentVersion)) { setLatestTag(undefined); return undefined; }
    // controller 取消卸载后不再需要的请求。
    const controller = new AbortController();
    // apply 仅在标签更高时更新状态。
    const apply = (tag: string): void => setLatestTag(isNewerVersion(tag, currentVersion) ? tag : undefined);
    // cachedTag 是缓存命中的标签。
    const cachedTag = readCachedTag(Date.now());
    if (cachedTag) { apply(cachedTag); return undefined; }
    getLatestReleaseTag({ signal: controller.signal, timeoutMs: 10_000 })
      .then(/* tag 是 GitHub 最新正式 Release 标签。 */ tag => {
        if (!tag) return;
        try { window.localStorage.setItem(cacheKey, JSON.stringify({ tag, at: Date.now() })); } catch { /* 缓存写入失败不影响提示。 */ }
        apply(tag);
      })
      .catch(/* 网络不可达（如内网/被墙）时静默，不打扰用户。 */ () => undefined);
    return /* cleanup 取消在途请求。 */ () => controller.abort();
  }, [currentVersion]);

  return latestTag;
};
