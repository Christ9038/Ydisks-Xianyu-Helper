import { Check, ChevronDown, Search, X } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { Item } from '../models';

// ITEM_MULTI_SELECT_DEBOUNCE_MS 是关联商品下拉搜索的防抖等待时长（毫秒）。
// 商品候选完全来自本地已加载列表，防抖只用于抑制高频重渲染，不触发任何网络请求。
const ITEM_MULTI_SELECT_DEBOUNCE_MS = 300;

// ItemMultiSelectProps 描述关联商品多选下拉框的候选数据、选中集合和变更回调。
export interface ItemMultiSelectProps {
  // options 是当前账号已同步到本地的商品候选列表，不做远程搜索。
  options: Item[];
  // value 是当前已选中的商品标识集合，空集合表示账号级回复。
  value: string[];
  // onChange 在用户增删商品后回传新的商品标识集合。
  onChange: (itemIDs: string[]) => void;
  // disabled 表示控件是否只读；只读时不可展开、不可增删。
  disabled?: boolean;
  // placeholder 是没有任何选中项时展示的占位文案。
  placeholder?: string;
}

// ItemMultiSelect 提供支持本地搜索的关联商品多选下拉框。
// 多选过程中下拉保持展开，只有点击组件外部才会关闭，搜索框内的点击同样不关闭。
const ItemMultiSelect = ({ options, value, onChange, disabled = false, placeholder = '账号级回复（不限定商品）' }: ItemMultiSelectProps) => {
  // open 表示下拉面板是否展开。
  const [open, setOpen] = useState(false);
  // searchInput 是用户最近一次键入的搜索文本，用于驱动防抖计时。
  const [searchInput, setSearchInput] = useState('');
  // debouncedSearch 是防抖后真正参与过滤的搜索文本。
  const [debouncedSearch, setDebouncedSearch] = useState('');
  // containerRef 保存下拉组件根节点，用于判断点击是否发生在组件外部。
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(/* 当前副作用在搜索文本变化后延迟同步防抖值并在卸载或再次输入时清理计时器。 */ () => {
    // timer 是本次搜索文本变化对应的防抖计时器。
    const timer = window.setTimeout(/* 当前回调把防抖后的搜索文本写入过滤状态。 */ () => {
      setDebouncedSearch(searchInput.trim());
    }, ITEM_MULTI_SELECT_DEBOUNCE_MS);
    return /* 当前回调清理尚未触发的防抖计时器。 */ () => window.clearTimeout(timer);
  }, [searchInput]);

  useEffect(/* 当前副作用在组件挂载期间监听全局鼠标按下事件以判定外部点击。 */ () => {
    // handlePointerDown 判断点击是否落在组件外部，仅在外部点击时收起下拉。
    const handlePointerDown = (event: MouseEvent) => {
      // target 是本次鼠标按下的目标节点。
      const target = event.target as Node | null;
      if (!target || !containerRef.current) return;
      // 组件内部的点击（含搜索框、选项、已选标签）一律不关闭下拉。
      if (containerRef.current.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handlePointerDown);
    return /* 当前回调在组件卸载时移除全局鼠标按下监听。 */ () => document.removeEventListener('mousedown', handlePointerDown);
  }, []);

  // collapsed 表示下拉收起时应清空搜索词，避免下次展开残留旧过滤条件。
  useEffect(/* 当前副作用在收起下拉时复位搜索状态。 */ () => {
    if (open) return;
    setSearchInput('');
    setDebouncedSearch('');
  }, [open]);

  // optionIndex 保存商品标识到商品候选的映射，用于把已选标识还原成可读标题。
  const optionIndex = useMemo(/* 当前回调按商品标识建立候选索引。 */ () => {
    // index 是本次构建的商品标识到候选的映射。
    const index = new Map<string, Item>();
    // option 表示当前待登记的商品候选。
    for (const /* option 是当前待登记标题索引的本地商品候选。 */ option of options) {
      // itemID 是去除首尾空白后的商品标识。
      const itemID = option.item_id?.trim();
      if (itemID && !index.has(itemID)) index.set(itemID, option);
    }
    return index;
  }, [options]);

  // filteredOptions 保存按防抖搜索文本在本地过滤后的商品候选。
  const filteredOptions = useMemo(/* 当前回调在本地商品列表上执行大小写不敏感匹配。 */ () => {
    // keyword 是归一化后的本地搜索关键词。
    const keyword = debouncedSearch.toLowerCase();
    if (!keyword) return options;
    return options.filter(/* 当前回调按商品标题或商品标识匹配关键词。 */ option => {
      // title 是商品的标题文本，缺失时回退商品标识。
      const title = (option.item_title || option.item_id || '').toLowerCase();
      return title.includes(keyword) || (option.item_id || '').toLowerCase().includes(keyword);
    });
  }, [debouncedSearch, options]);

  // selectedIDs 保存去重且按 value 顺序排列的已选商品标识，避免渲染重复标签。
  const selectedIDs = useMemo(/* 当前回调对已选商品标识去重。 */ () => {
    // result 是去重后的已选商品标识集合。
    const result: string[] = [];
    // candidate 表示当前待判定的已选商品标识。
    for (const /* candidate 是当前待去重规整的已选商品标识。 */ candidate of value) {
      // itemID 是去除首尾空白后的商品标识。
      const itemID = typeof candidate === 'string' ? candidate.trim() : '';
      if (itemID && !result.includes(itemID)) result.push(itemID);
    }
    return result;
  }, [value]);

  // toggleItem 在选中集合中增删单个商品标识。
  const toggleItem = (itemID: string) => {
    if (disabled) return;
    if (selectedIDs.includes(itemID)) {
      onChange(selectedIDs.filter(/* 当前回调剔除被取消选择的商品标识。 */ id => id !== itemID));
      return;
    }
    onChange([...selectedIDs, itemID]);
  };

  // removeItem 移除单个已选商品标签。
  const removeItem = (itemID: string) => {
    if (disabled) return;
    onChange(selectedIDs.filter(/* 当前回调剔除被移除标签的商品标识。 */ id => id !== itemID));
  };

  // itemTitle 返回商品的可读标题，缺失时回退商品标识。
  const itemTitle = (itemID: string): string => optionIndex.get(itemID)?.item_title || itemID;

  return (
    <div ref={containerRef} className="relative">
      <div
        role="button"
        tabIndex={0}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={/* 当前回调在只读时忽略点击，否则切换下拉展开状态。 */ () => {
          if (disabled) return;
          setOpen(/* 当前回调基于上一次展开状态取反。 */ current => !current);
        }}
        onKeyDown={/* 当前回调支持键盘展开下拉面板。 */ event => {
          if (disabled) return;
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            setOpen(/* 当前回调基于上一次展开状态取反。 */ current => !current);
          }
        }}
        className={`w-full ios-input px-4 py-3 rounded-xl flex items-center justify-between gap-2 text-left ${disabled ? 'opacity-60 cursor-not-allowed' : 'cursor-pointer'}`}
      >
        <div className="flex flex-1 flex-wrap items-center gap-2 min-w-0">
          {selectedIDs.length === 0 ? (
            <span className="text-gray-400">{placeholder}</span>
          ) : (
            selectedIDs.map(/* 当前回调渲染单个已选商品标签。 */ itemID => (
              <span key={itemID} className="inline-flex max-w-full items-center gap-1 rounded-lg bg-black px-2.5 py-1 text-xs font-bold text-white">
                <span className="truncate">{itemTitle(itemID)}</span>
                <button
                  type="button"
                  aria-label={`移除 ${itemTitle(itemID)}`}
                  onClick={/* 当前回调移除该已选商品且不冒泡触发展开切换。 */ event => {
                    event.stopPropagation();
                    removeItem(itemID);
                  }}
                  className="shrink-0 rounded-full hover:bg-white/20"
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))
          )}
        </div>
        <ChevronDown className={`h-4 w-4 shrink-0 text-gray-400 transition-transform ${open ? 'rotate-180' : ''}`} />
      </div>

      {open && (
        <div className="absolute z-[10001] mt-2 w-full rounded-2xl border border-gray-100 bg-white p-3 shadow-xl">
          <div className="relative mb-2">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
            <input
              type="text"
              value={searchInput}
              onChange={/* 当前回调记录搜索输入，实际过滤由防抖后的值驱动。 */ event => setSearchInput(event.target.value)}
              placeholder="搜索本地商品标题或商品 ID"
              className="w-full ios-input rounded-xl py-2.5 pl-9 pr-3 text-sm"
              autoFocus
            />
          </div>
          <div className="max-h-60 overflow-y-auto" role="listbox" aria-multiselectable="true">
            {filteredOptions.length === 0 ? (
              <div className="px-2 py-6 text-center text-sm text-gray-400">没有匹配的本地商品</div>
            ) : (
              filteredOptions.map(/* 当前回调渲染单个商品候选。 */ option => {
                // itemID 是当前候选去除首尾空白后的商品标识，必须与 selectedIDs 的规整方式一致。
                const itemID = option.item_id.trim();
                // selected 表示当前候选是否已被选中。
                const selected = selectedIDs.includes(itemID);
                return (
                  <button
                    key={`${option.cookie_id}-${itemID}`}
                    type="button"
                    role="option"
                    aria-selected={selected}
                    onClick={/* 当前回调切换该商品的选中状态，多选时不关闭下拉。 */ () => toggleItem(itemID)}
                    className={`flex w-full items-center justify-between gap-2 rounded-xl px-3 py-2 text-left text-sm transition-colors ${selected ? 'bg-gray-100 font-bold text-gray-900' : 'text-gray-700 hover:bg-gray-50'}`}
                  >
                    <span className="truncate">{option.item_title || itemID}</span>
                    {selected ? <Check className="h-4 w-4 shrink-0 text-emerald-600" /> : null}
                  </button>
                );
              })
            )}
          </div>
          {selectedIDs.length > 0 && (
            <div className="mt-2 flex items-center justify-between border-t border-gray-100 pt-2 text-xs text-gray-500">
              <span>已选 {selectedIDs.length} 个商品</span>
              <button
                type="button"
                onClick={/* 当前回调清空全部已选商品。 */ () => onChange([])}
                className="rounded-lg px-2 py-1 font-bold text-red-500 hover:bg-red-50"
              >
                清空
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default ItemMultiSelect;
