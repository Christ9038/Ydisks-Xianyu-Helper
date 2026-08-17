import React from 'react';
import { AppErrorBoundary } from './app/errors/AppErrorBoundary';
import { SessionProvider } from './app/providers/SessionProvider';
import { AppRouter } from './app/router/AppRouter';

/** App 只装配全局 Provider、错误边界和路由，不保留任何领域页面或请求状态。 */
const App: React.FC = () => (
  <AppErrorBoundary>
    <SessionProvider>
      <AppRouter />
    </SessionProvider>
  </AppErrorBoundary>
);

export default App;
