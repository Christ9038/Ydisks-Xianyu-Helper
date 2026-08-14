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
