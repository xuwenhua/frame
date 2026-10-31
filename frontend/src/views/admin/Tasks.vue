<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import api from '../../api'
import { toast } from '../../composables/useToast'
import { confirmDialog } from '../../composables/useConfirm'

interface TaskRow {
  id: string
  userId: string
  type: 'image' | 'video'
  assetType: string
  assetId: string
  assetName?: string
  prompt: string
  modelId: string
  params: string
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  progress: number
  resultUrl: string
  resultJson: string
  errorMsg: string
  credits: number
  creditsRefunded: boolean
  externalTaskId: string
  startedAt: string | null
  finishedAt: string | null
  createdAt: string
  userEmail?: string
  userNick?: string
}

interface Stats {
  task: {
    pending: number
    running: number
    todayDone: number
    todayFail: number
    todayAll: number
    avgCostMs: number
    hourDone: number
    hourFail: number
  }
  queue: { running: number; max: number; workers: number }
}

const rows = ref<TaskRow[]>([])
const total = ref(0)
const page = ref(1)
const size = 20
const loading = ref(false)
/** 状态发生变化的行，做一次高亮闪动 */
const flash = ref<Record<string, boolean>>({})

const stats = ref<Stats | null>(null)

// 实时刷新
const live = ref(true)
const interval = ref(3000)
const lastSync = ref<Date | null>(null)
let timer: number | null = null

// 筛选
const filterStatus = ref('')
const filterType = ref('')
const onlyToday = ref(true)
const keyword = ref('')

const statusTabs = [
  { value: '', label: '全部' },
  { value: 'running', label: '生成中' },
  { value: 'pending', label: '排队中' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' }
]

const statusMeta: Record<string, { label: string; cls: string }> = {
  pending: { label: '排队中', cls: 'pending' },
  running: { label: '生成中', cls: 'running' },
  succeeded: { label: '成功', cls: 'ok' },
  failed: { label: '失败', cls: 'fail' }
}

const assetLabel: Record<string, string> = {
  character: '角色',
  scene: '场景',
  item: '道具',
  frame: '分镜'
}

const loadList = async () => {
  loading.value = true
  try {
    const res: any = await api.get('/admin/tasks', {
      params: {
        page: page.value,
        size,
        status: filterStatus.value || undefined,
        type: filterType.value || undefined,
        keyword: keyword.value || undefined,
        today: onlyToday.value ? 1 : undefined
      }
    })
    // 标记刚变化的状态，做一次高亮闪动
    const prev = new Map(rows.value.map((r) => [r.id, r.status]))
    const next: TaskRow[] = res.data?.list || []
    for (const r of next) {
      const before = prev.get(r.id)
      if (before && before !== r.status) flash.value[r.id] = true
    }
    rows.value = next
    total.value = res.data?.total || 0
    lastSync.value = new Date()
  } catch (e: any) {
    toast.error(e.response?.data?.message || '加载任务失败')
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const res: any = await api.get('/admin/tasks/stats')
    stats.value = res.data
  } catch {
    /* 统计失败不打断列表 */
  }
}

const sync = async (withStats = true) => {
  await Promise.all([loadList(), withStats ? loadStats() : Promise.resolve()])
  // 闪动只保留一轮
  setTimeout(() => (flash.value = {}), 1200)
}

const startTimer = () => {
  stopTimer()
  if (!live.value) return
  timer = window.setInterval(() => sync(), interval.value)
}
const stopTimer = () => {
  if (timer !== null) {
    clearInterval(timer)
    timer = null
  }
}

watch([live, interval], () => startTimer())
watch([filterStatus, filterType, onlyToday], () => {
  page.value = 1
  sync()
})

let kwTimer: number | null = null
watch(keyword, () => {
  if (kwTimer) clearTimeout(kwTimer)
  kwTimer = window.setTimeout(() => {
    page.value = 1
    sync()
  }, 350)
})

// ---------- 详情 ----------
const detail = ref<TaskRow | null>(null)
const openDetail = async (row: TaskRow) => {
  detail.value = row
  try {
    const res: any = await api.get(`/admin/tasks/${row.id}`)
    detail.value = res.data
  } catch {
    /* 用列表里的数据兜底 */
  }
}

// ---------- 重试 ----------
const retrying = ref('')
const retry = async (row: TaskRow) => {
  const ok = await confirmDialog({
    title: '重试这个任务？',
    desc: `失败时积分已自动退还，重试不再扣费。\n任务：${row.assetName || row.assetId}`,
    confirmText: '重新入队'
  })
  if (!ok) return
  retrying.value = row.id
  try {
    await api.post(`/admin/tasks/${row.id}/retry`)
    toast.success('已重新入队')
    detail.value = null
    sync()
  } catch (e: any) {
    toast.error(e.response?.data?.message || '重试失败')
  } finally {
    retrying.value = ''
  }
}

// ---------- 展示工具 ----------
const fmtTime = (s: string | null) => {
  if (!s) return '—'
  const d = new Date(s)
  const today = new Date()
  const sameDay = d.toDateString() === today.toDateString()
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return sameDay ? `${hh}:${mm}:${ss}` : `${d.getMonth() + 1}-${d.getDate()} ${hh}:${mm}`
}

/** 任务耗时：运行中显示已耗时，结束显示总耗时 */
const cost = (r: TaskRow) => {
  if (!r.startedAt) return '—'
  const start = new Date(r.startedAt).getTime()
  const end = r.finishedAt ? new Date(r.finishedAt).getTime() : Date.now()
  const s = Math.max(0, Math.round((end - start) / 1000))
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m${String(s % 60).padStart(2, '0')}s`
}

const avgText = computed(() => {
  const ms = stats.value?.task.avgCostMs || 0
  if (!ms) return '—'
  return ms < 60000 ? `${Math.round(ms / 1000)}s` : `${Math.round(ms / 60000)}m`
})

const successRate = computed(() => {
  const t = stats.value?.task
  if (!t) return '—'
  const done = t.todayDone + t.todayFail
  if (done === 0) return '—'
  return `${Math.round((t.todayDone / done) * 1000) / 10}%`
})

const syncText = computed(() => {
  if (!lastSync.value) return '尚未同步'
  const s = Math.round((Date.now() - lastSync.value.getTime()) / 1000)
  return s < 2 ? '刚刚更新' : `${s} 秒前更新`
})

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / size)))

const goPage = (p: number) => {
  page.value = Math.min(Math.max(1, p), totalPages.value)
  sync(false)
}

onMounted(async () => {
  await sync()
  startTimer()
})
onUnmounted(stopTimer)
</script>

<template>
  <div class="tasks-page">
    <header class="page-head">
      <div>
        <h1>任务监控</h1>
        <p>所有用户的图片 / 视频生成任务，实时跟踪队列与失败原因</p>
      </div>
      <div class="live-box">
        <button class="live-toggle" :class="{ on: live }" @click="live = !live">
          <span class="pulse" :class="{ on: live }"></span>
          {{ live ? '实时刷新' : '已暂停' }}
        </button>
        <select v-model.number="interval" class="live-interval" :disabled="!live">
          <option :value="2000">2 秒</option>
          <option :value="3000">3 秒</option>
          <option :value="5000">5 秒</option>
          <option :value="10000">10 秒</option>
        </select>
        <button class="btn-refresh" :disabled="loading" @click="sync()">
          <span v-if="loading" class="spinner"></span>
          <svg v-else viewBox="0 0 24 24" fill="none" width="13" height="13">
            <path d="M4 12a8 8 0 0 1 13.7-5.6M20 12a8 8 0 0 1-13.7 5.6" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
            <path d="M18 3v4h-4M6 21v-4h4" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <span class="sync-text">{{ syncText }}</span>
      </div>
    </header>

    <!-- 统计卡 -->
    <section class="stat-row">
      <div class="stat-card">
        <span class="sc-label">排队中</span>
        <strong class="sc-val">{{ stats?.task.pending ?? '—' }}</strong>
        <span class="sc-sub">并发 {{ stats?.queue.running ?? 0 }} / {{ stats?.queue.max ?? 0 }}</span>
      </div>
      <div class="stat-card running">
        <span class="sc-label">生成中</span>
        <strong class="sc-val">{{ stats?.task.running ?? '—' }}</strong>
        <span class="sc-sub">一个任务一个协程</span>
      </div>
      <div class="stat-card ok">
        <span class="sc-label">今日成功</span>
        <strong class="sc-val">{{ stats?.task.todayDone ?? '—' }}</strong>
        <span class="sc-sub">共提交 {{ stats?.task.todayAll ?? 0 }}</span>
      </div>
      <div class="stat-card fail">
        <span class="sc-label">今日失败</span>
        <strong class="sc-val">{{ stats?.task.todayFail ?? '—' }}</strong>
        <span class="sc-sub">成功率 {{ successRate }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">平均耗时</span>
        <strong class="sc-val">{{ avgText }}</strong>
        <span class="sc-sub">近 1 小时 成 {{ stats?.task.hourDone ?? 0 }} / 败 {{ stats?.task.hourFail ?? 0 }}</span>
      </div>
    </section>

    <!-- 筛选 -->
    <section class="filter-bar">
      <div class="tabs">
        <button
          v-for="t in statusTabs"
          :key="t.value"
          class="tab"
          :class="{ on: filterStatus === t.value }"
          @click="filterStatus = t.value"
        >
          {{ t.label }}
        </button>
      </div>
      <div class="fb-right">
        <select v-model="filterType" class="sel">
          <option value="">全部类型</option>
          <option value="image">图片</option>
          <option value="video">视频</option>
        </select>
        <label class="chk">
          <input v-model="onlyToday" type="checkbox" />
          <span>仅今天</span>
        </label>
        <div class="search">
          <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
            <circle cx="11" cy="11" r="6.5" stroke="currentColor" stroke-width="1.9" />
            <path d="m16 16 4 4" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
          </svg>
          <input v-model="keyword" placeholder="用户邮箱 / 昵称 / 资产名 ID" />
        </div>
      </div>
    </section>

    <!-- 列表 -->
    <section class="table-wrap">
      <table class="task-table">
        <thead>
          <tr>
            <th style="width: 92px">状态</th>
            <th style="width: 180px">用户</th>
            <th style="width: 150px">资产</th>
            <th>提示词</th>
            <th style="width: 84px">耗时</th>
            <th style="width: 72px">积分</th>
            <th style="width: 92px">提交时间</th>
            <th style="width: 92px"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!rows.length && !loading">
            <td colspan="8" class="empty">没有符合条件的任务</td>
          </tr>
          <tr
            v-for="r in rows"
            :key="r.id"
            class="task-row"
            :class="[r.status, { flash: flash[r.id] }]"
            @click="openDetail(r)"
          >
            <td>
              <span class="status-pill" :class="statusMeta[r.status].cls">
                <span v-if="r.status === 'running' || r.status === 'pending'" class="dot"></span>
                {{ statusMeta[r.status].label }}
              </span>
            </td>
            <td>
              <div class="u-cell">
                <span class="u-name">{{ r.userNick || '未命名' }}</span>
                <span class="u-mail">{{ r.userEmail || r.userId }}</span>
              </div>
            </td>
            <td>
              <div class="a-cell">
                <span class="a-type" :class="r.assetType">{{ assetLabel[r.assetType] || r.assetType }}</span>
                <span class="a-name">{{ r.assetName || '—' }}</span>
              </div>
            </td>
            <td class="p-cell">
              <span v-if="r.errorMsg" class="err-text">{{ r.errorMsg }}</span>
              <span v-else class="prompt-text">{{ r.prompt }}</span>
            </td>
            <td class="mono">{{ cost(r) }}</td>
            <td class="mono">
              {{ r.credits }}
              <span v-if="r.creditsRefunded" class="refund">已退</span>
            </td>
            <td class="mono dim">{{ fmtTime(r.createdAt) }}</td>
            <td>
              <button
                v-if="r.status === 'failed'"
                class="retry-btn"
                :disabled="retrying === r.id"
                @click.stop="retry(r)"
              >
                重试
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- 分页 -->
    <footer class="pager">
      <span class="pg-info">共 {{ total }} 条 · 第 {{ page }} / {{ totalPages }} 页</span>
      <div class="pg-acts">
        <button class="btn-page" :disabled="page <= 1" @click="goPage(page - 1)">上一页</button>
        <button class="btn-page" :disabled="page >= totalPages" @click="goPage(page + 1)">下一页</button>
      </div>
    </footer>

    <!-- 详情抽屉 -->
    <div v-if="detail" class="drawer-mask" @click.self="detail = null">
      <aside class="drawer">
        <header class="dr-head">
          <div>
            <h3>{{ detail.assetName || detail.assetId }}</h3>
            <span class="dr-sub">
              {{ assetLabel[detail.assetType] || detail.assetType }} ·
              {{ detail.type === 'image' ? '图片' : '视频' }} ·
              {{ detail.userEmail }}
            </span>
          </div>
          <button class="dr-close" @click="detail = null">
            <svg viewBox="0 0 24 24" fill="none" width="16" height="16">
              <path d="M6 6l12 12M18 6 6 18" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
            </svg>
          </button>
        </header>

        <div class="dr-body">
          <div class="dr-row">
            <span class="dr-k">状态</span>
            <span class="status-pill" :class="statusMeta[detail.status].cls">
              {{ statusMeta[detail.status].label }}
            </span>
          </div>
          <div class="dr-row">
            <span class="dr-k">耗时</span>
            <span class="dr-v">{{ cost(detail) }}</span>
          </div>
          <div class="dr-row">
            <span class="dr-k">提交 / 完成</span>
            <span class="dr-v">{{ fmtTime(detail.createdAt) }} → {{ fmtTime(detail.finishedAt) }}</span>
          </div>
          <div class="dr-row">
            <span class="dr-k">积分</span>
            <span class="dr-v">{{ detail.credits }}{{ detail.creditsRefunded ? '（已退还）' : '' }}</span>
          </div>
          <div v-if="detail.modelId" class="dr-row">
            <span class="dr-k">模型</span>
            <span class="dr-v mono">{{ detail.modelId }}</span>
          </div>
          <div v-if="detail.externalTaskId" class="dr-row">
            <span class="dr-k">上游任务</span>
            <span class="dr-v mono">{{ detail.externalTaskId }}</span>
          </div>

          <div v-if="detail.errorMsg" class="dr-block err">
            <div class="db-title">失败原因</div>
            <p>{{ detail.errorMsg }}</p>
          </div>

          <div v-if="detail.resultUrl" class="dr-block">
            <div class="db-title">生成结果</div>
            <img v-if="detail.type === 'image'" :src="detail.resultUrl" class="dr-img" alt="result" />
            <video v-else :src="detail.resultUrl" class="dr-video" controls></video>
          </div>

          <div class="dr-block">
            <div class="db-title">提示词（实际提交）</div>
            <p class="dr-prompt">{{ detail.prompt }}</p>
          </div>

          <div v-if="detail.params" class="dr-block">
            <div class="db-title">参数</div>
            <p class="mono dr-params">{{ detail.params }}</p>
          </div>
        </div>

        <footer v-if="detail.status === 'failed'" class="dr-foot">
          <button class="btn-retry-lg" :disabled="retrying === detail.id" @click="retry(detail)">
            <span v-if="retrying === detail.id" class="spinner"></span>
            重新入队（不扣积分）
          </button>
        </footer>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.tasks-page {
  padding: 0 0 40px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* ---------- 头部 ---------- */
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  flex-wrap: wrap;
}
.page-head h1 {
  margin: 0 0 5px;
  font-size: 21px;
  font-weight: 650;
  letter-spacing: -0.01em;
}
.page-head p {
  margin: 0;
  font-size: 12.5px;
  color: var(--text-mute);
}

.live-box {
  display: flex;
  align-items: center;
  gap: 9px;
}
.live-toggle {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 32px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-dim);
  font-size: 12.5px;
  font-family: inherit;
  cursor: pointer;
  transition: all 0.22s var(--ease);
}
.live-toggle.on {
  border-color: rgba(52, 211, 153, 0.45);
  background: rgba(52, 211, 153, 0.12);
  color: #6ee7b7;
}
.pulse {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--text-mute);
}
.pulse.on {
  background: #34d399;
  box-shadow: 0 0 0 0 rgba(52, 211, 153, 0.6);
  animation: ring 1.8s ease-out infinite;
}
@keyframes ring {
  0% { box-shadow: 0 0 0 0 rgba(52, 211, 153, 0.55); }
  70% { box-shadow: 0 0 0 7px rgba(52, 211, 153, 0); }
  100% { box-shadow: 0 0 0 0 rgba(52, 211, 153, 0); }
}
.live-interval,
.sel {
  height: 32px;
  padding: 0 26px 0 10px;
  border-radius: 9px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-dim);
  font-size: 12.5px;
  font-family: inherit;
  cursor: pointer;
  appearance: none;
  background-image: linear-gradient(45deg, transparent 50%, currentColor 50%),
    linear-gradient(135deg, currentColor 50%, transparent 50%);
  background-position: calc(100% - 13px) 14px, calc(100% - 8px) 14px;
  background-size: 5px 5px, 5px 5px;
  background-repeat: no-repeat;
}
.btn-refresh {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border-radius: 9px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-dim);
  cursor: pointer;
}
.btn-refresh:hover { background: var(--surface-hover); color: var(--text); }
.sync-text { font-size: 11px; color: var(--text-mute); min-width: 62px; }

/* ---------- 统计卡 ---------- */
.stat-row {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}
.stat-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 14px 16px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
}
.stat-card::after {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--border-strong);
}
.stat-card.running::after { background: #22d3ee; }
.stat-card.ok::after { background: #34d399; }
.stat-card.fail::after { background: #fb7185; }
.sc-label { font-size: 11.5px; color: var(--text-mute); }
.sc-val {
  font-size: 24px;
  font-weight: 680;
  letter-spacing: -0.02em;
  font-variant-numeric: tabular-nums;
}
.sc-sub { font-size: 10.5px; color: var(--text-mute); }

/* ---------- 筛选 ---------- */
.filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
}
.tabs {
  display: flex;
  gap: 4px;
  padding: 3px;
  border-radius: 11px;
  border: 1px solid var(--border);
  background: var(--surface);
}
.tab {
  height: 28px;
  padding: 0 12px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-dim);
  font-size: 12.5px;
  font-family: inherit;
  cursor: pointer;
  transition: all 0.2s var(--ease);
}
.tab:hover { color: var(--text); }
.tab.on {
  background: var(--accent-soft);
  color: #cfc2ff;
}
.fb-right { display: flex; align-items: center; gap: 10px; }
.chk {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--text-dim);
  cursor: pointer;
}
.chk input { accent-color: var(--accent); }
.search {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 10px;
  border-radius: 9px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-mute);
}
.search input {
  width: 190px;
  border: none;
  background: transparent;
  color: var(--text);
  font-size: 12.5px;
  font-family: inherit;
  outline: none;
}

/* ---------- 表格 ---------- */
.table-wrap {
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
}
.task-table {
  width: 100%;
  border-collapse: collapse;
}
.task-table th {
  padding: 10px 14px;
  text-align: left;
  font-size: 11.5px;
  font-weight: 550;
  color: var(--text-mute);
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}
.task-row {
  cursor: pointer;
  transition: background 0.16s var(--ease);
}
.task-row:hover { background: var(--surface-hover); }
.task-row td {
  padding: 10px 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.045);
  font-size: 12.5px;
  vertical-align: middle;
}
.task-row:last-child td { border-bottom: none; }
/* 最后一列放操作按钮，右侧留足内边距，避免按钮贴边被切 */
.task-row td:last-child { padding-right: 16px; }
.task-table th:last-child { padding-right: 16px; }
.task-row.flash { animation: rowFlash 1.1s var(--ease); }
@keyframes rowFlash {
  0% { background: rgba(124, 92, 255, 0.2); }
  100% { background: transparent; }
}
.empty {
  padding: 40px 0 !important;
  text-align: center;
  color: var(--text-mute);
}

.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 9px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 550;
  white-space: nowrap;
}
.status-pill.pending { background: rgba(251, 191, 36, 0.14); color: #fbbf24; }
.status-pill.running { background: rgba(34, 211, 238, 0.14); color: #22d3ee; }
.status-pill.ok { background: rgba(52, 211, 153, 0.14); color: #34d399; }
.status-pill.fail { background: rgba(251, 113, 133, 0.14); color: #fb7185; }
.status-pill .dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: currentColor;
  animation: blink 1.4s ease-in-out infinite;
}
@keyframes blink {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.25; }
}

.u-cell, .a-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.u-name { font-size: 12.5px; color: var(--text); }
.u-mail, .a-name {
  font-size: 10.5px;
  color: var(--text-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.a-type {
  align-self: flex-start;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 10px;
  background: rgba(255, 255, 255, 0.07);
  color: var(--text-dim);
}
.a-type.character { color: #a48bff; }
.a-type.scene { color: #22d3ee; }
.a-type.item { color: #fbbf24; }
.a-type.frame { color: #ff8fb0; }
.p-cell { max-width: 0; }
.prompt-text, .err-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--text-mute);
}
.err-text { color: #fb7185; }
.mono { font-variant-numeric: tabular-nums; font-size: 12px; }
.dim { color: var(--text-mute); }
.refund {
  margin-left: 4px;
  padding: 0 5px;
  border-radius: 4px;
  font-size: 9.5px;
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.07);
}
.retry-btn {
  padding: 3px 10px;
  border-radius: 7px;
  border: 1px solid rgba(124, 92, 255, 0.4);
  background: var(--accent-soft);
  color: #cfc2ff;
  font-size: 11.5px;
  font-family: inherit;
  cursor: pointer;
}
.retry-btn:hover { background: rgba(124, 92, 255, 0.24); }

/* ---------- 分页 ---------- */
.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.pg-info { font-size: 12px; color: var(--text-mute); }
.pg-acts { display: flex; gap: 8px; }
.btn-page {
  height: 30px;
  padding: 0 13px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-dim);
  font-size: 12.5px;
  font-family: inherit;
  cursor: pointer;
}
.btn-page:hover:not(:disabled) { background: var(--surface-hover); color: var(--text); }
.btn-page:disabled { opacity: 0.4; cursor: not-allowed; }

/* ---------- 抽屉 ---------- */
.drawer-mask {
  position: fixed;
  inset: 0;
  z-index: 9000;
  display: flex;
  justify-content: flex-end;
  background: rgba(4, 4, 9, 0.6);
  backdrop-filter: blur(4px);
}
.drawer {
  width: 460px;
  max-width: 92vw;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #12121a;
  border-left: 1px solid var(--border-strong);
  animation: slideIn 0.24s var(--ease) both;
}
@keyframes slideIn {
  from { transform: translateX(24px); }
  to { transform: none; }
}
.dr-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--border);
}
.dr-head h3 { margin: 0 0 4px; font-size: 15.5px; font-weight: 620; }
.dr-sub { font-size: 11.5px; color: var(--text-mute); }
.dr-close {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  border: none;
  background: transparent;
  color: var(--text-mute);
  cursor: pointer;
}
.dr-close:hover { background: var(--surface-hover); color: var(--text); }
.dr-body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.dr-row {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12.5px;
}
.dr-k { width: 78px; flex-shrink: 0; color: var(--text-mute); }
.dr-v { color: var(--text-dim); word-break: break-all; }
.dr-block {
  margin-top: 6px;
  padding: 11px 13px;
  border-radius: 11px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.025);
}
.dr-block.err { border-color: rgba(251, 113, 133, 0.32); background: rgba(251, 113, 133, 0.08); }
.db-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-mute);
  margin-bottom: 7px;
  letter-spacing: 0.02em;
}
.dr-block p { margin: 0; font-size: 12px; line-height: 1.7; color: var(--text-dim); }
.dr-block.err p { color: #fb7185; }
.dr-prompt { white-space: pre-wrap; word-break: break-word; }
.dr-params { font-size: 11px; color: var(--text-mute); word-break: break-all; }
.dr-img { width: 100%; border-radius: 9px; border: 1px solid var(--border); }
.dr-video { width: 100%; border-radius: 9px; border: 1px solid var(--border); }
.dr-foot {
  padding: 14px 20px;
  border-top: 1px solid var(--border);
}
.btn-retry-lg {
  width: 100%;
  height: 38px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: 10px;
  border: 1px solid rgba(124, 92, 255, 0.45);
  background: var(--accent-soft);
  color: #cfc2ff;
  font-size: 13px;
  font-family: inherit;
  font-weight: 550;
  cursor: pointer;
}
.btn-retry-lg:hover:not(:disabled) { background: rgba(124, 92, 255, 0.26); }
.btn-retry-lg:disabled { opacity: 0.55; cursor: not-allowed; }

@media (max-width: 1200px) {
  .stat-row { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 860px) {
  .stat-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .search input { width: 120px; }
}
</style>
