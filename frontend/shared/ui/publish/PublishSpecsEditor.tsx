import { ImagePlus, Plus, Trash2 } from 'lucide-react';
import type { ReactNode } from 'react';
import { createPublishSpecId, reconcilePublishSkuRows, type PublishSkuRow, type PublishSpec } from './publishSpecs';

/** 闲鱼官方发布页提供的常用规格名称。 */
const standardSpecNames = ['颜色', '尺码', '容量', '份数', '大小', '高度', '总量'];

/** PublishSpecsEditorProps 描述多规格编辑器与发布表单之间的状态边界。 */
export interface PublishSpecsEditorProps {
  /** specs 是当前发布草稿的规格维度。 */
  specs: PublishSpec[];
  /** skuRows 是规格组合对应的价格和库存。 */
  skuRows: PublishSkuRow[];
  /** onChange 接收规格结构变化，并由父级保存表单与 SKU 行。 */
  onChange: (next: { specs: PublishSpec[]; skuRows: PublishSkuRow[] }) => void;
  /** renderValueImage 可选地替换规格值图片入口（例如从图片库选择）；缺省时使用本地文件上传。 */
  renderValueImage?: (value: PublishSpec['values'][number]) => ReactNode;
}

/** PublishSpecsEditor 按闲鱼官方逻辑编辑最多两种规格并维护组合 SKU 表。 */
export const PublishSpecsEditor = ({ specs, skuRows, onChange, renderValueImage }: PublishSpecsEditorProps) => {
  /** updateSpecs 写入规格草稿并按规格值变化重建 SKU 组合。 */
  const updateSpecs = (nextSpecs: PublishSpec[]) => onChange({ specs: nextSpecs, skuRows: reconcilePublishSkuRows(nextSpecs, skuRows) });

  /** addSpec 新增一个规格维度；第一维默认开启规格图片。 */
  const addSpec = () => {
    if (specs.length >= 2) return;
    // nextSpecs 保存新增规格维度后的草稿。
    const nextSpecs = [...specs, { specId: createPublishSpecId('spec'), name: '', supportImage: specs.length === 0, values: [{ valueId: createPublishSpecId('value'), value: '' }] }];
    updateSpecs(nextSpecs);
  };

  /** updateSpec 修改指定维度的名称、图片开关或值列表。 */
  const updateSpec = (specIndex: number, patch: Partial<PublishSpec>) => updateSpecs(specs.map((spec, index) => index === specIndex ? { ...spec, ...patch } : spec));

  /** updateSpecValue 修改指定维度下的一个规格值。 */
  const updateSpecValue = (specIndex: number, valueIndex: number, patch: Partial<PublishSpec['values'][number]>) => updateSpec(specIndex, { values: specs[specIndex].values.map(/* value、index 定位待修改的规格值。 */ (value, index) => index === valueIndex ? { ...value, ...patch } : value) });

  /** handleSpecValueKeyDown 在用户回车确认规格值后追加下一个输入框。 */
  const handleSpecValueKeyDown = (specIndex: number, valueIndex: number, key: string) => {
    if (key !== 'Enter') return;
    // spec 保存当前正在编辑的规格维度。
    const spec = specs[specIndex];
    // value 保存当前按下回车键的规格值。
    const value = spec.values[valueIndex];
    if (!value.value.trim() || valueIndex !== spec.values.length - 1) return;
    updateSpec(specIndex, { values: [...spec.values, { valueId: createPublishSpecId(`value-${valueIndex}`), value: '' }] });
  };

  /** removeSpecValue 删除一个规格值并保持至少一个输入框可继续录入。 */
  const removeSpecValue = (specIndex: number, valueIndex: number) => {
    // values 保存删除目标后的规格值列表。
    const values = specs[specIndex].values.filter(/* index 标识待判断的规格值位置。 */ (_, index) => index !== valueIndex);
    updateSpec(specIndex, { values: values.length ? values : [{ valueId: createPublishSpecId('value'), value: '' }] });
  };

  /** removeSpec 删除整个规格维度并同步移除不再存在的 SKU 组合。 */
  const removeSpec = (specIndex: number) => updateSpecs(specs.filter((_, index) => index !== specIndex));

  /** updateSkuRow 写入一行 SKU 的价格或库存。 */
  const updateSkuRow = (rowIndex: number, patch: Partial<PublishSkuRow>) => onChange({ specs, skuRows: skuRows.map(/* row、index 定位待修改的 SKU 行。 */ (row, index) => index === rowIndex ? { ...row, ...patch } : row) });

  return (
    <section className="space-y-3" aria-labelledby="publish-specs-title">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h4 id="publish-specs-title" className="text-sm font-extrabold text-gray-900">商品规格</h4>
          <p className="mt-1 text-xs leading-5 text-gray-500">最多添加 2 个规格类型；每种规格至少填写 2 个值，系统会自动生成全部组合。</p>
        </div>
        <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-bold text-slate-500">{specs.length}/2</span>
      </div>

      {specs.map(/* spec、specIndex 渲染一个规格维度编辑卡片。 */ (spec, specIndex) => (
        <div key={spec.specId} className="rounded-2xl bg-slate-100 p-4 shadow-sm">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <span className="text-lg font-black leading-none text-slate-300" aria-hidden="true">⋮⋮</span>
            <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center">
              <select aria-label={`第 ${specIndex + 1} 个规格类型`} className="ios-input min-w-0 flex-1 rounded-xl !border-0 !bg-white px-3 py-2.5 !text-slate-900 shadow-sm focus:!border-0 focus:ring-2 focus:ring-emerald-100" value={spec.customName ? '__custom__' : spec.name} onChange={/* event 携带用户选择的规格类型。 */ event => {
                // nextName 保存用户刚选择或输入的规格名称。
                const nextName = event.target.value;
                updateSpec(specIndex, nextName === '__custom__' ? { customName: true, name: '' } : { customName: false, name: nextName });
              }}>
                <option value="">请选择规格类型</option>
                {standardSpecNames.filter(/* name 保留未被其他维度占用的官方规格名。 */ name => !specs.some(/* otherSpec、otherIndex 检查规格名是否重复。 */ (otherSpec, otherIndex) => otherIndex !== specIndex && otherSpec.name === name)).map(/* name 渲染一个官方规格选项。 */ name => <option key={name} value={name}>{name}</option>)}
                <option value="__custom__">无合适选项？输入自定义类型</option>
              </select>
              {spec.customName && <input aria-label={`第 ${specIndex + 1} 个自定义规格类型`} className="ios-input min-w-0 flex-1 rounded-xl !border-0 !bg-white px-3 py-2.5 !text-slate-900 !placeholder:text-slate-500 shadow-sm focus:!border-0 focus:ring-2 focus:ring-emerald-100" placeholder="输入自定义类型" value={spec.name} onChange={/* event 携带自定义规格名称变化。 */ event => updateSpec(specIndex, { name: event.target.value })} />}
            </div>
            <label className="flex shrink-0 items-center gap-2 text-xs font-bold text-slate-600">
              <input type="checkbox" checked={spec.supportImage} onChange={/* event 携带规格图片开关的新状态。 */ event => updateSpec(specIndex, { supportImage: event.target.checked })} />
              支持规格图片
            </label>
            <button type="button" aria-label={`删除第 ${specIndex + 1} 个规格类型`} className="self-end rounded-lg p-2 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-500 sm:self-auto" onClick={/* 删除当前规格维度并重建组合。 */ () => removeSpec(specIndex)}>
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
          <div className="mt-3 grid gap-2 sm:grid-cols-2">
            {spec.values.map(/* value、valueIndex 渲染一个规格值输入行。 */ (value, valueIndex) => (
              <div key={value.valueId} className="flex items-center gap-2">
                <input aria-label={`${spec.name || `第 ${specIndex + 1} 个规格`}第 ${valueIndex + 1} 个规格值`} className="ios-input min-w-0 flex-1 rounded-xl !border-0 !bg-white px-3 py-2.5 !text-slate-900 !placeholder:text-slate-500 shadow-sm focus:!border-0 focus:ring-2 focus:ring-emerald-100" placeholder={`请输入具体的${spec.name || '规格值'}`} value={value.value} onChange={/* event 携带规格值输入变化。 */ event => updateSpecValue(specIndex, valueIndex, { value: event.target.value })} onKeyDown={/* event 携带规格值输入框的按键事件。 */ event => handleSpecValueKeyDown(specIndex, valueIndex, event.key)} />
                {spec.supportImage && renderValueImage && renderValueImage(value)}
                {spec.supportImage && !renderValueImage && <label className={`flex h-10 w-10 shrink-0 cursor-pointer items-center justify-center rounded-xl border ${value.image ? 'border-emerald-300 bg-emerald-50 text-emerald-600' : 'border-slate-200 bg-white text-slate-400'} hover:border-emerald-300`} title={value.image ? `已选择 ${value.image.name}` : '上传规格图片'}>
                  <ImagePlus className="h-4 w-4" />
                  <input className="hidden" type="file" accept="image/*" onChange={/* event 携带规格值图片文件选择结果。 */ event => updateSpecValue(specIndex, valueIndex, { image: event.target.files?.[0] || null })} />
                </label>}
                <button type="button" aria-label={`删除${spec.name || '规格'}值 ${value.value || valueIndex + 1}`} className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-500" onClick={/* 删除当前规格值并重建组合。 */ () => removeSpecValue(specIndex, valueIndex)}>
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            ))}
          </div>
        </div>
      ))}

      <button type="button" disabled={specs.length >= 2} className="flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-slate-300 bg-white px-4 py-3 text-sm font-extrabold text-slate-600 transition-colors hover:border-emerald-300 hover:bg-emerald-50 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-50" onClick={addSpec}>
        <Plus className="h-4 w-4" />添加规格类型（{specs.length}/2）
      </button>

      {specs.length > 0 && <div className="overflow-x-auto rounded-2xl border border-slate-200 bg-white">
        <table className="min-w-full text-left text-sm">
          <thead className="bg-slate-50 text-xs font-extrabold text-slate-500">
            <tr>{specs.map(/* spec 渲染 SKU 表头中的一个规格维度。 */ spec => <th key={spec.specId} className="px-4 py-3">{spec.name || '规格'}</th>)}<th className="min-w-40 px-4 py-3">价格</th><th className="min-w-40 px-4 py-3">库存</th></tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {skuRows.map(/* row、rowIndex 渲染一个 SKU 组合行。 */ (row, rowIndex) => <tr key={row.rowKey}>
              {row.valueLabels.map(/* label、valueIndex 渲染当前组合的规格值单元格。 */ (label, valueIndex) => <td key={`${row.rowKey}-${valueIndex}`} className="whitespace-nowrap px-4 py-3 font-bold text-slate-700">{label}</td>)}
              <td className="px-4 py-2"><div className="flex items-center gap-1"><span className="font-bold text-slate-700">¥</span><input aria-label={`${row.valueLabels.join(' ')} 价格`} className="ios-input w-full rounded-xl !border-0 !bg-white px-3 py-2 !text-slate-900 !placeholder:text-slate-500 shadow-sm focus:!border-0 focus:ring-2 focus:ring-emerald-100" inputMode="decimal" placeholder="0.00" value={row.price} onChange={/* event 携带 SKU 售价变化。 */ event => updateSkuRow(rowIndex, { price: event.target.value })} /></div></td>
              <td className="px-4 py-2"><input aria-label={`${row.valueLabels.join(' ')} 库存`} className="ios-input w-full rounded-xl !border-0 !bg-white px-3 py-2 !text-slate-900 !placeholder:text-slate-500 shadow-sm focus:!border-0 focus:ring-2 focus:ring-emerald-100" inputMode="numeric" min="0" placeholder="0" value={row.quantity} onChange={/* event 携带 SKU 库存变化。 */ event => updateSkuRow(rowIndex, { quantity: event.target.value })} /></td>
            </tr>)}
          </tbody>
        </table>
        {specs.length > 0 && skuRows.length === 0 && <p className="px-4 py-4 text-xs text-amber-700">请先填写每种规格的规格值，组合表会自动出现。</p>}
      </div>}
    </section>
  );
};
