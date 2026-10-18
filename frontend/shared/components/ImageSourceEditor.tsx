import { useId } from 'react';
import { imageSource, type ImageSourceDraft } from '../imageSource';

/** ImageSourceEditorProps 不依赖任何 feature，只编辑图片引用。 */
interface ImageSourceEditorProps {
  /** value 是独立表单中的图片来源草稿。 */
  value: ImageSourceDraft;
  /** onChange 返回完整来源字段，切换时同时清空两种旧输入。 */
  onChange: (value: ImageSourceDraft) => void;
}

/** ImageSourceEditor 共用 URL/服务器路径控件，不上传浏览器文件，也不请求图片预览。 */
export function ImageSourceEditor({ value, onChange }: ImageSourceEditorProps) {
  // helpID 让多消息编辑器中的各个路径输入独立关联说明。
  const helpID = useId();
  // source 保留用户选择但尚未输入路径的状态。
  const source = imageSource(value);
  return <div className="space-y-3">
    <label className="block text-sm font-bold text-gray-700">
      图片来源
      <select value={source} onChange={/* event 切换来源并丢弃旧输入，禁止两个来源并存。 */ event => onChange({ image_source: event.target.value as 'url' | 'local', image_url: '', image_path: '' })} className="w-full ios-input px-4 py-3 rounded-xl mt-2">
        <option value="url">图片 URL</option><option value="local">服务器本地图片</option>
      </select>
    </label>
    {source === 'url' ? <label className="block text-sm font-bold text-gray-700">
      图片 URL
      <input type="url" value={value.image_url || ''} onChange={/* event 仅更新远程图片引用，不发起浏览器预览请求。 */ event => onChange({ image_source: 'url', image_url: event.target.value, image_path: '' })} placeholder="https://example.com/image.jpg" className="w-full ios-input px-4 py-3 rounded-xl mt-2" />
    </label> : <>
      <label className="block text-sm font-bold text-gray-700">
        本地图片相对路径
        <input type="text" value={value.image_path || ''} onChange={/* event 只保存程序所在机器的文件引用。 */ event => onChange({ image_source: 'local', image_url: '', image_path: event.target.value })} placeholder="product-a/guide.jpg" aria-describedby={helpID} className="w-full ios-input px-4 py-3 rounded-xl mt-2" />
      </label>
      <div id={helpID} className="space-y-1 text-xs leading-5 text-gray-500 break-all">
        <p>把图片放到运行程序机器的 <code>XIANYU_UPLOAD_DIR/reply-images/&lt;实际发货账号&gt;/</code> 内，默认目录为 <code>data/uploads/reply-images/&lt;实际发货账号&gt;/</code>。只填写目录内相对路径，使用 / 分隔；Docker 需挂载该目录。这不是浏览器上传，也不是选择浏览器电脑的文件。</p>
        <p>跨账号复用模板或卡密时，各实际发货账号目录内都需要放置同名相对图片。每次发货读取 PNG、JPEG 或 GIF 图片，最大 10 MiB。</p>
      </div>
    </>}
  </div>;
}
