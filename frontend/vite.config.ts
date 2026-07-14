import path from 'path';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  base: '/static/',
  server: {
    port: 3000,
    host: '0.0.0.0',
    proxy: {
      // 代理API请求到后端
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      // 代理其他后端请求
      '/cookies': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/account': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/qr-login': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/password-login': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/keywords': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/keywords-with-item-id': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/default-reply': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/items': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/cards': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/automation-rules': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/automation-issues': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/automation-runs': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/automation-pending-tasks': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/notification-channels': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/message-notifications': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ai-reply-settings': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ai-models': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/system-settings': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/user-settings': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/admin': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/analytics': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/login': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/verify': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/logout': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/change-password': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  plugins: [react()],
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
        manualChunks(id) {
          const modulePath = id.split(path.sep).join('/');
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
          return 'vendor';
        },
      },
    },
    emptyOutDir: true,
  },
});
