<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import api from '../api'
import { useUserStore } from '../stores/user'

const store = useUserStore()
const loading = ref(true)
const list = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const size = 20

// 账户累计（来自接口，不是当前页求和）
const account = ref({ balance: 0, totalIn: 0, totalOut: 0 })

// 类型筛选
const filterType = ref('all')
const typeOptions = [
  { key: 'all', label: '全部' },
  { key: 'in', label: '收入' },
  { key: 'out', label: '支出' }
]

const typeMap: Record<string, { label: string; cls: string }> = {
  register: { label: '注册赠送', cls: 'in' },
  recharge: { label: '充值', cls: 'in' },
  refund: { label: '退还', cls: 'in' },
  identify: { label: '资产识别', cls: 'out' },
  image: { label: '图像生成', cls: 'out' },
  video: { label: '视频生成', cls: 'out' }
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / size)))

// 前端过滤当前页
const filtered = computed(() => {
  if (filterType.value === 'all') return list.value
  const wantIn = filterType.value === 'in'
  return list.value.filter((t) => (t.amount > 0) === wantIn)
})

const load = async () => {
  loading.value = true
  try {
    const [tx, acc]: any = await Promise.all([
      api.get('/me/points/transactions', { params: { page: page.value, size } }),
      api.get('/me/points')
    ])
    list.value = tx.data.list || []
    total.value = tx.data.total || 0
    account.value = {
      balance: acc.data.balance,
      totalIn: acc.data.totalIn,
      totalOut: acc.data.totalOut
    }
    store.points = acc.data.balance
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const goPage = (p: number) => {
  if (p < 1 || p > totalPages.value) return
  page.value = p
  load()
}

const fmtTime = (iso: string) => {
  const d = new Date(iso)
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

onMounted(load)
</script>

<template>
  <div class="points-page">
    <div class="page-head">
      <div>
        <h1>积分明细</h1>
        <p>查看积分收支与消耗记录</p>
      </div>
    </div>

    <div class="layout">
      <!-- 左：余额与汇总 -->
      <aside class="side">
        <div class="balance-card">
          <span class="bc-label">当前余额</span>
          <div class="bc-value">
            <span class="num">{{ account.balance }}</span>
            <span class="unit">积分</span>
          </div>
          <div class="bc-bar">
            <div class="bc-bar-track">
              <div
                class="bc-bar-fill"
                :style="{
                  width:
                    account.totalIn > 0
                      ? Math.min(100, Math.round((account.totalOut / account.totalIn) * 100)) + '%'
                      : '0%'
                }"
              ></div>
            </div>
            <span class="bc-bar-hint">已消耗占比</span>
          </div>
        </div>

        <div class="sum-card">
          <div class="sum-row">
            <span class="sum-label">累计获得</span>
            <span class="sum-value in">+{{ account.totalIn }}</span>
          </div>
          <div class="sum-row">
            <span class="sum-label">累计消耗</span>
            <span class="sum-value out">{{ account.totalOut }}</span>
          </div>
          <div class="sum-row">
            <span class="sum-label">记录条数</span>
            <span class="sum-value">{{ total }}</span>
          </div>
        </div>

        <div class="tip-card">
          <div class="tip-title">积分用途</div>
          <div class="tip-item">
            <span class="tip-dot role"></span>资产识别 <b>14</b> / 次
          </div>
          <div class="tip-item">
            <span class="tip-dot image"></span>单张图片 <b>2</b> / 张
          </div>
          <div class="tip-item">
            <span class="tip-dot video"></span>单条视频 <b>35</b> / 条
          </div>
          <div class="tip-foot">生成失败会自动退还</div>
        </div>
      </aside>

      <!-- 右：流水列表 -->
      <div class="main">
        <div class="tx-card">
          <div class="tx-head">
            <div class="tx-tabs">
              <button
                v-for="t in typeOptions"
                :key="t.key"
                class="tx-tab"
                :class="{ on: filterType === t.key }"
                @click="filterType = t.key"
              >
                {{ t.label }}
              </button>
            </div>
            <span class="tx-count">{{ total }} 条记录</span>
          </div>

          <div v-if="loading" class="tx-loading">加载中…</div>

          <div v-else-if="filtered.length === 0" class="tx-empty">
            {{ filterType === 'all' ? '还没有积分记录' : '当前页没有这类记录' }}
          </div>

          <div v-else class="tx-list">
            <!-- 表头 -->
            <div class="tx-row head">
              <span class="col-type">类型</span>
              <span class="col-reason">说明</span>
              <span class="col-time">时间</span>
              <span class="col-amount">变动</span>
              <span class="col-balance">余额</span>
            </div>

            <div v-for="t in filtered" :key="t.id" class="tx-row">
              <div class="col-type">
                <span class="tx-tag" :class="typeMap[t.type]?.cls || 'out'">
                  {{ typeMap[t.type]?.label || t.type }}
                </span>
              </div>
              <div class="col-reason">{{ t.reason || '—' }}</div>
              <div class="col-time">{{ fmtTime(t.createdAt) }}</div>
              <div class="col-amount">
                <span class="tx-amount" :class="t.amount > 0 ? 'in' : 'out'">
                  {{ t.amount > 0 ? '+' : '' }}{{ t.amount }}
                </span>
              </div>
              <div class="col-balance">{{ t.balance }}</div>
            </div>
          </div>

          <!-- 分页 -->
          <div v-if="totalPages > 1" class="pager">
            <span class="pg-info">第 {{ page }} / {{ totalPages }} 页</span>
            <div class="pg-btns">
              <button class="pg" :disabled="page === 1" @click="goPage(page - 1)">上一页</button>
              <button class="pg" :disabled="page === totalPages" @click="goPage(page + 1)">下一页</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.points-page {
  width: 100%;
  padding-top: 20px;
}

.page-head { margin-bottom: 22px; }
.page-head h1 { font-size: 26px; font-weight: 690; letter-spacing: -0.03em; }
.page-head p { font-size: 13.5px; color: var(--text-mute); margin-top: 3px; }

.layout {
  display: grid;
  grid-template-columns: 296px minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}

/* ---------- 左侧 ---------- */
.side {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.balance-card {
  padding: 22px 20px;
  border-radius: var(--r-xl);
  border: 1px solid var(--border);
  background: linear-gradient(155deg, rgba(124, 92, 255, 0.13), rgba(255, 77, 148, 0.055));
}
.bc-label { font-size: 11.5px; color: var(--text-mute); }
.bc-value {
  display: flex;
  align-items: baseline;
  gap: 7px;
  margin: 4px 0 16px;
}
.bc-value .num {
  font-size: 42px;
  font-weight: 720;
  letter-spacing: -0.045em;
  background: var(--grad);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
}
.bc-value .unit { font-size: 12.5px; color: var(--text-mute); }

.bc-bar-track {
  height: 5px;
  border-radius: 99px;
  background: rgba(255, 255, 255, 0.07);
  overflow: hidden;
}
.bc-bar-fill {
  height: 100%;
  border-radius: 99px;
  background: var(--grad);
  transition: width 0.5s var(--ease);
}
.bc-bar-hint {
  display: block;
  font-size: 10.5px;
  color: var(--text-mute);
  margin-top: 6px;
}

.sum-card {
  padding: 15px 17px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
}
.sum-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 7px 0;
  border-bottom: 1px dashed rgba(255, 255, 255, 0.055);
}
.sum-row:last-child { border-bottom: none; }
.sum-label { font-size: 11.5px; color: var(--text-mute); }
.sum-value { font-size: 13.5px; font-weight: 640; }
.sum-value.in { color: var(--ok); }
.sum-value.out { color: var(--err); }

.tip-card {
  padding: 15px 17px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
}
.tip-title {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--text-mute);
  letter-spacing: 0.03em;
  margin-bottom: 10px;
}
.tip-item {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11.5px;
  color: var(--text-dim);
  padding: 4px 0;
}
.tip-item b { color: var(--text); font-weight: 620; margin-left: auto; }
.tip-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
.tip-dot.role { background: #7c5cff; }
.tip-dot.image { background: #22d3ee; }
.tip-dot.video { background: #fbbf24; }
.tip-foot {
  margin-top: 10px;
  padding-top: 9px;
  border-top: 1px dashed rgba(255, 255, 255, 0.055);
  font-size: 10.5px;
  color: var(--text-mute);
}

/* ---------- 右侧 ---------- */
.main { min-width: 0; }

.tx-card {
  border-radius: var(--r-xl);
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
}
.tx-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 13px 20px;
  border-bottom: 1px solid var(--border);
}

.tx-tabs { display: flex; gap: 3px; }
.tx-tab {
  padding: 5px 13px;
  border-radius: 8px;
  font-size: 12.5px;
  color: var(--text-mute);
  transition: all 0.2s var(--ease);
}
.tx-tab:hover { color: var(--text); background: var(--surface-hover); }
.tx-tab.on {
  color: #c4b5fd;
  background: var(--accent-soft);
  font-weight: 550;
}
.tx-count { font-size: 11.5px; color: var(--text-mute); }

.tx-loading, .tx-empty {
  padding: 70px 20px;
  text-align: center;
  color: var(--text-mute);
  font-size: 13px;
}

/* 流水行 */
.tx-row {
  display: grid;
  grid-template-columns: 118px minmax(0, 1fr) 116px 92px 88px;
  align-items: center;
  gap: 14px;
  padding: 12px 20px;
  border-bottom: 1px solid var(--border);
  transition: background 0.18s var(--ease);
}
.tx-row:last-child { border-bottom: none; }
.tx-row:not(.head):hover { background: rgba(255, 255, 255, 0.018); }

.tx-row.head {
  padding-top: 9px;
  padding-bottom: 9px;
  background: rgba(255, 255, 255, 0.022);
  font-size: 11px;
  font-weight: 560;
  color: var(--text-mute);
  letter-spacing: 0.02em;
}

.col-type { min-width: 0; }
.col-reason {
  font-size: 12.5px;
  color: var(--text-dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.col-time { font-size: 11.5px; color: var(--text-mute); }
.col-amount { text-align: right; }
.col-balance {
  text-align: right;
  font-size: 12px;
  font-family: 'SF Mono', Menlo, monospace;
  color: var(--text-mute);
}
.tx-row.head .col-amount,
.tx-row.head .col-balance { text-align: right; }

.tx-tag {
  display: inline-block;
  padding: 2px 9px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 540;
  white-space: nowrap;
}
.tx-tag.in { color: var(--ok); background: rgba(52, 211, 153, 0.11); }
.tx-tag.out { color: var(--text-dim); background: rgba(255, 255, 255, 0.055); }

.tx-amount { font-size: 13.5px; font-weight: 650; letter-spacing: -0.01em; }
.tx-amount.in { color: var(--ok); }
.tx-amount.out { color: var(--text-dim); }

/* 分页 */
.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 13px 20px;
  border-top: 1px solid var(--border);
}
.pg-info { font-size: 11.5px; color: var(--text-mute); }
.pg-btns { display: flex; gap: 8px; }
.pg {
  padding: 6px 14px;
  border-radius: 8px;
  font-size: 12px;
  color: var(--text-dim);
  border: 1px solid var(--border);
  transition: all 0.2s var(--ease);
}
.pg:hover:not(:disabled) { background: var(--surface-hover); color: var(--text); }
.pg:disabled { opacity: 0.4; cursor: not-allowed; }

@media (max-width: 1100px) {
  .layout { grid-template-columns: 1fr; }
  .side {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 12px;
    align-items: start;
  }
}

@media (max-width: 780px) {
  .tx-row {
    grid-template-columns: 100px minmax(0, 1fr) 80px;
    gap: 10px;
  }
  .col-time, .col-balance { display: none; }
  .tx-row.head .col-time,
  .tx-row.head .col-balance { display: none; }
}
</style>
