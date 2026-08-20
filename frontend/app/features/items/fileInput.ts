/**
 * consumeSelectedFile 读取文件控件当前选择的首个文件，并立即重置原生值。
 * input 是批量铺货表格控件；返回值是可由 React 状态持有的文件快照，重置让用户可再次选择相同路径的已修改文件。
 */
export const consumeSelectedFile = (input: HTMLInputElement): File | null => {
  // selectedFile 保存本次 change 事件携带的文件快照；无选择或用户取消时为 null。
  const selectedFile = input.files?.[0] || null;
  // 原生控件必须在读取后清空，否则浏览器可能将相同路径的再次选择视为未变化而不触发 change。
  input.value = '';
  return selectedFile;
};
