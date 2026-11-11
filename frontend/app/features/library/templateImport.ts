import { useCallback, useEffect, useRef, useState } from 'react';
import { downloadCsv } from '../../../shared/browser/csvDownload';
import { commitTemplateImport, discardTemplateImport, libraryErrorMessage, previewTemplateImport } from './api';
import type { LibraryImportPreview, LibraryImportProgress } from './types';

/** TemplateImportPhase 是导入流程阶段：空闲、预检中、预检完成、导入中、导入完成。 */
export type TemplateImportPhase = 'idle' | 'previewing' | 'previewed' | 'importing' | 'done';

/** TemplateImportState 是 Excel 导入商品模板 Hook 的返回值。 */
export interface TemplateImportState {
  /** sheet 是用户选择的商品表格。 */
  sheet: File | null;
  /** imagesZip 是用户选择的可选图片压缩包。 */
  imagesZip: File | null;
  /** phase 是当前阶段。 */
  phase: TemplateImportPhase;
  /** preview 是最近一次预检结果。 */
  preview: LibraryImportPreview | null;
  /** progress 是最近一次分段提交后的进度。 */
  progress: LibraryImportProgress | null;
  /** error 是最近一次失败原因。 */
  error: string;
  /** selectSheet 更换表格，已有预检结果作废并丢弃服务端暂存。 */
  selectSheet: (file: File | null) => void;
  /** selectImagesZip 更换图片压缩包，已有预检结果作废并丢弃服务端暂存。 */
  selectImagesZip: (file: File | null) => void;
  /** runPreview 上传文件并预检。 */
  runPreview: () => Promise<void>;
  /** startImport 循环分段提交直到导入完成；中途失败后可再次调用以继续。 */
  startImport: () => Promise<void>;
  /** cancel 停止进行中的请求并丢弃未完成的导入任务，已创建的模板保留。 */
  cancel: () => void;
}

/** importSheetHeaders 是导入商品模板的 CSV 表头：与批量铺货列一致，但没有“账号ID”（发布时再选账号），首列为可选的“模板名称”。 */
export const importSheetHeaders = ['模板名称', '标题', '描述', '价格', '原价', '库存', '邮费模式', '邮费', '图片', '类目ID', '类目名称', '频道类目ID', '淘宝类目ID',
  '付款发货启用', '付款发货内容', '评价赠品启用', '评价赠品内容', '求评价启用', '求评价等待小时', '求评价文案', '求评价最多次数', '变量.link'];

/** importSheetExamples 是导入模板的示例行：引用共享发货模板 1 的网盘资源、只发卡密的普通虚拟商品。 */
const importSheetExamples = [
  ['网盘资源A', '网盘资源自动发链接', '付款后自动发送网盘链接。', '9.90', '', '99', 'none', '', 'https://example.com/a.jpg', '', '', '', '', '是', 'T1', '否', '', '否', '', '', '', 'https://pan.example.com/s/xxxx'],
  ['', '会员卡自动发货', '下单后发送卡密。', '19.90', '29.90', '10', 'none', '', 'images/card.jpg', '', '', '', '', '是', '101:1', '否', '', '是', '72', '亲，满意的话麻烦给个评价，谢谢～', '1', ''],
];

/** downloadImportTemplate 下载导入商品模板用的 CSV 模板。 */
export const downloadImportTemplate = (): void => {
  downloadCsv('商品模板导入模板.csv', [importSheetHeaders, ...importSheetExamples]);
};

/** isAbortError 判断错误是否来自主动取消，取消不应展示给用户。 */
const isAbortError = (error: unknown): boolean => error instanceof Error && (error.name === 'AbortError' || error.message.includes('取消'));

/**
 * useTemplateImport 管理 Excel 导入商品模板的完整流程。
 * onImported 在导入完成或中途取消时收到至今创建的模板主键，用于刷新列表并勾选新模板。
 * 每个请求都有独立的 AbortController，并以代次号丢弃过期响应；卸载时取消请求并丢弃未完成的暂存任务。
 */
export const useTemplateImport = (onImported: (templateIds: number[]) => void): TemplateImportState => {
  // sheet、imagesZip 是用户选择的文件。
  const [sheet, setSheet] = useState<File | null>(null);
  // imagesZip 是可选图片压缩包。
  const [imagesZip, setImagesZip] = useState<File | null>(null);
  // phase 是当前阶段。
  const [phase, setPhase] = useState<TemplateImportPhase>('idle');
  // preview 是最近一次预检结果。
  const [preview, setPreview] = useState<LibraryImportPreview | null>(null);
  // progress 是最近一次分段提交后的进度。
  const [progress, setProgress] = useState<LibraryImportProgress | null>(null);
  // error 是最近一次失败原因。
  const [error, setError] = useState('');
  // generationRef 是请求代次，任何作废操作都会递增它，使旧响应被丢弃。
  const generationRef = useRef(0);
  // controllerRef 是当前请求的取消控制器。
  const controllerRef = useRef<AbortController | null>(null);
  // pendingImportRef 是尚未完成、需要在作废时丢弃的服务端导入任务标识。
  const pendingImportRef = useRef('');
  // onImportedRef 保存最新回调，避免回调变化导致循环重建。
  const onImportedRef = useRef(onImported);
  useEffect(/* 同步最新的导入完成回调。 */ () => { onImportedRef.current = onImported; }, [onImported]);

  /** invalidate 作废进行中的请求与未完成的服务端任务；后台丢弃失败不影响界面，暂存会在 24 小时后自动清理。 */
  const invalidate = useCallback(/* 作废当前请求并丢弃未完成任务。 */ (): void => {
    generationRef.current++;
    controllerRef.current?.abort();
    controllerRef.current = null;
    // importId 是需要丢弃的任务。
    const importId = pendingImportRef.current;
    pendingImportRef.current = '';
    if (importId) void discardTemplateImport(importId).catch(/* 丢弃失败由服务端过期清理兜底。 */ () => undefined);
  }, []);

  useEffect(/* 卸载时作废请求并丢弃未完成任务。 */ () => invalidate, [invalidate]);

  /** resetResults 清空预检与进度，回到空闲阶段。 */
  const resetResults = useCallback(/* 清空结果。 */ (): void => {
    invalidate();
    setPreview(null);
    setProgress(null);
    setError('');
    setPhase('idle');
  }, [invalidate]);

  /** selectSheet 更换表格并作废旧结果。 */
  const selectSheet = useCallback(/* 更换表格。 */ (file: File | null): void => { resetResults(); setSheet(file); }, [resetResults]);

  /** selectImagesZip 更换压缩包并作废旧结果。 */
  const selectImagesZip = useCallback(/* 更换压缩包。 */ (file: File | null): void => { resetResults(); setImagesZip(file); }, [resetResults]);

  /** runPreview 上传当前文件并预检，过期响应被丢弃。 */
  const runPreview = useCallback(/* 执行预检。 */ async (): Promise<void> => {
    if (!sheet) { setError('请选择商品表格'); return; }
    invalidate();
    // generation 是本次请求代次。
    const generation = generationRef.current;
    // controller 控制本次预检请求。
    const controller = new AbortController();
    controllerRef.current = controller;
    setPhase('previewing');
    setPreview(null);
    setProgress(null);
    setError('');
    try {
      // result 是预检结果。
      const result = await previewTemplateImport({ sheet, imagesZip }, { signal: controller.signal });
      if (generation !== generationRef.current) {
        if (result.importId) void discardTemplateImport(result.importId).catch(/* 过期任务由服务端过期清理兜底。 */ () => undefined);
        return;
      }
      pendingImportRef.current = result.importId;
      setPreview(result);
      setPhase('previewed');
    } catch (failure /* failure 是预检失败原因。 */) {
      if (generation !== generationRef.current) return;
      // 非本 Hook 作废引起的中止（例如外部信号）不提示错误，但必须退出“预检中”，否则按钮会一直不可用。
      if (!isAbortError(failure)) setError(libraryErrorMessage(failure, '导入预检失败'));
      setPhase('idle');
    }
  }, [imagesZip, invalidate, sheet]);

  /** startImport 循环分段提交，直到服务端返回 done；每次响应都更新进度。 */
  const startImport = useCallback(/* 执行导入。 */ async (): Promise<void> => {
    // importId 是待导入任务。
    const importId = pendingImportRef.current;
    if (!importId) return;
    controllerRef.current?.abort();
    // generation 是本轮导入的代次，作废后循环立即停止。
    const generation = ++generationRef.current;
    // controller 控制本轮全部分段请求。
    const controller = new AbortController();
    controllerRef.current = controller;
    setPhase('importing');
    setError('');
    try {
      for (;;) {
        // next 是本次分段提交后的进度。
        const next = await commitTemplateImport(importId, { signal: controller.signal });
        if (generation !== generationRef.current) return;
        setProgress(next);
        if (next.done) {
          pendingImportRef.current = '';
          setPhase('done');
          onImportedRef.current(next.createdTemplateIds);
          return;
        }
      }
    } catch (failure /* failure 是分段提交失败原因。 */) {
      if (generation !== generationRef.current) return;
      // 同上：外部中止不提示错误，但回到可“继续导入”的阶段，已完成的进度保留在服务端。
      if (!isAbortError(failure)) setError(libraryErrorMessage(failure, '导入商品模板失败'));
      setPhase('previewed');
    }
  }, []);

  /** cancel 停止当前流程：已创建的模板通过回调刷新到列表，未完成的任务被丢弃。 */
  const cancel = useCallback(/* 取消导入。 */ (): void => {
    // created 是至今已创建的模板。
    const created = progress?.createdTemplateIds ?? [];
    resetResults();
    if (created.length > 0) onImportedRef.current(created);
  }, [progress, resetResults]);

  return { sheet, imagesZip, phase, preview, progress, error, selectSheet, selectImagesZip, runPreview, startImport, cancel };
};
