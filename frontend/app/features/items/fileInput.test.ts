// @vitest-environment jsdom
// 本测试需要浏览器文件控件与 File API，因此固定使用 jsdom 环境。
import { describe,expect,test } from 'vitest';
import { consumeSelectedFile } from './fileInput';

// ItemList 文件选择测试覆盖同一路径的表格在保存后能够被重新读取。
describe('consumeSelectedFile',
  // 测试组回调验证读取文件后重置原生 input 的浏览器兼容行为。
  () => {
  // 连续两次选择同名文件时，每次都返回对应 File 快照并清空上一轮原生选择值。
  test('clears the native input after each selected file',
    // 测试回调模拟浏览器在用户重新选择同一路径前写入的文件列表和伪路径值。
    () => {
    // input 保存可被测试替换 files 与 value 行为的文件控件。
    const input = document.createElement('input');
    // currentValue 保存浏览器文件控件的当前伪路径值，空值代表下次同一路径选择会重新触发 change。
    let currentValue = 'C:\\fakepath\\products.xlsx';
    Object.defineProperty(input, 'value', {
      configurable: true,
      get: /* 当前访问器返回模拟的文件控件伪路径。 */ () => currentValue,
      set: /* 当前访问器记录业务代码清空原生控件的操作。 */ value => { currentValue = String(value); },
    });
    // firstFile 保存首次选择时浏览器创建的文件快照。
    const firstFile = new File(['old-content'], 'products.xlsx', { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' });
    Object.defineProperty(input, 'files', { configurable: true, value: [firstFile] as unknown as FileList });
    expect(consumeSelectedFile(input)).toBe(firstFile);
    expect(currentValue).toBe('');

    // secondFile 保存同名文件修改后再次选择时浏览器创建的新快照。
    const secondFile = new File(['new-content'], 'products.xlsx', { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' });
    currentValue = 'C:\\fakepath\\products.xlsx';
    Object.defineProperty(input, 'files', { configurable: true, value: [secondFile] as unknown as FileList });
    expect(consumeSelectedFile(input)).toBe(secondFile);
    expect(currentValue).toBe('');
  });
  });
