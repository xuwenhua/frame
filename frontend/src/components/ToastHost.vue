<script setup lang="ts">
import { toastState, toast } from '../composables/useToast'

const iconPath: Record<string, string> = {
  success: 'M4.5 12.5 9 17l10.5-10.5',
  error: 'M12 7v7M12 17.2v.1',
  warning: 'M12 8v6M12 17.2v.1',
  info: 'M12 11v6M12 7.2v.1',
  loading: 'M12 4a8 8 0 1 0 8 8'
}
</script>

<template>
  <Teleport to="body">
    <div class="toast-host">
      <div v-for="t in toastState.items" :key="t.id" class="toast" :class="t.type">
        <div class="t-icon">
          <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
            <!-- loading 用旋转圈 -->
            <circle
              v-if="t.type === 'loading'"
              cx="12"
              cy="12"
              r="8"
              stroke="currentColor"
              stroke-width="2.2"
              stroke-linecap="round"
              stroke-dasharray="36 14"
            />
            <path
              v-else
              :d="iconPath[t.type]"
              stroke="currentColor"
              stroke-width="2.2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </div>

        <div class="t-body">
          <div class="t-title">{{ t.title }}</div>
          <div v-if="t.desc" class="t-desc">{{ t.desc }}</div>
        </div>

        <button class="t-close" title="关闭" @click="toast.close(t.id)">
          <svg viewBox="0 0 24 24" fill="none" width="12" height="12">
            <path d="M6 6l12 12M18 6 6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>

        <span
          v-if="t.duration > 0"
          class="t-bar"
          :style="{ animationDuration: t.duration + 'ms' }"
        ></span>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-host {
  position: fixed;
  /* 避开顶部导航（68px）与管理端顶栏（60px） */
  top: 82px;
  left: 50%;
  transform: translateX(-50%);
  /* 必须高于所有弹层：弹窗遮罩 9100、大图查看 9200、确认框 9100。
     之前是 9000，弹窗一开 toast 就被压到下面看不见了
     （首帧规划弹窗个子高，顶部正好盖住 toast 的位置）。
     toast-host 整体 pointer-events: none，浮在最上层也不会挡住弹窗的按钮。 */
  z-index: 9800;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  pointer-events: none;
}

.toast {
  position: relative;
  pointer-events: auto;
  display: flex;
  align-items: flex-start;
  gap: 11px;
  min-width: 268px;
  max-width: 400px;
  padding: 13px 14px 14px;
  border-radius: 14px;
  background: rgba(23, 23, 33, 0.94);
  border: 1px solid var(--border-strong);
  box-shadow:
    0 24px 60px -22px rgba(0, 0, 0, 0.92),
    0 0 0 1px rgba(0, 0, 0, 0.4) inset;
  backdrop-filter: blur(16px) saturate(140%);
  overflow: hidden;
  animation: toastIn 0.32s cubic-bezier(0.22, 1, 0.36, 1) both;
}

@keyframes toastIn {
  from { opacity: 0; transform: translateY(-12px) scale(0.96); }
  to { opacity: 1; transform: none; }
}

/* 左侧类型色描边 */
.toast::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--tc);
  opacity: 0.9;
}

.toast.success { --tc: var(--ok); }
.toast.error   { --tc: var(--err); }
.toast.warning { --tc: var(--warn); }
.toast.info    { --tc: var(--accent); }
.toast.loading { --tc: var(--accent-3); }

.t-icon {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  color: var(--tc);
  background: color-mix(in srgb, var(--tc) 15%, transparent);
  margin-top: 1px;
}
.toast.loading .t-icon svg { animation: spin 0.85s linear infinite; }

@keyframes spin {
  to { transform: rotate(360deg); }
}

.t-body { flex: 1; min-width: 0; }

.t-title {
  font-size: 13px;
  font-weight: 590;
  line-height: 1.45;
  letter-spacing: -0.01em;
  color: var(--text);
}

.t-desc {
  margin-top: 3px;
  font-size: 11.5px;
  line-height: 1.55;
  color: var(--text-mute);
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.t-close {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  border-radius: 5px;
  display: grid;
  place-items: center;
  color: var(--text-mute);
  opacity: 0;
  transition: all 0.2s var(--ease);
  margin-top: 1px;
}
.toast:hover .t-close { opacity: 1; }
.t-close:hover { color: var(--text); background: var(--surface-hover); }

.t-bar {
  position: absolute;
  left: 0;
  bottom: 0;
  height: 1.5px;
  width: 100%;
  background: var(--tc);
  opacity: 0.45;
  transform-origin: left;
  animation: shrink linear forwards;
}

@keyframes shrink {
  from { transform: scaleX(1); }
  to { transform: scaleX(0); }
}
</style>
