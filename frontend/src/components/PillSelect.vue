<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'

interface PillOption {
  value: string
  label: string
  hint?: string
  /** 选项角标，例如画幅 16:9 */
  badge?: string
}

const props = defineProps<{
  label: string
  modelValue: string
  options: PillOption[]
  disabled?: boolean
  width?: number
  /** 打开时右侧是否显示效果预览（需配合 #preview 插槽） */
  previewable?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)
const hovered = ref('')
/** 靠近屏幕右缘时改为右对齐，避免下拉跑出视口 */
const alignRight = ref(false)

const current = computed(
  () => props.options.find((o) => o.value === props.modelValue) || props.options[0]
)

/** 预览跟随鼠标悬停，未悬停时显示当前选中项 */
const previewOption = computed(
  () => props.options.find((o) => o.value === (hovered.value || props.modelValue)) || current.value
)

const close = () => {
  open.value = false
  hovered.value = ''
}

const toggle = async () => {
  if (open.value) {
    close()
    return
  }
  alignRight.value = false
  open.value = true
  // 先按左对齐渲染，量出实际宽度后再决定是否翻到右侧
  await nextTick()
  const el = menuEl.value
  if (!el) return
  const r = el.getBoundingClientRect()
  if (r.right > window.innerWidth - 12) alignRight.value = true
}

const pick = (v: string) => {
  emit('update:modelValue', v)
  close()
}

const onDocClick = (e: MouseEvent) => {
  if (root.value && !root.value.contains(e.target as Node)) close()
}
const onEsc = (e: KeyboardEvent) => {
  if (e.key === 'Escape') close()
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onEsc)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onEsc)
})
</script>

<template>
  <div ref="root" class="pill-group">
    <span class="pill-label">{{ label }}</span>
    <div class="pill-wrap">
      <button
        type="button"
        class="pill-btn"
        :class="{ on: open }"
        :disabled="disabled"
        :style="width ? { minWidth: width + 'px' } : undefined"
        @click="toggle"
      >
        <span class="pill-val">{{ current?.label || '未选择' }}</span>
        <svg class="chev" :class="{ rot: open }" viewBox="0 0 24 24" fill="none" width="11" height="11">
          <path d="m6 9 6 6 6-6" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>

      <div
        v-if="open"
        ref="menuEl"
        class="pill-menu"
        :class="{ 'with-preview': previewable, 'align-right': alignRight }"
      >
        <div class="pm-list">
          <button
            v-for="o in options"
            :key="o.value"
            type="button"
            class="pill-item"
            :class="{ active: o.value === modelValue }"
            @mouseenter="hovered = o.value"
            @mouseleave="hovered = ''"
            @click="pick(o.value)"
          >
            <span class="pi-main">
              <span class="pi-top">
                <span class="pi-label">{{ o.label }}</span>
                <span v-if="o.badge" class="pi-badge">{{ o.badge }}</span>
              </span>
              <span v-if="o.hint" class="pi-hint">{{ o.hint }}</span>
            </span>
            <svg v-if="o.value === modelValue" class="pi-check" viewBox="0 0 24 24" fill="none" width="12" height="12">
              <path d="m5 13 4.5 4.5L19 7" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>

        <div v-if="previewable" class="pm-preview">
          <slot name="preview" :option="previewOption" />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pill-group {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.pill-label {
  font-size: 12.5px;
  color: var(--text-mute);
  white-space: nowrap;
}
.pill-wrap {
  position: relative;
}
.pill-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 10px 0 12px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  font-size: 12.5px;
  font-family: inherit;
  cursor: pointer;
  transition: all 0.22s var(--ease);
  white-space: nowrap;
}
.pill-btn:hover:not(:disabled) {
  background: var(--surface-hover);
  border-color: var(--border-strong);
}
.pill-btn.on {
  border-color: rgba(124, 92, 255, 0.55);
  background: var(--accent-soft);
  color: #cfc2ff;
}
.pill-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.pill-val {
  max-width: 168px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.chev {
  color: var(--text-mute);
  transition: transform 0.22s var(--ease);
  flex-shrink: 0;
}
.chev.rot {
  transform: rotate(180deg);
}

.pill-menu {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 40;
  min-width: 240px;
  padding: 6px;
  border-radius: 14px;
  border: 1px solid var(--border-strong);
  background: #14141d;
  box-shadow: 0 18px 44px rgba(0, 0, 0, 0.55);
  animation: pillIn 0.16s var(--ease) both;
  max-height: 420px;
  overflow-y: auto;
}
.pill-menu.align-right {
  left: auto;
  right: 0;
}
/* 带预览时排成两栏：左选项、右效果图；宽度写死，翻边判定才能在第一帧就拿到真实宽度 */
.pill-menu.with-preview {
  display: flex;
  gap: 8px;
  width: 528px;
  min-width: 0;
  padding: 8px;
  max-height: none;
  overflow: visible;
}
.pm-list {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
  gap: 2px;
}
.pm-preview {
  flex: 0 0 240px;
  width: 240px;
  padding: 8px;
  border-radius: 11px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--border);
}
/* 只用位移入场，不碰 opacity：后台标签页 rAF 被节流时动画会冻在中途，
   带透明度的入场会让下拉停在半透明状态 */
@keyframes pillIn {
  from {
    transform: translateY(-5px) scale(0.99);
  }
  to {
    transform: none;
  }
}
.pill-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 9px;
  background: transparent;
  color: var(--text-dim);
  font-family: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.16s var(--ease);
}
.pill-item:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.pill-item.active {
  background: var(--accent-soft);
  color: #cfc2ff;
}
.pi-main {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  flex: 1;
}
.pi-top {
  display: flex;
  align-items: center;
  gap: 8px;
}
.pi-label {
  font-size: 12.5px;
}
.pi-badge {
  padding: 1px 6px;
  border-radius: 5px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.07);
  border: 1px solid var(--border);
}
.pill-item.active .pi-badge {
  color: #cfc2ff;
  background: rgba(124, 92, 255, 0.16);
  border-color: rgba(124, 92, 255, 0.3);
}
.pi-hint {
  font-size: 11px;
  line-height: 1.5;
  color: var(--text-mute);
}
.pi-check {
  flex-shrink: 0;
  color: var(--accent);
}
</style>
