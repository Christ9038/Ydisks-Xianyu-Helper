import React,{ createContext,useContext,useEffect,useState } from 'react';
import type { SessionResponse } from '../features/session/api';
import { initializeAdmin,login,logout,verifySession } from '../features/session/api';

// LoginCredentials 描述登录表单提交给会话服务的凭据字段。
export interface LoginCredentials {
  // username 表示管理员登录名。
  username: string;
  // password 表示管理员登录密码。
  password: string;
}

// SessionContextValue 描述认证状态和会话操作的统一 Provider 契约。
export interface SessionContextValue {
  // checkingAuth 表示首次会话校验是否尚未完成。
  checkingAuth: boolean;
  // isLoggedIn 表示当前浏览器会话是否已认证。
  isLoggedIn: boolean;
  // isAdmin 表示当前已认证用户是否为管理员。
  isAdmin: boolean;
  // needsInit 表示后端尚未完成首次管理员初始化。
  needsInit: boolean;
  // signIn 执行登录并在成功后更新认证状态。
  signIn: (credentials: LoginCredentials) => Promise<SessionResponse>;
  // initialize 执行首次管理员初始化并在成功后更新认证状态。
  initialize: (password: string) => Promise<SessionResponse>;
  // signOut 注销后端会话并清理本地认证状态。
  signOut: () => Promise<void>;
}

// SessionContext 保存认证状态，避免页面组件直接依赖会话 API 和全局事件。
const SessionContext = createContext<SessionContextValue | undefined>(undefined);

// SessionProvider 提供会话校验、认证操作和全局注销事件的统一生命周期边界。
export const SessionProvider: React.FC<React.PropsWithChildren> = ({ children }) => {
  // [isLoggedIn, setIsLoggedIn] 保存当前会话是否已经通过认证。
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  // [isAdmin, setIsAdmin] 保存当前会话的管理员权限。
  const [isAdmin, setIsAdmin] = useState(false);
  // [checkingAuth, setCheckingAuth] 保存首次会话校验是否仍在进行。
  const [checkingAuth, setCheckingAuth] = useState(true);
  // [needsInit, setNeedsInit] 保存系统是否需要首次管理员初始化。
  const [needsInit, setNeedsInit] = useState(false);

  // clearSession 将本地认证状态恢复为未登录且非管理员。
  const clearSession = () => {
    setIsLoggedIn(false);
    setIsAdmin(false);
  };

  useEffect(/* 当前回调管理会话校验请求和全局注销监听。 */ () => {
    // controller 负责取消组件卸载后的会话校验请求。
    const controller = new AbortController();
    // active 表示当前 Provider 是否仍然允许异步结果更新状态。
    let active = true;

    verifySession({ signal: controller.signal })
      .then(/* 当前回调处理会话校验结果。 */ response /* response 表示会话校验响应。 */ => {
        if (!active) return;
        if (response?.initialized === false) {
          setNeedsInit(true);
          clearSession();
          return;
        }

        setNeedsInit(false);
        if (response?.authenticated) {
          setIsLoggedIn(true);
          setIsAdmin(response.is_admin === true);
        } else {
          clearSession();
        }
      })
      .catch(/* 当前回调处理会话校验失败。 */ error /* error 表示会话校验错误。 */ => {
        if (!active || (error instanceof DOMException && error.name === 'AbortError')) return;
        setNeedsInit(false);
        clearSession();
      })
      .finally(/* 当前回调收束会话校验加载状态。 */ () => {
        if (active) setCheckingAuth(false);
      });

    // handleAuthLogoutEvent 响应其他页面发出的全局注销通知。
    const handleAuthLogoutEvent = () => clearSession();
    window.addEventListener('auth:logout', handleAuthLogoutEvent);

    return /* 当前回调清理会话校验和全局事件资源。 */ () => {
      active = false;
      controller.abort();
      window.removeEventListener('auth:logout', handleAuthLogoutEvent);
    };
  }, []);

  // signIn 调用登录接口，并只在服务端确认成功后更新认证状态。
  const signIn = async (credentials: LoginCredentials): Promise<SessionResponse> => {
    // response 表示登录接口返回的认证结果。
    const response = await login(credentials);
    if (response.success) {
      setNeedsInit(false);
      setIsLoggedIn(true);
      setIsAdmin(response.is_admin === true);
    }
    return response;
  };

  // initialize 调用首次初始化接口，并在成功后建立管理员会话。
  const initialize = async (password: string): Promise<SessionResponse> => {
    // response 表示首次初始化接口返回的认证结果。
    const response = await initializeAdmin(password);
    if (response.success) {
      setNeedsInit(false);
      setIsLoggedIn(true);
      setIsAdmin(response.is_admin === true);
    }
    return response;
  };

  // signOut 先请求后端注销，再无条件清理本地状态；接口失败会继续抛给界面记录。
  const signOut = async (): Promise<void> => {
    try {
      await logout();
    } finally {
      clearSession();
    }
  };

  return (
    <SessionContext.Provider value={{ checkingAuth, isLoggedIn, isAdmin, needsInit, signIn, initialize, signOut }}>
      {children}
    </SessionContext.Provider>
  );
};

// useSession 读取认证 Provider；缺少 Provider 时立即抛出装配错误。
export const useSession = (): SessionContextValue => {
  // context 表示当前组件树中装配的会话上下文值。
  const context = useContext(SessionContext);
  if (!context) {
    throw new Error('useSession 必须在 SessionProvider 内使用');
  }
  return context;
};
