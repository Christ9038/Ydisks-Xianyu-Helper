import React from 'react';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import APIAccessTokensSection from '../components/APIAccessTokensSection';

/** APIAccessTokensPage 为当前登录用户提供独立的外部 API 令牌管理页面。 */
const APIAccessTokensPage: React.FC = () => (
  <div className="min-w-0 space-y-8 animate-fade-in">
    <PageHeader title="外部 API 管理" description="为外部 Agent、自动化程序和第三方系统签发最小权限访问令牌" />
    <APIAccessTokensSection />
  </div>
);

export default APIAccessTokensPage;
