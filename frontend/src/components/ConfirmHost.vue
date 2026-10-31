<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { confirmState, answerConfirm } from '../composables/useConfirm'

const onKey = (e: KeyboardEvent) => {
  if (!confirmState.visible) return
  if (e.key === 'Escape') {
    e.preventDefault()
    answerConfirm(false)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    answerConfirm(true)
  }
}

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <div v-if="confirmState.visible" class="cf-mask" @click.self="answerConfirm(false)">
      <div class="cf-box" role="dialog" aria-modal="true">
        <div class="cf-head">
          <div class="cf-icon" :class="{ danger: confirmState.options.danger }">
            <svg viewBox="0 0 24 24" fill="none" width="17" height="17">
              <path
                d="M12 8.5v5M12 16.8v.1M10.3 3.9 2.6 17.2a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          </div>
          <div class="cf-text">
            <h3>{{ confirmState.options.title }}</h3>
            <p v-if="confirmState.options.desc">{{ confirmState.options.desc }}</p>
          </div>
        </div>

        <div class="cf-foot">
          <button class="btn btn-ghost btn-sm" @click="answerConfirm(false)">
            {{ confirmState.options.cancelText || '取消' }}
          </button>
          <button
            class="btn btn-sm"
            :class="confirmState.options.danger ? 'btn-danger' : 'btn-primary'"
            @click="answerConfirm(true)"
          >
            {{ confirmState.options.confirmText || '确定' }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.cf-mask {
  position: fixed;
  inset: 0;
  z-index: 9100;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(4, 4, 9, 0.62);
  backdrop-filter: blur(6px);
}

.cf-box {
  width: 100%;
  max-width: 400px;
  padding: 20px;
  border-radius: 18px;
  background: #15151e;
  border: 1px solid var(--border-strong);
  box-shadow: 0 32px 80px -28px rgba(0, 0, 0, 0.95);
  animation: cfPop 0.26s var(--ease) both;
}

/* 只用位移/缩放入场，不碰 opacity：后台标签页 rAF 被节流时动画会冻在中途，
   带透明度的入场会让确认框停在半透明 */
@keyframes cfPop {
  from { transform: translateY(10px) scale(0.96); }
  to { transform: none; }
}

.cf-head {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.cf-icon {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  color: var(--accent);
  background: var(--accent-soft);
}
.cf-icon.danger {
  color: var(--err);
  background: rgba(251, 113, 133, 0.14);
}

.cf-text h3 {
  font-size: 14.5px;
  font-weight: 620;
  letter-spacing: -0.015em;
  line-height: 1.4;
}

.cf-text p {
  margin-top: 5px;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--text-mute);
}

.cf-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}
</style>
