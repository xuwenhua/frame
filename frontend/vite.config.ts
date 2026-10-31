import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  // 依赖缓存放到系统临时目录，避免 node_modules 目录被安全策略锁住导致 rename 失败
  cacheDir: '/tmp/vite-cache-jikeai-clone',
  resolve: {
    alias: {
      '@': resolve(import.meta.dirname, 'src')
    }
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true
      },
      // 生成产物（转存后的图片/视频）
      '/static': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
