<script setup lang="ts">
import { ref, onMounted } from 'vue'
import api from '../../api'
import { toast } from '../../composables/useToast'

const props = defineProps<{
  group: string
  title: string
  desc: string
}>()

const settings = ref<any[]>([])
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)

const load = async () => {
  loading.value = true
  try {
    const res: any = await api.get('/admin/settings', { params: { group: props.group } })
    settings.value = res.data || []
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const save = async () => {
  saving.value = true
  saved.value = false
  try {
    await api.put('/admin/settings', settings.value.map((s) => ({ key: s.key, value: String(s.value) })))
    saved.value = true
    toast.success('设置已保存', '新的数值会立即生效')
    setTimeout(() => (saved.value = false), 2200)
  } catch (e: any) {
    toast.error('保存失败', e.response?.data?.message || e.message)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="settings-page">
    <div class="page-head">
      <div>
        <h1>{{ title }}</h1>
        <p>{{ desc }}</p>
      </div>
      <div class="head-actions">
        <span v-if="saved" class="saved-hint">已保存</span>
        <button class="btn btn-primary" :disabled="saving || loading" @click="save">
          <span v-if="saving" class="spinner"></span>
          {{ saving ? '保存中' : '保存设置' }}
        </button>
      </div>
    </div>

    <div v-if="loading" class="loading">加载中…</div>

    <div v-else class="setting-list">
      <div v-for="s in settings" :key="s.key" class="setting-row">
        <div class="sr-left">
          <div class="sr-label">{{ s.label || s.key }}</div>
          <div class="sr-key">{{ s.key }}</div>
          <div v-if="s.remark" class="sr-remark">{{ s.remark }}</div>
        </div>
        <div class="sr-right">
          <input
            v-if="s.valueType === 'int'"
            v-model.number="s.value"
            class="field"
            type="number"
          />
          <input v-else v-model="s.value" class="field" />
          <span v-if="s.valueType" class="sr-type">{{ s.valueType }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-page {
  width: 100%;
}

.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 22px;
}
.page-head h1 { font-size: 22px; font-weight: 680; letter-spacing: -0.025em; }
.page-head p { font-size: 13px; color: var(--text-mute); margin-top: 3px; }
.head-actions { display: flex; align-items: center; gap: 12px; }
.saved-hint { font-size: 12px; color: var(--ok); }

.loading { padding: 60px; text-align: center; color: var(--text-mute); font-size: 13px; }

.setting-list {
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  overflow: hidden;
}
.setting-row {
  display: flex;
  align-items: center;
  gap: 24px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
}
.setting-row:last-child { border-bottom: none; }
.setting-row:hover { background: rgba(255, 255, 255, 0.015); }

.sr-left { flex: 1; min-width: 0; }
.sr-label { font-size: 13.5px; font-weight: 550; margin-bottom: 2px; }
.sr-key {
  font-size: 10.5px;
  font-family: 'SF Mono', Menlo, monospace;
  color: var(--text-mute);
}
.sr-remark { font-size: 11.5px; color: var(--text-mute); margin-top: 3px; }

.sr-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  width: 260px;
}
.field {
  width: 100%;
  height: 36px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.025);
  font-size: 13px;
  color: var(--text);
  transition: all 0.22s var(--ease);
  text-align: right;
}
.field:focus { border-color: rgba(124, 92, 255, 0.5); background: rgba(124, 92, 255, 0.05); }
.sr-type {
  font-size: 9.5px;
  color: var(--text-mute);
  font-family: 'SF Mono', Menlo, monospace;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.04);
}

@media (max-width: 620px) {
  .page-head { flex-direction: column; align-items: stretch; }
  .setting-row { flex-direction: column; align-items: stretch; gap: 10px; }
  .sr-right { width: 100%; }
  .field { text-align: left; }
}
</style>
