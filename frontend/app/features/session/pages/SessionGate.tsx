import React, { useState } from 'react';
import { ArrowRight, Loader2, Lock, ShieldCheck, User } from 'lucide-react';
import { YdisksBrandIcon } from '../../../../shared/ui/YdisksLogo';
import { useSession } from '../../../providers/SessionProvider';

/** SessionGate 在会话校验完成前显示加载状态，并承载首次初始化和管理员登录表单。 */
export const SessionGate: React.FC = () => {
  // checkingAuth 与 needsInit 是 Provider 维护的会话服务端状态，页面不自行请求认证接口。
  const { checkingAuth, needsInit, signIn, initialize } = useSession();
  // username 与 password 是登录表单短暂状态，永不写入持久化存储。
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  // initialPassword 与 initialPasswordConfirm 是首次初始化时一次性提交的秘密输入。
  const [initialPassword, setInitialPassword] = useState('');
  const [initialPasswordConfirm, setInitialPasswordConfirm] = useState('');
  // isSubmitting 区分当前正在提交的表单，防止重复发起认证或初始化请求。
  const [isSubmitting, setIsSubmitting] = useState(false);
  // formError 是仅用于当前界面显示的通用失败信息，不保存接口响应载荷。
  const [formError, setFormError] = useState('');

  /** handleLogin 在管理员明确提交后调用 Provider 登录，并在失败时展示安全错误文本。 */
  const handleLogin = async (event: React.FormEvent<HTMLFormElement>): Promise<void> => {
    event.preventDefault();
    setIsSubmitting(true);
    setFormError('');
    try {
      // response 是服务端登录契约，只使用其公开的成功和消息字段。
      const response = await signIn({ username, password });
      if (!response.success) setFormError(response.message || '登录失败');
    } catch (error /* error 是认证请求失败原因，不能包含或重显密码。 */) {
      setFormError(error instanceof Error ? error.message || '登录失败' : '登录失败');
    } finally {
      setIsSubmitting(false);
    }
  };

  /** handleInitialize 校验两次管理员密码后提交首次初始化，并在成功后立即清空输入。 */
  const handleInitialize = async (event: React.FormEvent<HTMLFormElement>): Promise<void> => {
    event.preventDefault();
    setFormError('');
    if (initialPassword.length < 8) {
      setFormError('密码至少需要 8 个字符');
      return;
    }
    if (initialPassword !== initialPasswordConfirm) {
      setFormError('两次输入的密码不一致');
      return;
    }

    setIsSubmitting(true);
    try {
      // response 是首次初始化接口返回的公开认证结果。
      const response = await initialize(initialPassword);
      if (!response.success) {
        setFormError(response.message || '初始化失败，请重试');
        return;
      }
      setInitialPassword('');
      setInitialPasswordConfirm('');
    } catch (error /* error 是初始化请求失败原因，不输出用户输入的密码。 */) {
      setFormError(error instanceof Error ? error.message || '初始化失败，请重试' : '初始化失败，请重试');
    } finally {
      setIsSubmitting(false);
    }
  };

  if (checkingAuth) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-surface" aria-label="正在校验会话">
        <Loader2 className="h-8 w-8 animate-spin text-brand" />
      </main>
    );
  }

  // heading 是当前认证流程的页面标题，保证初始化和登录表单使用一致视觉壳。
  const heading = needsInit ? '首次设置管理员密码' : '欢迎回来';
  // description 是当前认证流程的辅助说明，不泄露任何会话或账户敏感数据。
  const description = needsInit ? '设置完成后会自动进入系统，管理员账号为 admin。' : 'Ydisks闲鱼助手 · 自动发货与管家系统';

  return (
    <main className="flex min-h-screen items-center justify-center bg-canvas p-4 font-sans">
      <section className="w-full max-w-lg rounded-xl border border-white bg-white/80 p-8 shadow-panel backdrop-blur-3xl md:p-12">
        <header className="mb-8 text-center">
          <div className="mx-auto mb-6 flex justify-center">
            <YdisksBrandIcon sizeClass="h-24 w-24" />
          </div>
          <h1 className="text-3xl font-extrabold text-gray-900">{heading}</h1>
          <p className="mt-2 font-medium text-gray-600">{description}</p>
        </header>

        {needsInit ? (
          <form className="space-y-5" onSubmit={handleInitialize}>
            <label className="relative block">
              <span className="sr-only">设置管理员密码</span>
              <Lock className="absolute left-5 top-1/2 h-5 w-5 -translate-y-1/2 text-gray-400" />
              <input
                autoFocus
                className="ios-input h-14 w-full rounded-xl py-4 pl-14 pr-6"
                onChange={/* initialPasswordChange 仅将本次密码输入保存在短暂表单状态。 */ (event /* event 是密码输入框的最新用户编辑事件。 */) => setInitialPassword(event.target.value)}
                placeholder="设置管理员密码（至少 8 个字符）"
                type="password"
                value={initialPassword}
              />
            </label>
            <label className="relative block">
              <span className="sr-only">确认管理员密码</span>
              <ShieldCheck className="absolute left-5 top-1/2 h-5 w-5 -translate-y-1/2 text-gray-400" />
              <input
                className="ios-input h-14 w-full rounded-xl py-4 pl-14 pr-6"
                onChange={/* initialPasswordConfirmChange 仅更新用于前端一致性校验的确认输入。 */ (event /* event 是确认密码输入框的最新用户编辑事件。 */) => setInitialPasswordConfirm(event.target.value)}
                placeholder="再次输入密码"
                type="password"
                value={initialPasswordConfirm}
              />
            </label>
            <SubmitButton submitting={isSubmitting} text="设置密码并进入系统" />
          </form>
        ) : (
          <form className="space-y-5" onSubmit={handleLogin}>
            <label className="relative block">
              <span className="sr-only">管理员账号</span>
              <User className="absolute left-5 top-1/2 h-5 w-5 -translate-y-1/2 text-gray-400" />
              <input
                autoFocus
                className="ios-input h-14 w-full rounded-xl py-4 pl-14 pr-6"
                onChange={/* usernameChange 保存管理员主动输入的登录名称。 */ (event /* event 是管理员账号输入框的最新用户编辑事件。 */) => setUsername(event.target.value)}
                placeholder="管理员账号"
                type="text"
                value={username}
              />
            </label>
            <label className="relative block">
              <span className="sr-only">管理员密码</span>
              <Lock className="absolute left-5 top-1/2 h-5 w-5 -translate-y-1/2 text-gray-400" />
              <input
                className="ios-input h-14 w-full rounded-xl py-4 pl-14 pr-6"
                onChange={/* passwordChange 只在当前表单生命周期中保存管理员密码。 */ (event /* event 是管理员密码输入框的最新用户编辑事件。 */) => setPassword(event.target.value)}
                placeholder="密码"
                type="password"
                value={password}
              />
            </label>
            <SubmitButton submitting={isSubmitting} text="立即登录" />
          </form>
        )}

        {formError ? <p className="mt-5 rounded-xl bg-red-50 p-3 text-center text-sm font-medium text-red-600">{formError}</p> : null}
      </section>
    </main>
  );
};

/** SubmitButton 为认证表单提供固定尺寸的提交控件，避免加载状态改变布局。 */
const SubmitButton: React.FC<{ /** submitting 表示认证请求是否仍在执行。 */ submitting: boolean; /** text 是按钮在空闲时显示的操作名称。 */ text: string }> = ({ submitting, text }) => (
  <button className="ios-btn-primary flex h-14 w-full items-center justify-center gap-2 rounded-xl text-lg disabled:opacity-70" disabled={submitting} type="submit">
    {submitting ? <Loader2 className="h-5 w-5 animate-spin" /> : <><span>{text}</span><ArrowRight className="h-5 w-5" /></>}
  </button>
);
