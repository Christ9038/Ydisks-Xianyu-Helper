import { Check,X } from 'lucide-react';

// ToastValue 描述轻提示的类型与文案；null 表示当前没有提示。
export interface ToastValue {
  // type 决定提示的语义颜色和图标。
  type: 'success' | 'error';
  // text 是要展示给用户的提示文案，不得包含账号凭证等敏感内容。
  text: string;
}

// ToastProps 描述轻提示组件需要渲染的提示内容。
export interface ToastProps {
  // toast 是当前需要展示的提示；为空时不渲染任何节点。
  toast: ToastValue | null;
}

// Toast 在页面底部居中展示短暂的操作反馈。
// 组件本身无状态，自动消失由调用方的定时器负责，便于在卸载时统一清理。
const Toast = ({ toast }: ToastProps) => {
  if (!toast) return null;
  return (
    <div className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-[10000] px-5 py-3 rounded-xl shadow-lg font-bold text-sm flex items-center gap-2 animate-fade-in text-white ${toast.type === 'success' ? 'bg-success-500' : 'bg-danger-500'}`} role="status">
      {toast.type === 'success' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}
      {toast.text}
    </div>
  );
};

export default Toast;
