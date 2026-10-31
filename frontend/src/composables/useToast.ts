import { reactive } from 'vue'

export type ToastType = 'success' | 'error' | 'warning' | 'info' | 'loading'

export interface ToastItem {
  id: number
  type: ToastType
  title: string
  desc?: string
  /** 毫秒，0 表示不自动关闭 */
  duration: number
  /** 0-100，用于展示进度条 */
  progress?: number
}

let seed = 0

export const toastState = reactive<{ items: ToastItem[] }>({ items: [] })

const close = (id: number) => {
  const i = toastState.items.findIndex((t) => t.id === id)
  if (i > -1) toastState.items.splice(i, 1)
}

const push = (type: ToastType, title: string, desc?: string, duration = 3200) => {
  const id = ++seed
  const item: ToastItem = { id, type, title, desc, duration }
  toastState.items.push(item)

  // 最多同时显示 4 条，超出挤掉最早的
  if (toastState.items.length > 4) {
    toastState.items.shift()
  }

  if (duration > 0) {
    setTimeout(() => close(id), duration)
  }
  return id
}

export const toast = {
  success: (title: string, desc?: string, duration?: number) => push('success', title, desc, duration),
  error: (title: string, desc?: string, duration?: number) => push('error', title, desc, duration ?? 4500),
  warning: (title: string, desc?: string, duration?: number) => push('warning', title, desc, duration),
  info: (title: string, desc?: string, duration?: number) => push('info', title, desc, duration),
  loading: (title: string, desc?: string) => push('loading', title, desc, 0),
  close,
  clear: () => (toastState.items = [])
}

export const useToast = () => toast
