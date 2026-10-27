// download 提供把内存中的 Blob 保存为浏览器文件的共享能力，供导出类功能复用。

/**
 * downloadBlob 通过临时 <a download> 触发浏览器保存文件。
 * 参数 blob 是完整文件内容；filename 是建议文件名。临时对象 URL 在点击后立即释放。
 */
export const downloadBlob = (blob: Blob, filename: string): void => {
  // url 是指向内存文件的临时对象地址，必须在使用后释放。
  const url = URL.createObjectURL(blob);
  // anchor 是仅用于触发下载的临时链接元素。
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  URL.revokeObjectURL(url);
};

/**
 * filenameFromContentDisposition 从 Content-Disposition 响应头解析文件名。
 * 优先读取 RFC 5987/2231 的 filename*=UTF-8''编码形式，其次读取 filename="..."；
 * 解析失败或缺少响应头时返回 fallback。
 */
export const filenameFromContentDisposition = (header: string | null | undefined, fallback: string): string => {
  if (!header) return fallback;
  // encoded 匹配 filename*=charset'lang'percent-encoded 形式。
  const encoded = /filename\*\s*=\s*[^']*'[^']*'([^;]+)/i.exec(header);
  if (encoded) {
    try {
      // decoded 是百分号解码后的文件名。
      const decoded = decodeURIComponent(encoded[1].trim());
      if (decoded) return decoded;
    } catch {
      // 编码损坏时继续尝试普通 filename 形式。
    }
  }
  // plain 匹配带引号或不带引号的 filename 形式。
  const plain = /filename\s*=\s*(?:"((?:[^"\\]|\\.)*)"|([^;]+))/i.exec(header);
  // value 是去掉引号转义后的普通文件名。
  const value = plain ? (plain[1] ?? plain[2] ?? '').replace(/\\(.)/g, '$1').trim() : '';
  return value || fallback;
};
