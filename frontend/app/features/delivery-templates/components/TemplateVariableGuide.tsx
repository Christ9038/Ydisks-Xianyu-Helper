/** TemplateVariableGuide 说明文本消息的变量语法，与图片消息编辑控件一起按需加载。 */
export function TemplateVariableGuide() {
  return (
    <aside className="space-y-4 rounded-2xl border border-sky-100 bg-sky-50/60 p-4" aria-labelledby="delivery-template-variable-guide">
      <div>
        <p className="text-[11px] font-black uppercase tracking-[0.18em] text-sky-600">Placeholder guide</p>
        <h3 id="delivery-template-variable-guide" className="mt-1 text-base font-black text-sky-950">变量和占位符</h3>
      </div>
      <div className="rounded-xl border border-sky-100 bg-white/80 p-3">
        <p className="text-xs font-bold text-gray-700">内置、卡密、自定义变量均用双大括号</p>
        <p className="mt-2 text-xs leading-5 text-gray-600">直接写变量名即可，例如 <code className="rounded bg-white px-1 py-0.5 font-mono text-[11px] text-sky-700">{'{{order_id}}'}</code>；保存后系统会校验拼写。不支持空格、中文变量名或未闭合的大括号。</p>
      </div>
      <div className="space-y-2 rounded-xl border border-sky-100 bg-white/80 p-3 text-xs leading-5 text-gray-700">
        <p><code className="font-mono text-sky-700">{'{{buyer_nickname}}'}</code>：购买用户昵称。</p>
        <p><code className="font-mono text-sky-700">{'{{order_id}}'}</code>：订单号。</p>
        <p><code className="font-mono text-sky-700">{'{{buyer_id}}'}</code>：买家 ID。</p>
        <p><code className="font-mono text-sky-700">{'{{card_name}}'}</code>：当前模板绑定的卡密库存名称。</p>
        <p><code className="font-mono text-sky-700">{'{{cards.<变量名>}}'}</code>：卡密内容；变量名只能使用英文字母、数字、下划线或短横线。</p>
        <p><code className="font-mono text-sky-700">{'{{custom.<变量名>}}'}</code>：发货规则传入的自定义字符串；变量名与规则页 key 对应。</p>
      </div>
      <div className="space-y-2 text-xs leading-5 text-gray-600">
        <p className="font-bold text-gray-800">如何声明和使用</p>
        <ol className="list-decimal space-y-1.5 pl-5">
          <li>直接在消息正文中写入占位符，例如 <code className="rounded bg-white px-1 py-0.5 font-mono text-[11px] text-sky-700">{'{{order_id}}'}</code>，不用填写 <code>delivery.</code> 前缀。</li>
          <li>保存模板后，在自动化规则中选择该模板。</li>
          <li>模板中的卡密变量需要在规则页分别绑定库存；自定义变量需要在规则页填写对应的 key 和字符串 value。</li>
          <li>发货时系统会替换订单、库存、卡密和自定义字符串。</li>
        </ol>
      </div>
      <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-900">
        <p className="font-bold">示例</p>
        <pre className="mt-2 whitespace-pre-wrap font-mono text-[11px]">{'感谢购买！\n订单：{{order_id}}\n主卡：{{cards.main}}\n备注：{{custom.remark}}'}</pre>
        <p className="mt-2">上例会要求在规则页绑定 <code className="font-mono">main</code> 卡密库存，并填写 <code className="font-mono">remark</code> 对应的字符串。</p>
      </div>
      <p className="text-[11px] leading-5 text-gray-500">买家昵称缺失时替换为空字符串；模板变量仅用于文本消息。图片不参与变量解析，图片卡密不能绑定文本模板变量。</p>
    </aside>
  );
}
