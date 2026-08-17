import React from 'react';

/** ErrorBoundaryProps 描述顶层错误边界可恢复的页面子树。 */
interface ErrorBoundaryProps {
  /** children 是由错误边界保护的整个应用路由树。 */
  children: React.ReactNode;
}

/** ErrorBoundaryState 只记录渲染期故障，避免把错误对象或敏感响应写入浏览器状态。 */
interface ErrorBoundaryState {
  /** hasError 表示应用子树是否因渲染错误而被隔离。 */
  hasError: boolean;
}

/** AppErrorBoundary 隔离未捕获的页面渲染错误，并提供不依赖网络请求的恢复入口。 */
export class AppErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  /** state 保存当前子树的错误隔离状态。 */
  public state: ErrorBoundaryState = { hasError: false };

  /** getDerivedStateFromError 在 React 捕获到子树异常时切换到静态恢复视图。 */
  public static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  /** handleReload 由用户点击触发，使用完整刷新重新建立会话和路由运行环境。 */
  private handleReload = (): void => {
    window.location.reload();
  };

  /** render 在正常路径渲染应用子树；捕获异常后只暴露安全的恢复界面。 */
  public render(): React.ReactNode {
    // hasError 表示当前是否需要显示错误隔离界面。
    const { hasError } = this.state;
    // children 是未发生渲染错误时应继续展示的应用路由树。
    const { children } = this.props;
    if (!hasError) return children;

    return (
      <main className="flex min-h-screen items-center justify-center bg-canvas p-6 text-ink">
        <section className="max-w-md text-center">
          <h1 className="text-xl font-semibold">页面暂时无法显示</h1>
          <button className="ios-btn-primary mt-6 px-4 py-2" type="button" onClick={this.handleReload}>
            重新加载
          </button>
        </section>
      </main>
    );
  }
}
