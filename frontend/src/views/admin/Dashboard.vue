<script setup lang="ts">
import { ref, onMounted } from 'vue'
import api from '../../api'

const stats = ref({
  modelTotal: 0,
  modelEnabled: 0,
  llmCount: 0,
  imageCount: 0,
  videoCount: 0,
  totalCalls: 0,
  totalFails: 0
})
const loading = ref(true)
const models = ref<any[]>([])

const load = async () => {
  loading.value = true
  try {
    const [ov, ms]: any = await Promise.all([
      api.get('/admin/overview'),
      api.get('/admin/models')
    ])
    stats.value = ov.data
    models.value = ms.data || []
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

// 检查配置完整性
const warnings = () => {
  const out: { level: string; text: string }[] = []
  const cats = ['llm', 'image', 'video']
  for (const c of cats) {
    const items = models.value.filter((m) => m.category === c && m.enabled)
    if (items.length === 0) {
      out.push({ level: 'error', text: `${c === 'llm' ? '文本' : c === 'image' ? '图像' : '视频'}模型未配置，该环节无法运行` })
    } else if (!items.some((m) => m.hasKey)) {
      out.push({ level: 'warn', text: `${c === 'llm' ? '文本' : c === 'image' ? '图像' : '视频'}模型都缺少 API Key` })
    }
  }
  return out
}

onMounted(load)
</script>

<template>
  <div class="dash">
    <div class="page-head">
      <div>
        <h1>仪表盘</h1>
        <p>系统运行状态与配置概览</p>
      </div>
    </div>

    <!-- 统计卡 -->
    <div class="stat-grid">
      <div class="stat-card">
        <div class="sc-label">已配置模型</div>
        <div class="sc-value">{{ stats.modelTotal }}</div>
        <div class="sc-sub">其中 {{ stats.modelEnabled }} 个启用</div>
      </div>
      <div class="stat-card">
        <div class="sc-label">累计调用</div>
        <div class="sc-value">{{ stats.totalCalls }}</div>
        <div class="sc-sub" :class="{ err: stats.totalFails > 0 }">
          失败 {{ stats.totalFails }} 次
        </div>
      </div>
      <div class="stat-card">
        <div class="sc-label">文本 / 图像 / 视频</div>
        <div class="sc-value small">
          {{ stats.llmCount }} / {{ stats.imageCount }} / {{ stats.videoCount }}
        </div>
        <div class="sc-sub">各分类模型数</div>
      </div>
    </div>

    <!-- 配置检查 -->
    <div class="section">
      <h2>配置检查</h2>
      <div v-if="warnings().length === 0" class="check ok">
        <svg viewBox="0 0 24 24" fill="none" width="16" height="16">
          <path d="m5 12.5 4.5 4.5L19 7" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        核心模型均已配置
      </div>
      <div v-else class="check-list">
        <div v-for="(w, i) in warnings()" :key="i" class="check" :class="w.level">
          <svg v-if="w.level === 'error'" viewBox="0 0 24 24" fill="none" width="15" height="15">
            <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.8" />
            <path d="M12 8v5M12 16h.01" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" width="15" height="15">
            <path d="M12 4 2.5 20h19L12 4Zm0 5v5m0 3h.01" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          {{ w.text }}
        </div>
      </div>
    </div>

    <!-- 模型速览 -->
    <div class="section">
      <div class="sec-head">
        <h2>模型速览</h2>
        <router-link to="/admin/models" class="sec-link">去配置 →</router-link>
      </div>
      <div class="mini-list">
        <div v-for="m in models.slice(0, 8)" :key="m.id" class="mini-row">
          <span class="mr-cat" :class="m.category">{{ m.category }}</span>
          <span class="mr-name">{{ m.displayName }}</span>
          <span class="mr-id">{{ m.modelId }}</span>
          <span v-if="m.hasKey" class="mr-key ok">已配密钥</span>
          <span v-else class="mr-key no">缺密钥</span>
          <span v-if="m.isDefault" class="mr-default">默认</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dash {
  width: 100%;
}
.page-head { margin-bottom: 22px; }
.page-head h1 { font-size: 22px; font-weight: 680; letter-spacing: -0.025em; }
.page-head p { font-size: 13px; color: var(--text-mute); margin-top: 3px; }

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 12px;
  margin-bottom: 28px;
}
.stat-card {
  padding: 16px 18px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
}
.sc-label { font-size: 11.5px; color: var(--text-mute); margin-bottom: 6px; }
.sc-value {
  font-size: 30px;
  font-weight: 700;
  letter-spacing: -0.035em;
  background: var(--grad);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
}
.sc-value.small { font-size: 24px; }
.sc-sub { font-size: 11.5px; color: var(--text-mute); margin-top: 3px; }
.sc-sub.err { color: var(--err); }

.section { margin-bottom: 28px; }
.section h2 { font-size: 15px; font-weight: 620; margin-bottom: 12px; }
.sec-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.sec-head h2 { margin-bottom: 0; }
.sec-link { font-size: 12px; color: var(--accent); }

.check-list { display: flex; flex-direction: column; gap: 8px; }
.check {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 11px 14px;
  border-radius: var(--r-md);
  font-size: 12.5px;
  border: 1px solid var(--border);
}
.check.ok { color: var(--ok); border-color: rgba(52, 211, 153, 0.28); background: rgba(52, 211, 153, 0.07); }
.check.warn { color: var(--warn); border-color: rgba(251, 191, 36, 0.26); background: rgba(251, 191, 36, 0.07); }
.check.error { color: var(--err); border-color: rgba(251, 113, 133, 0.26); background: rgba(251, 113, 133, 0.07); }

.mini-list {
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  overflow: hidden;
}
.mini-row {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  font-size: 12.5px;
}
.mini-row:last-child { border-bottom: none; }
.mini-row:hover { background: rgba(255, 255, 255, 0.02); }
.mr-cat {
  font-size: 9.5px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 3px;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.mr-cat.llm { color: #c4b5fd; background: rgba(124, 92, 255, 0.16); }
.mr-cat.image { color: #67e8f9; background: rgba(34, 211, 238, 0.14); }
.mr-cat.video { color: #fcd34d; background: rgba(251, 191, 36, 0.14); }
.mr-cat.tts { color: #f9a8d4; background: rgba(244, 114, 182, 0.14); }
.mr-name { font-weight: 560; min-width: 130px; }
.mr-id {
  font-family: 'SF Mono', Menlo, monospace;
  font-size: 11px;
  color: var(--text-mute);
  flex: 1;
}
.mr-key { font-size: 10.5px; }
.mr-key.ok { color: var(--ok); }
.mr-key.no { color: var(--err); }
.mr-default {
  font-size: 9.5px;
  padding: 1px 6px;
  border-radius: 3px;
  color: #c4b5fd;
  background: rgba(124, 92, 255, 0.16);
}

@media (max-width: 720px) {
  .stat-grid { grid-template-columns: 1fr; }
  .mr-id { display: none; }
}
</style>
