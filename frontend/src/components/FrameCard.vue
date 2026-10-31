<script setup lang="ts">
import { ref, computed } from 'vue'

const props = defineProps<{
  frame: any
  index: number
  active: boolean
  status?: string
}>()

const emit = defineEmits<{
  (e: 'select'): void
  (e: 'save', payload: { text: string; videoPrompt: string }): void
  (e: 'regenerate'): void
}>()

const editing = ref(false)
const draftText = ref('')
const draftPrompt = ref('')

// 视频是否为占位标记
const isPh = computed(
  () => typeof props.frame.videoUrl === 'string' && props.frame.videoUrl.startsWith('placeholder://')
)

const startEdit = () => {
  draftText.value = props.frame.text
  draftPrompt.value = props.frame.videoPrompt
  editing.value = true
}

const save = () => {
  emit('save', { text: draftText.value, videoPrompt: draftPrompt.value })
  editing.value = false
}

const cancel = () => {
  editing.value = false
}
</script>

<template>
  <div class="frame-card" :class="{ active }" @click="emit('select')">
    <!-- 头部 -->
    <div class="fc-head">
      <span class="fc-idx">镜 {{ String(index + 1).padStart(2, '0') }}</span>
      <div class="fc-tags">
        <span v-for="r in frame.roles || []" :key="r.name" class="tag role">@{{ r.name }}</span>
        <span v-for="s in frame.scenes || []" :key="s.name" class="tag scene">@{{ s.name }}</span>
        <span v-for="it in frame.items || []" :key="it.name" class="tag item">@{{ it.name }}</span>
      </div>
      <span v-if="frame.videoUrl && !isPh" class="fc-badge done">已出片</span>
      <span v-else-if="frame.videoUrl && isPh" class="fc-badge ph">占位视频</span>
      <span v-else-if="status && status !== 'failed'" class="fc-badge run">
        <span class="spinner" style="width: 9px; height: 9px; border-width: 1.5px"></span>
        {{ status === 'pending' ? '排队' : '生成中' }}
      </span>
      <span class="fc-dur">3-5s</span>
    </div>

    <!-- 已生成视频：真实地址用播放器，占位标记渲染占位卡 -->
    <video v-if="frame.videoUrl && !isPh" class="fc-video" :src="frame.videoUrl" controls muted playsinline preload="metadata"></video>
    <div v-else-if="isPh" class="fc-video-ph">
      <svg viewBox="0 0 24 24" fill="none" width="26" height="26">
        <rect x="2.5" y="5" width="19" height="14" rx="2.5" stroke="currentColor" stroke-width="1.6" />
        <path d="M10 9.5v5l4.5-2.5L10 9.5Z" fill="currentColor" />
      </svg>
      <span>视频占位 · 待接入真实模型</span>
    </div>

    <!-- 编辑态 -->
    <template v-if="editing">
      <div class="fc-edit">
        <label>人读脚本</label>
        <textarea v-model="draftText" rows="6"></textarea>
        <label>机读描述词</label>
        <textarea v-model="draftPrompt" rows="10"></textarea>
        <div class="fc-edit-actions">
          <button class="btn btn-ghost btn-sm" @click.stop="cancel">取消</button>
          <button class="btn btn-primary btn-sm" @click.stop="save">保存</button>
        </div>
      </div>
    </template>

    <!-- 展示态 -->
    <template v-else>
      <pre class="fc-text">{{ frame.text }}</pre>
      <div class="fc-foot">
        <button class="fc-btn" @click.stop="startEdit">
          <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
            <path d="m4 20 4-1 10-10-3-3L5 16l-1 4Z" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
          </svg>
          编辑
        </button>
        <button class="fc-btn" @click.stop="emit('regenerate')">
          <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
            <path d="M4 12a8 8 0 0 1 13.7-5.6M20 12a8 8 0 0 1-13.7 5.6" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
            <path d="M18 3v4h-4M6 21v-4h4" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          重生成
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.frame-card {
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.02);
  padding: 13px 14px;
  transition: all 0.26s var(--ease);
  cursor: pointer;
}
.frame-card:hover {
  border-color: var(--border-strong);
  background: var(--surface-hover);
}
.frame-card.active {
  border-color: rgba(124, 92, 255, 0.5);
  background: var(--accent-soft);
  box-shadow: 0 0 0 3px rgba(124, 92, 255, 0.08);
}

.fc-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 9px;
}
.fc-idx {
  font-size: 11px;
  font-weight: 680;
  letter-spacing: 0.03em;
  color: var(--accent);
  flex-shrink: 0;
}
.fc-tags {
  display: flex;
  gap: 5px;
  flex-wrap: wrap;
  flex: 1;
  min-width: 0;
}
.tag {
  padding: 1px 7px;
  border-radius: 4px;
  font-size: 10.5px;
  font-weight: 520;
  white-space: nowrap;
}
.tag.role {
  color: #c4b5fd;
  background: rgba(124, 92, 255, 0.16);
}
.tag.scene {
  color: #67e8f9;
  background: rgba(34, 211, 238, 0.13);
}
.tag.item {
  color: #fcd34d;
  background: rgba(251, 191, 36, 0.13);
}
.fc-dur {
  font-size: 10.5px;
  color: var(--text-mute);
  flex-shrink: 0;
}

.fc-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 7px;
  border-radius: 99px;
  font-size: 10px;
  font-weight: 550;
  flex-shrink: 0;
}
.fc-badge.done {
  color: var(--ok);
  background: rgba(52, 211, 153, 0.12);
}
.fc-badge.run {
  color: var(--warn);
  background: rgba(251, 191, 36, 0.12);
}
.fc-badge.ph {
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.06);
}

.fc-video {
  width: 100%;
  border-radius: var(--r-sm);
  margin-bottom: 10px;
  background: #000;
  max-height: 220px;
}

.fc-video-ph {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 132px;
  margin-bottom: 10px;
  border-radius: var(--r-sm);
  border: 1px dashed var(--border-strong);
  background: rgba(255, 255, 255, 0.018);
  color: var(--text-mute);
  font-size: 11px;
}

.fc-text {
  font-family: 'SF Mono', Menlo, 'PingFang SC', monospace;
  font-size: 11.5px;
  line-height: 1.75;
  color: var(--text-dim);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 190px;
  overflow-y: auto;
  margin: 0;
}

.fc-foot {
  display: flex;
  gap: 6px;
  margin-top: 10px;
  padding-top: 9px;
  border-top: 1px solid var(--border);
}
.fc-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 9px;
  border-radius: 6px;
  font-size: 11.5px;
  color: var(--text-mute);
  transition: all 0.2s var(--ease);
}
.fc-btn:hover {
  color: var(--text);
  background: var(--surface-hover);
}

.fc-edit label {
  display: block;
  font-size: 10.5px;
  color: var(--text-mute);
  margin-bottom: 4px;
  margin-top: 8px;
}
.fc-edit label:first-child {
  margin-top: 0;
}
.fc-edit textarea {
  width: 100%;
  padding: 8px 10px;
  border-radius: 7px;
  border: 1px solid var(--border);
  background: var(--bg-soft);
  font-family: 'SF Mono', Menlo, monospace;
  font-size: 11px;
  line-height: 1.7;
  color: var(--text);
}
.fc-edit textarea:focus {
  border-color: rgba(124, 92, 255, 0.5);
}
.fc-edit-actions {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  margin-top: 10px;
}
</style>
