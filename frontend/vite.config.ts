import path from 'path';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  base: '/static/',
  server: {
    port: 3000,
    host: '0.0.0.0',
    proxy: {
      // 代理API请求到后端
      '/api': {
        target: 'http://localhost:59188',
        changeOrigin: true,
		ws: true,
      },
      '/health': {
        target: 'http://localhost:59188',
        changeOrigin: true,
      },
    },
  },
  plugins: [react()],
  test: {
    environment: 'node',
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      reportsDirectory: './coverage',
      include: ['**/*.{ts,tsx}'],
      exclude: [
        '**/*.test.{ts,tsx}',
        '**/node_modules/**',
        '**/dist/**',
        '**/scripts/**',
        '**/vite.config.ts',
        // 纯 UI 组件不属于本项目的业务覆盖率目标，交互逻辑应在业务 Hook/状态模块中验证。
        '**/components/**',
        'components/**',
        'App.tsx',
        'index.tsx',
        'chatEmojis.tsx',
        'app/features/dashboard/DashboardTrendChart.tsx',
      ],
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, '.'),
    },
  },
  build: {
    outDir: '../internal/webui/static',
    sourcemap: false,
    rollupOptions: {
      output: {
        // manualChunks 按依赖领域拆分生产构建文件，控制首屏资源体积。
        manualChunks(id) {
          // modulePath 是统一分隔符后的模块绝对路径，用于稳定匹配依赖目录。
          const modulePath = id.split(path.sep).join('/');
          // 聊天元数据包含快捷回复和备注弹窗等低频交互，独立分片可避免挤占会话阅读首屏。
          if (modulePath.includes('/app/features/chat/components/ChatMetadataFeature.') || modulePath.includes('/app/features/chat/metadata.')) {
            return 'chat-metadata';
          }
          // 模板请求 Hook 只服务模板管理页，独立分片可保持编辑器页面在既有下载预算内。
          if (modulePath.includes('/app/features/delivery-templates/hooks.')) {
            return 'delivery-template-runtime';
          }
          if (!modulePath.includes('/node_modules/')) {
            return undefined;
          }
          if (
            modulePath.includes('/react/') ||
            modulePath.includes('/react-dom/') ||
            modulePath.includes('/scheduler/')
          ) {
            return 'react-vendor';
          }
          if (
            modulePath.includes('/recharts/') ||
            modulePath.includes('/victory-vendor/') ||
            modulePath.includes('/d3-')
          ) {
            return 'charts-vendor';
          }
          if (modulePath.includes('/lucide-react/')) {
            return 'icons-vendor';
          }
          // AMR 解码器体积较大，随聊天懒加载路由使用独立块，避免拖慢应用首屏并保持依赖可审计。
          if (modulePath.includes('/benz-amr-recorder/')) {
            return 'audio-codec';
          }
          return 'vendor';
        },
      },
    },
    emptyOutDir: true,
  },
});
