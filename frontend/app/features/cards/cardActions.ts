import { useCallback,useEffect,useMemo,useRef,useState,type Dispatch,type SetStateAction } from 'react';
import type { Card,CardMutation } from './api';
import { createCard,deleteCard,getCardAPIEditor,updateCard } from './api';
import { apiConfigFromEditForm, apiConfigFromAddForm } from './editConfig';
import { filterCards } from './batchState';
import type { AddCardForm,EditCardForm } from './types';

// emptyAddForm 创建新增卡密组表单的初始值。
export const emptyAddForm = (): AddCardForm => ({
  name: '',
  type: 'data',
  content: '',
  library_image_id: 0,
  description: '',
  enabled: true,
  delay_seconds: 0,
  api_method: 'GET',
  api_timeout: 10,
  api_headers: '',
  api_params: '',
  api_content_type: 'application/json',
  api_body: '',
  api_response_path: '',
  api_retry_enabled: false,
});

// CardActionsOptions 描述卡密动作协调器依赖的库存状态和刷新操作。
export interface CardActionsOptions {
  // cards 保存当前卡密组列表。
  cards: Card[];
  // loadCards 刷新卡密组列表。
  loadCards: () => Promise<void>;
}

// CardActionsState 暴露卡密页面的短暂状态、筛选状态和动作函数。
export interface CardActionsState {
  // dataCards 保存可追加库存的 data 类型卡密组。
  dataCards: Card[];
  // filteredCards 保存筛选后的卡密组列表。
  filteredCards: Card[];
  // typeFilter 保存当前卡密类型筛选条件。
  typeFilter: Card['type'] | '';
  // setTypeFilter 更新卡密类型筛选条件。
  setTypeFilter: Dispatch<SetStateAction<Card['type'] | ''>>;
  // nameSearch 保存当前卡密名称搜索文本。
  nameSearch: string;
  // setNameSearch 更新卡密名称搜索文本。
  setNameSearch: Dispatch<SetStateAction<string>>;
  // showEditModal 表示编辑卡密弹窗是否打开。
  showEditModal: boolean;
  // closeEdit 取消读取并销毁含密钥的编辑草稿。
  closeEdit: () => void;
  // editLoading 表示完整配置仍在读取。
  editLoading: boolean;
  // editError 保存配置读取失败的安全提示。
  editError: string;
  // showAddModal 表示新增卡密弹窗是否打开。
  showAddModal: boolean;
  // setShowAddModal 更新新增弹窗展示状态。
  setShowAddModal: Dispatch<SetStateAction<boolean>>;
  // selectedCard 保存当前正在编辑的卡密组。
  selectedCard: Card | null;
  // editForm 保存当前卡密编辑草稿。
  editForm: EditCardForm;
  // setEditForm 更新当前卡密编辑草稿。
  setEditForm: Dispatch<SetStateAction<EditCardForm>>;
  // addForm 保存当前新增卡密表单。
  addForm: AddCardForm;
  // setAddForm 更新当前新增卡密表单。
  setAddForm: Dispatch<SetStateAction<AddCardForm>>;
  // handleEdit 打开指定卡密组的编辑弹窗。
  handleEdit: (card: Card) => Promise<void>;
  // handleSaveEdit 保存当前卡密编辑草稿。
  handleSaveEdit: () => Promise<void>;
  // handleDelete 删除指定卡密组并刷新库存。
  handleDelete: (id: string | number) => Promise<void>;
  // handleAddCard 创建新的卡密组并刷新库存。
  handleAddCard: () => Promise<void>;
  // toggleCardStatus 切换指定卡密组的启用状态。
  toggleCardStatus: (card: Card) => Promise<void>;
  // copyCardID 复制卡密组标识并提供失败回退。
  copyCardID: (id: string | number) => Promise<void>;
  // downloadCardTemplate 下载批量导入模板文件。
  downloadCardTemplate: () => void;
}

// cardErrorMessage 将未知异常转换为稳定的卡密操作提示。
const cardErrorMessage = (error: unknown, fallback = '状态失败'): string => error instanceof Error ? error.message : fallback;

// useCardActions 集中管理卡密新增、编辑、删除、筛选和展示动作。
export const useCardActions = ({ cards, loadCards }: CardActionsOptions): CardActionsState => {
  // showEditModal 表示编辑卡密弹窗是否打开。
  const [showEditModal, setShowEditModal] = useState(false);
  // showAddModal 表示新增卡密弹窗是否打开。
  const [showAddModal, setShowAddModal] = useState(false);
  // selectedCard 保存当前正在编辑的卡密组。
  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  // editForm 保存当前卡密编辑草稿。
  const [editForm, setEditForm] = useState<EditCardForm>({});
  // addForm 保存当前新增卡密表单。
  const [addForm, setAddForm] = useState<AddCardForm>(emptyAddForm);
  // typeFilter 保存当前卡密类型筛选条件。
  const [typeFilter, setTypeFilter] = useState<Card['type'] | ''>('');
  // nameSearch 保存当前卡密名称搜索文本。
  const [nameSearch, setNameSearch] = useState('');
  // dataCards 保存可追加库存的 data 类型卡密组。
  const dataCards = useMemo(/* dataCardsMemo 按类型派生可追加库存列表。 */ () => cards.filter(/* dataCardFilter 筛选 data 类型卡密组。 */ card => card.type === 'data'), [cards]);
  // filteredCards 保存应用类型和名称条件后的卡密列表。
  const filteredCards = useMemo(
    /* filteredCardsMemo 根据当前条件派生卡密列表。 */ () => filterCards(cards, typeFilter, nameSearch),
    [cards, nameSearch, typeFilter],
  );

  // editorRequest 标识当前编辑会话，旧请求完成时不能覆盖新草稿。
  const editorRequest = useRef<AbortController | null>(null);
  // editLoading 在完整配置载入前禁用测试与保存。
  const [editLoading, setEditLoading] = useState(false);
  // editError 显示读取失败提示，避免使用不完整表单覆盖配置。
  const [editError, setEditError] = useState('');
  // closeEdit 立即清除当前弹窗持有的敏感配置。
  const closeEdit = useCallback(/* closeEditor 结束当前编辑会话。 */ () => {
    editorRequest.current?.abort();
    editorRequest.current = null;
    setEditForm({});
    setSelectedCard(null);
    setEditLoading(false);
    setEditError('');
    setShowEditModal(false);
  }, []);
  // cleanupEditor 页面卸载时取消仍在进行的编辑配置读取。
  useEffect(/* editorLifecycle 注册卸载清理。 */ () => /* abortEditor 取消页面卸载后的配置读取。 */ () => editorRequest.current?.abort(), []);

  // handleEdit 打开指定卡密组的编辑弹窗并初始化草稿。
  const handleEdit = useCallback(/* editAction 打开卡密编辑弹窗。 */ async (card: Card) => {
    editorRequest.current?.abort();
    // request 绑定这次打开操作，隔离切换卡密与关闭后的迟到响应。
    const request = new AbortController();
    editorRequest.current = request;
    setEditLoading(card.type === 'api');
    setEditError('');
    setSelectedCard(card);
    setEditForm({
      id: card.id,
      name: card.name || '',
      type: card.type || 'text',
      api_url: card.api_config?.url || '',
      api_method: card.api_config?.method || 'GET',
      api_timeout: card.api_config?.timeout_seconds || 10,
      api_headers: '',
      api_params: '',
      api_content_type: card.api_config?.content_type || 'application/json',
      api_body: '',
      api_response_path: card.api_config?.response_path || '',
      api_retry_enabled: card.api_config?.retry_enabled || false,
      api_headers_action: 'retain',
      api_params_action: 'retain',
      text_content: card.text_content || '',
      data_content: card.data_content || '',
      image_url: card.image_url || '',
      library_image_id: card.library_image_id || 0,
      delay_seconds: card.delay_seconds || 0,
      description: card.description || '',
      enabled: card.enabled,
    });
    setShowEditModal(true);
    if (card.type !== 'api') return;
    try {
      // config 只进入当前编辑草稿，不写入列表缓存或浏览器存储。
      const config = await getCardAPIEditor(card.id, { signal: request.signal });
      if (request.signal.aborted) return;
      setEditForm(/* loadEditor 将已授权的完整配置载入当前草稿。 */ current => ({
        ...current, api_url: config.url, api_method: config.method, api_timeout: config.timeout_seconds,
        api_content_type: config.content_type, api_headers: config.headers, api_params: config.params,
        api_body: config.body, api_response_path: config.response_path || '', api_retry_enabled: config.retry_enabled,
      }));
    } catch {
      if (!request.signal.aborted) setEditError('读取配置失败，请重试');
    } finally {
      if (!request.signal.aborted) setEditLoading(false);
    }
  }, []);

  // handleSaveEdit 保存当前卡密编辑草稿并刷新库存。
  const handleSaveEdit = useCallback(/* saveEditAction 保存卡密编辑草稿。 */ async () => {
    if (!selectedCard || editLoading || editError) return;
    if (!editForm.name?.trim()) {
      alert('请输入卡密名称');
      return;
    }
    if (!editForm.type) {
      alert('请选择卡密类型');
      return;
    }
    try {
      // updateData 保存映射到卡密更新接口的字段。
      const updateData: CardMutation = {
        name: editForm.name.trim(),
        type: editForm.type,
        description: editForm.description?.trim(),
        delay_seconds: editForm.delay_seconds || 0,
        enabled: editForm.enabled ?? true,
      };
      if (editForm.type === 'api') {
        updateData.api_config = apiConfigFromEditForm(editForm);
      } else if (editForm.type === 'text') {
        updateData.text_content = editForm.text_content?.trim() || '';
      } else if (editForm.type === 'data') {
        // 仅在用户实际修改库存编辑框时提交，并附带打开时的库存快照；编辑期间库存被自动发货消费时服务端拒绝覆盖。
        if (editForm.data_content !== selectedCard.data_content) {
          updateData.data_content = editForm.data_content?.trim() || '';
          updateData.expected_data_content = selectedCard.data_content ?? '';
        }
      } else if (editForm.type === 'image') {
        updateData.image_url = editForm.library_image_id ? '' : (editForm.image_url?.trim() || '');
        updateData.library_image_id = editForm.library_image_id || 0;
      }
      // request 记录保存发起时的编辑会话，防止迟到保存关闭后来打开的弹窗。
      const request = editorRequest.current;
      await updateCard(selectedCard.id, updateData);
      if (editorRequest.current === request) closeEdit();
      await loadCards();
    } catch (/* error 表示卡密编辑请求异常。 */ error: unknown) {
      console.error('更新卡密失败:', error);
      alert(cardErrorMessage(error, '更新失败，请重试'));
    }
  }, [closeEdit, editError, editForm, editLoading, loadCards, selectedCard]);

  // handleDelete 删除指定卡密组并刷新库存。
  const handleDelete = useCallback(/* deleteAction 删除卡密组。 */ async (id: string | number) => {
    if (!confirm('确认删除该卡密吗？')) return;
    try {
      await deleteCard(id);
      await loadCards();
    } catch (/* error 表示卡密删除请求异常。 */ error: unknown) {
      console.error('删除卡密失败:', error);
      alert(cardErrorMessage(error, '删除失败，请重试'));
    }
  }, [loadCards]);

  // handleAddCard 校验新增表单、创建卡密组并刷新库存。
  const handleAddCard = useCallback(/* addAction 创建新的卡密组。 */ async () => {
    if (!addForm.name.trim()) {
      alert('请输入卡密名称');
      return;
    }
    if (!addForm.content.trim() && !(addForm.type === 'image' && addForm.library_image_id > 0)) {
      alert(addForm.type === 'api' ? '请输入 API 地址' : '请输入卡密内容');
      return;
    }
    try {
      // payload 保存新增卡密组的接口载荷。
      const payload: CardMutation = {
        name: addForm.name.trim(),
        type: addForm.type,
        description: addForm.description.trim(),
        enabled: addForm.enabled,
        delay_seconds: addForm.delay_seconds,
      };
      if (addForm.type === 'text') payload.text_content = addForm.content.trim();
      if (addForm.type === 'data') payload.data_content = addForm.content.trim();
      if (addForm.type === 'image') {
        payload.image_url = addForm.library_image_id ? '' : addForm.content.trim();
        payload.library_image_id = addForm.library_image_id;
      }
      if (addForm.type === 'api') {
        payload.api_config = apiConfigFromAddForm(addForm);
      }
      await createCard(payload);
      setShowAddModal(false);
      setAddForm(emptyAddForm());
      await loadCards();
    } catch (/* error 表示卡密创建请求异常。 */ error: unknown) {
      console.error('添加卡密失败:', error);
      alert(cardErrorMessage(error, '添加失败，请重试'));
    }
  }, [addForm, loadCards]);

  // toggleCardStatus 切换指定卡密组的启用状态并刷新库存。
  const toggleCardStatus = useCallback(/* toggleAction 切换卡密启用状态。 */ async (card: Card) => {
    try {
      // data_content 为 undefined 时请求 JSON 会省略库存字段，避免旧快照覆盖自动发货结果。
      await updateCard(card.id, { ...card, enabled: !card.enabled, data_content: undefined });
      await loadCards();
    } catch (/* error 表示卡密状态切换请求异常。 */ error: unknown) {
      alert(cardErrorMessage(error));
    }
  }, [loadCards]);

  // copyCardID 复制卡密组标识，剪贴板不可用时回退到提示框。
  const copyCardID = useCallback(/* copyAction 复制卡密组标识。 */ async (id: string | number) => {
    try {
      await navigator.clipboard.writeText(String(id));
      alert(`已复制卡密组ID：${id}`);
    } catch {
      prompt('复制卡密组ID', String(id));
    }
  }, []);

  // downloadCardTemplate 生成并下载卡密批量导入模板。
  const downloadCardTemplate = useCallback(/* downloadAction 下载卡密导入模板。 */ () => {
    // headers 保存模板列名。
    const headers = ['名称', '类型', '内容', '描述', '启用', '延迟秒', '多规格', '规格名', '规格值'];
    // rows 保存模板示例数据。
    const rows = [
      ['VIP月卡', 'data', 'VIP-MONTH-001\nVIP-MONTH-002\nVIP-MONTH-003', '按行消费的卡密队列', '是', '0', '否', '', ''],
      ['感谢文案', 'text', '感谢购买，如有问题联系客服～', '固定文本', '是', '0', '否', '', ''],
      ['教程图', 'image', 'https://cdn.example.com/tutorial.jpg', '图片URL', '是', '0', '否', '', ''],
    ];
    // csv 保存转义后的模板文本。
    const csv = [headers, ...rows]
      .map(/* csvRowMap 处理模板中的每一行。 */ row => row.map(/* csvCellMap 转义模板单元格。 */ cell => `"${String(cell).replace(/"/g, '""')}"`).join(','))
      .join('\n');
    // blob 保存生成的 CSV 文件对象。
    const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' });
    // url 保存模板文件的临时对象地址。
    const url = URL.createObjectURL(blob);
    // link 保存触发浏览器下载的临时链接。
    const link = document.createElement('a');
    link.href = url;
    link.download = '卡密组批量导入模板.csv';
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }, []);

  return {
    dataCards,
    filteredCards,
    typeFilter,
    setTypeFilter,
    nameSearch,
    setNameSearch,
    showEditModal,
    closeEdit,
    editLoading,
    editError,
    showAddModal,
    setShowAddModal,
    selectedCard,
    editForm,
    setEditForm,
    addForm,
    setAddForm,
    handleEdit,
    handleSaveEdit,
    handleDelete,
    handleAddCard,
    toggleCardStatus,
    copyCardID,
    downloadCardTemplate,
  };
};
