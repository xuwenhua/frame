import { reactive } from 'vue'

export interface ConfirmOptions {
  title: string
  desc?: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

export const confirmState = reactive<{
  visible: boolean
  options: ConfirmOptions
  resolve: ((v: boolean) => void) | null
}>({
  visible: false,
  options: { title: '' },
  resolve: null
})

/** 返回 Promise<boolean> 的应用内确认框，替代原生 confirm */
export const confirmDialog = (options: ConfirmOptions): Promise<boolean> => {
  // 若已有未处理的确认框，先取消掉
  if (confirmState.resolve) {
    confirmState.resolve(false)
    confirmState.resolve = null
  }
  confirmState.options = options
  confirmState.visible = true
  return new Promise<boolean>((resolve) => {
    confirmState.resolve = resolve
  })
}

export const answerConfirm = (value: boolean) => {
  confirmState.visible = false
  const r = confirmState.resolve
  confirmState.resolve = null
  r?.(value)
}
