import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import { resolve } from 'path'
import { tmpdir } from 'node:os'

export default defineConfig({
  plugins: [vue()],
  // 依赖缓存放到系统临时目录，避免 node_modules 目录被安全策略锁住导致 rename 失败。
  // 必须用 tmpdir() 而不是字面量 '/tmp'：在 Windows 上 '/tmp' 会被解析成
  // 「当前盘符根目录\tmp」（如 D:\tmp），等于在用户盘根目录里乱建目录。
  cacheDir: resolve(tmpdir(), 'vite-cache-jikeai-clone'),
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
