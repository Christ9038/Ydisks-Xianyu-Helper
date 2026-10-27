import { useCallback, useEffect, useRef, useState } from 'react';
import {
  deleteLibraryImage, deleteProductTemplate, listAccountOptions, listCardOptions, listDeliveryTemplateOptions,
  libraryErrorMessage, listLibraryImages, listProductTemplates, saveProductTemplate, uploadLibraryImages, type TemplateRequestTransport,
} from './api';
import type { LibraryAccountOption, LibraryCardOption, LibraryDeliveryTemplateOption, LibraryImage, LibraryUploadResult, ProductTemplate } from './types';

/** isAbort 判断错误是否来自主动取消，取消不应展示给用户。 */
const isAbort = (error: unknown): boolean => error instanceof Error && error.message.includes('取消');

/** LibraryImagesState 是图片库 Hook 的返回值。 */
export interface LibraryImagesState {
  /** images 是最新的图片列表。 */
  images: LibraryImage[];
  /** loading 表示列表正在加载。 */
  loading: boolean;
  /** busy 表示上传或删除正在进行。 */
  busy: boolean;
  /** error 是最近一次失败的用户可见原因。 */
  error: string;
  /** reload 重新读取图片列表。 */
  reload: () => Promise<void>;
  /** upload 上传一批图片并刷新列表，返回逐文件结果。 */
  upload: (files: File[]) => Promise<LibraryUploadResult[]>;
  /** remove 删除一张图片并刷新列表。 */
  remove: (id: number) => Promise<boolean>;
}

/** useLibraryImages 管理图片库列表；旧请求会被取消，过期响应不会覆盖新状态。 */
export const useLibraryImages = (): LibraryImagesState => {
  // images 是服务端图片列表。
  const [images, setImages] = useState<LibraryImage[]>([]);
  // loading 表示列表加载中。
  const [loading, setLoading] = useState(true);
  // busy 表示变更请求进行中，用于防止重复提交。
  const [busy, setBusy] = useState(false);
  // error 保存最近一次失败原因。
  const [error, setError] = useState('');
  // controllerRef 保存当前列表请求的取消控制器。
  const controllerRef = useRef<AbortController | null>(null);
  // generationRef 标记最新列表请求代次。
  const generationRef = useRef(0);

  /** reload 取消旧请求并只让最新代次写入状态。 */
  const reload = useCallback(/* 加载最新列表，过期代次的响应被丢弃。 */ async (): Promise<void> => {
    // generation 是本次请求的代次。
    const generation = ++generationRef.current;
    controllerRef.current?.abort();
    // controller 控制本次请求的取消。
    const controller = new AbortController();
    controllerRef.current = controller;
    setLoading(true);
    try {
      // result 是本次读取到的图片列表。
      const result = await listLibraryImages({ signal: controller.signal });
      if (generation === generationRef.current) { setImages(result); setError(''); }
    } catch (failure /* failure 是本次读取失败原因。 */) {
      if (generation === generationRef.current && !isAbort(failure)) setError(libraryErrorMessage(failure, '读取图片库失败'));
    } finally {
      if (generation === generationRef.current) setLoading(false);
    }
  }, []);

  useEffect(/* 挂载时加载图片库，卸载时取消未完成请求。 */ () => {
    void reload();
    return /* 卸载清理使未完成的响应失效。 */ () => { generationRef.current++; controllerRef.current?.abort(); };
  }, [reload]);

  /** upload 上传图片；全部失败时错误信息来自服务端。 */
  const upload = useCallback(/* 上传图片并刷新列表。 */ async (files: File[]): Promise<LibraryUploadResult[]> => {
    setBusy(true);
    setError('');
    try {
      // results 是逐文件结果。
      const results = await uploadLibraryImages(files);
      await reload();
      return results;
    } catch (failure /* failure 是上传失败原因。 */) {
      setError(libraryErrorMessage(failure, '上传图片失败'));
      return [];
    } finally {
      setBusy(false);
    }
  }, [reload]);

  /** remove 删除图片；被模板引用时保留图片并展示引用它的模板。 */
  const remove = useCallback(/* 删除目标并刷新列表。 */ async (id: number): Promise<boolean> => {
    setBusy(true);
    setError('');
    try {
      await deleteLibraryImage(id);
      await reload();
      return true;
    } catch (failure /* failure 是删除失败原因。 */) {
      setError(libraryErrorMessage(failure, '删除图片失败'));
      return false;
    } finally {
      setBusy(false);
    }
  }, [reload]);

  return { images, loading, busy, error, reload, upload, remove };
};

/** ProductTemplatesState 是商品模板 Hook 的返回值。 */
export interface ProductTemplatesState {
  /** templates 是最新模板列表。 */
  templates: ProductTemplate[];
  /** loading 表示列表正在加载。 */
  loading: boolean;
  /** busy 表示保存或删除正在进行。 */
  busy: boolean;
  /** error 是最近一次失败原因。 */
  error: string;
  /** save 创建或更新模板，成功返回 true。 */
  save: (id: number | null, body: TemplateRequestTransport) => Promise<boolean>;
  /** remove 删除模板，成功返回 true。 */
  remove: (id: number) => Promise<boolean>;
}

/** useProductTemplates 管理商品模板列表，规则与 useLibraryImages 一致。 */
export const useProductTemplates = (): ProductTemplatesState => {
  // templates 是服务端模板列表。
  const [templates, setTemplates] = useState<ProductTemplate[]>([]);
  // loading 表示列表加载中。
  const [loading, setLoading] = useState(true);
  // busy 表示变更进行中。
  const [busy, setBusy] = useState(false);
  // error 保存最近一次失败原因。
  const [error, setError] = useState('');
  // controllerRef 保存当前列表请求的取消控制器。
  const controllerRef = useRef<AbortController | null>(null);
  // generationRef 标记最新列表请求代次。
  const generationRef = useRef(0);

  /** reload 读取最新列表，过期响应被丢弃。 */
  const reload = useCallback(/* 加载最新列表，过期代次的响应被丢弃。 */ async (): Promise<void> => {
    // generation 是本次请求的代次。
    const generation = ++generationRef.current;
    controllerRef.current?.abort();
    // controller 控制本次请求的取消。
    const controller = new AbortController();
    controllerRef.current = controller;
    setLoading(true);
    try {
      // result 是本次读取到的模板列表。
      const result = await listProductTemplates({ signal: controller.signal });
      if (generation === generationRef.current) { setTemplates(result); setError(''); }
    } catch (failure /* failure 是本次读取失败原因。 */) {
      if (generation === generationRef.current && !isAbort(failure)) setError(libraryErrorMessage(failure, '读取商品模板失败'));
    } finally {
      if (generation === generationRef.current) setLoading(false);
    }
  }, []);

  useEffect(/* 挂载时加载模板，卸载时取消未完成请求。 */ () => {
    void reload();
    return /* 卸载清理使未完成的响应失效。 */ () => { generationRef.current++; controllerRef.current?.abort(); };
  }, [reload]);

  /** save 保存模板后刷新列表。 */
  const save = useCallback(/* 创建或更新模板并刷新列表。 */ async (id: number | null, body: TemplateRequestTransport): Promise<boolean> => {
    setBusy(true);
    setError('');
    try {
      await saveProductTemplate(id, body);
      await reload();
      return true;
    } catch (failure /* failure 是保存失败原因。 */) {
      setError(libraryErrorMessage(failure, '保存商品模板失败'));
      return false;
    } finally {
      setBusy(false);
    }
  }, [reload]);

  /** remove 删除模板后刷新列表。 */
  const remove = useCallback(/* 删除目标并刷新列表。 */ async (id: number): Promise<boolean> => {
    setBusy(true);
    setError('');
    try {
      await deleteProductTemplate(id);
      await reload();
      return true;
    } catch (failure /* failure 是删除失败原因。 */) {
      setError(libraryErrorMessage(failure, '删除商品模板失败'));
      return false;
    } finally {
      setBusy(false);
    }
  }, [reload]);

  return { templates, loading, busy, error, save, remove };
};

/** AutomationOptions 是自动化编辑器需要的卡密组与发货模板选项。 */
export interface AutomationOptions {
  /** cards 是可用卡密组。 */
  cards: LibraryCardOption[];
  /** deliveryTemplates 是可用发货模板。 */
  deliveryTemplates: LibraryDeliveryTemplateOption[];
  /** accounts 是可发布账号。 */
  accounts: LibraryAccountOption[];
  /** error 是读取选项失败的原因。 */
  error: string;
}

/** useAutomationOptions 并行读取卡密组、发货模板和账号选项；卸载或重复触发时取消旧请求。 */
export const useAutomationOptions = (enabled: boolean): AutomationOptions => {
  // options 是三类选项的合并状态。
  const [options, setOptions] = useState<AutomationOptions>({ cards: [], deliveryTemplates: [], accounts: [], error: '' });
  useEffect(/* 启用时并行读取三类选项，并用取消标记丢弃过期响应。 */ () => {
    if (!enabled) return undefined;
    // controller 控制三条并行请求的取消。
    const controller = new AbortController();
    void Promise.all([listCardOptions({ signal: controller.signal }), listDeliveryTemplateOptions({ signal: controller.signal }), listAccountOptions({ signal: controller.signal })])
      .then(/* 三类选项全部成功后一次写入。 */ ([cards, deliveryTemplates, accounts]) => { if (!controller.signal.aborted) setOptions({ cards, deliveryTemplates, accounts, error: '' }); })
      .catch(/* 失败时展示原因，取消不提示。 */ (failure /* failure 是读取选项失败原因。 */) => { if (!controller.signal.aborted && !isAbort(failure)) setOptions(/* previous 是失败前的选项状态。 */ previous => ({ ...previous, error: libraryErrorMessage(failure, '读取发货选项失败') })); });
    return /* 清理时取消未完成请求。 */ () => controller.abort();
  }, [enabled]);
  return options;
};
