<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import api from '../../api'
import { toast } from '../../composables/useToast'
import { confirmDialog } from '../../composables/useConfirm'

interface UserRow {
  id: string
  email: string
  nickname: string
  avatar: string
  status: string
  balance: number
  totalIn: number
  totalOut: number
  projectCount: number
  lastLoginAt: string | null
  createdAt: string
}

const list = ref<UserRow[]>([])
const total = ref(0)
const page = ref(1)
const size = ref(20)
const keyword = ref('')
const status = ref('all')
const loading = ref(true)
const stats = ref<any>({ total: 0, active: 0, banned: 0, totalBalance: 0, newToday: 0 })

// 调整积分弹窗
const adjust = ref({ open: false, user: null as UserRow | null, amount: 10, reason: '', submitting: false })

// 详情抽屉
const detail = ref<{ open: boolean; loading: boolean; data: any }>({ open: false, loading: false, data: null })

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / size.value)))

const load = async () => {
  loading.value = true
  try {
    const res: any = await api.get('/admin/users', {
      params: { page: page.value, size: size.value, keyword: keyword.value, status: status.value }
    })
    list.value = res.data?.list || []
    total.value = res.data?.total || 0
    stats.value = res.data?.stats || stats.value
  } catch (e: any) {
    toast.error('加载失败', e.response?.data?.message || e.message)
  } finally {
    loading.value = false
  }
}

const search = () => {
  page.value = 1
  load()
}

const goto = (p: number) => {
  if (p < 1 || p > pageCount.value) return
  page.value = p
  load()
}

const fmtDate = (s?: string | null) => {
  if (!s) return '—'
  const d = new Date(s)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const fmtTime = (s?: string | null) => {
  if (!s) return '从未登录'
  const d = new Date(s)
  const diff = Date.now() - d.getTime()
  const min = Math.floor(diff / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  const h = Math.floor(min / 60)
  if (h < 24) return `${h} 小时前`
  const day = Math.floor(h / 24)
  if (day < 30) return `${day} 天前`
  return fmtDate(s)
}

const initial = (u: UserRow) => (u.avatar || u.nickname?.[0] || u.email[0] || '?').toUpperCase()

const openAdjust = (u: UserRow) => {
  adjust.value = { open: true, user: u, amount: 10, reason: '', submitting: false }
}

const submitAdjust = async () => {
  const u = adjust.value.user
  if (!u) return
  const amount = Number(adjust.value.amount)
  if (!amount) {
    toast.warning('积分数不能为 0', '正数为增加，负数为扣减')
    return
  }
  adjust.value.submitting = true
  try {
    const res: any = await api.post(`/admin/users/${u.id}/points`, {
      amount,
      reason: adjust.value.reason
    })
    toast.success(
      amount > 0 ? `已为「${u.nickname || u.email}」增加 ${amount} 积分` : `已扣减 ${-amount} 积分`,
      `当前余额 ${res.data?.balance ?? '—'}`
    )
    adjust.value.open = false
    await load()
  } catch (e: any) {
    toast.error('调整失败', e.response?.data?.message || e.message)
  } finally {
    adjust.value.submitting = false
  }
}

const toggleBan = async (u: UserRow) => {
  const banning = u.status !== 'banned'
  const ok = await confirmDialog({
    title: banning ? `封禁「${u.nickname || u.email}」？` : `解封「${u.nickname || u.email}」？`,
    desc: banning
      ? '封禁后该用户无法登录，已登录的会话在下次请求时失效。已有作品与积分不会删除。'
      : '解封后该用户可以正常登录并继续创作。',
    confirmText: banning ? '封禁' : '解封',
    danger: banning
  })
  if (!ok) return
  try {
    await api.put(`/admin/users/${u.id}/status`, { status: banning ? 'banned' : 'active' })
    toast.success(banning ? '已封禁' : '已解封', u.nickname || u.email)
    await load()
  } catch (e: any) {
    toast.error('操作失败', e.response?.data?.message || e.message)
  }
}

const openDetail = async (u: UserRow) => {
  detail.value = { open: true, loading: true, data: null }
  try {
    const res: any = await api.get(`/admin/users/${u.id}`)
    detail.value.data = res.data
  } catch (e: any) {
    toast.error('加载详情失败', e.response?.data?.message || e.message)
    detail.value.open = false
  } finally {
    detail.value.loading = false
  }
}

onMounted(load)
</script>

<template>
  <div class="users-page">
    <div class="page-head">
      <div>
        <h1>用户管理</h1>
        <p>查看注册用户、调整积分、封禁违规账号</p>
      </div>
      <button class="btn btn-ghost btn-sm" :disabled="loading" @click="load">刷新</button>
    </div>

    <!-- 统计卡 -->
    <div class="stat-grid">
      <div class="stat">
        <div class="st-label">总用户</div>
        <div class="st-value">{{ stats.total || 0 }}</div>
        <div class="st-sub">今日新增 {{ stats.newToday || 0 }}</div>
      </div>
      <div class="stat">
        <div class="st-label">活跃</div>
        <div class="st-value ok">{{ stats.active || 0 }}</div>
        <div class="st-sub">可正常登录</div>
      </div>
      <div class="stat">
        <div class="st-label">已封禁</div>
        <div class="st-value bad">{{ stats.banned || 0 }}</div>
        <div class="st-sub">被限制登录</div>
      </div>
      <div class="stat">
        <div class="st-label">平台积分总量</div>
        <div class="st-value">{{ stats.totalBalance || 0 }}</div>
        <div class="st-sub">所有用户余额之和</div>
      </div>
    </div>

    <!-- 工具条 -->
    <div class="toolbar">
      <div class="search">
        <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
          <circle cx="11" cy="11" r="6.5" stroke="currentColor" stroke-width="1.8" />
          <path d="m16 16 4 4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
        </svg>
        <input
          v-model="keyword"
          class="search-input"
          placeholder="搜索邮箱或昵称"
          @keyup.enter="search"
        />
      </div>
      <select v-model="status" class="field-sm" @change="search">
        <option value="all">全部状态</option>
        <option value="active">正常</option>
        <option value="banned">已封禁</option>
      </select>
      <button class="btn btn-primary btn-sm" @click="search">查询</button>
      <span class="tb-hint">共 {{ total }} 位用户</span>
    </div>

    <!-- 表格 -->
    <div class="table-wrap">
      <div class="thead">
        <span>用户</span>
        <span>状态</span>
        <span class="num">积分余额</span>
        <span class="num">累计消耗</span>
        <span class="num">项目</span>
        <span>注册</span>
        <span>最近登录</span>
        <span class="ops">操作</span>
      </div>

      <div v-if="loading" class="empty-row">加载中…</div>
      <div v-else-if="list.length === 0" class="empty-row">没有匹配的用户</div>

      <div v-for="u in list" v-else :key="u.id" class="trow" @click="openDetail(u)">
        <div class="cell-user">
          <div class="avatar" :class="{ img: !!u.avatar }">
            <img v-if="u.avatar && u.avatar.startsWith('http')" :src="u.avatar" :alt="u.nickname" />
            <template v-else>{{ initial(u) }}</template>
          </div>
          <div class="u-meta">
            <div class="u-name">{{ u.nickname || '未命名' }}</div>
            <div class="u-email">{{ u.email }}</div>
          </div>
        </div>
        <div>
          <span class="pill" :class="u.status === 'banned' ? 'bad' : 'ok'">
            {{ u.status === 'banned' ? '已封禁' : '正常' }}
          </span>
        </div>
        <div class="num strong">{{ u.balance }}</div>
        <div class="num dim">{{ u.totalOut }}</div>
        <div class="num dim">{{ u.projectCount }}</div>
        <div class="dim">{{ fmtDate(u.createdAt) }}</div>
        <div class="dim">{{ fmtTime(u.lastLoginAt) }}</div>
        <div class="ops" @click.stop>
          <button class="act accent" @click="openAdjust(u)">调整积分</button>
          <button class="act" :class="{ danger: u.status !== 'banned' }" @click="toggleBan(u)">
            {{ u.status === 'banned' ? '解封' : '封禁' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 分页 -->
    <div v-if="pageCount > 1" class="pager">
      <button class="pg" :disabled="page === 1" @click="goto(page - 1)">上一页</button>
      <span class="pg-info">{{ page }} / {{ pageCount }}</span>
      <button class="pg" :disabled="page === pageCount" @click="goto(page + 1)">下一页</button>
    </div>

    <!-- 调整积分 -->
    <Teleport to="body">
      <div v-if="adjust.open" class="mask" @click.self="adjust.open = false">
        <div class="dialog">
          <div class="dlg-head">
            <h3>调整积分</h3>
            <p>{{ adjust.user?.nickname || adjust.user?.email }} · 当前余额 {{ adjust.user?.balance }}</p>
          </div>
          <div class="dlg-body">
            <label>变动数量</label>
            <div class="amount-row">
              <button class="step" @click="adjust.amount = Number(adjust.amount) - 10">−10</button>
              <input v-model.number="adjust.amount" class="field" type="number" />
              <button class="step" @click="adjust.amount = Number(adjust.amount) + 10">+10</button>
            </div>
            <div class="quick">
              <button v-for="v in [10, 50, 100, 500]" :key="v" class="chip" @click="adjust.amount = v">+{{ v }}</button>
              <button v-for="v in [-10, -50]" :key="v" class="chip neg" @click="adjust.amount = v">{{ v }}</button>
            </div>
            <label>原因</label>
            <input v-model="adjust.reason" class="field" placeholder="如：活动补偿 / 违规扣除" />
            <p class="dlg-hint">正数为增加，负数为扣减；扣减不能超过当前余额。操作会写入积分流水。</p>
          </div>
          <div class="dlg-foot">
            <button class="btn btn-ghost btn-sm" @click="adjust.open = false">取消</button>
            <button class="btn btn-primary btn-sm" :disabled="adjust.submitting" @click="submitAdjust">
              {{ adjust.submitting ? '提交中' : '确认调整' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 用户详情抽屉 -->
    <Teleport to="body">
      <div v-if="detail.open" class="mask" @click.self="detail.open = false">
        <div class="drawer">
          <div class="dlg-head">
            <h3>{{ detail.data?.user?.nickname || '用户详情' }}</h3>
            <p>{{ detail.data?.user?.email }}</p>
            <button class="close" @click="detail.open = false">
              <svg viewBox="0 0 24 24" fill="none" width="16" height="16">
                <path d="M6 6l12 12M18 6 6 18" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
              </svg>
            </button>
          </div>

          <div v-if="detail.loading" class="empty-row">加载中…</div>
          <div v-else-if="detail.data" class="drawer-body">
            <div class="d-stats">
              <div class="d-stat">
                <span class="d-label">余额</span>
                <span class="d-value">{{ detail.data.points?.balance ?? 0 }}</span>
              </div>
              <div class="d-stat">
                <span class="d-label">累计获得</span>
                <span class="d-value">{{ detail.data.points?.totalIn ?? 0 }}</span>
              </div>
              <div class="d-stat">
                <span class="d-label">累计消耗</span>
                <span class="d-value">{{ detail.data.points?.totalOut ?? 0 }}</span>
              </div>
              <div class="d-stat">
                <span class="d-label">项目数</span>
                <span class="d-value">{{ detail.data.projectTotal ?? 0 }}</span>
              </div>
            </div>

            <div class="d-sec-title">项目</div>
            <div v-if="detail.data.projects?.length" class="d-list">
              <div v-for="p in detail.data.projects" :key="p.id" class="d-item">
                <span class="d-item-main">{{ p.name }}</span>
                <span class="d-item-sub">{{ fmtDate(p.createdAt) }}</span>
              </div>
            </div>
            <div v-else class="d-empty">还没有创建项目</div>

            <div class="d-sec-title">最近积分流水</div>
            <div v-if="detail.data.transactions?.length" class="d-list">
              <div v-for="t in detail.data.transactions" :key="t.id" class="d-item">
                <span class="d-item-main">{{ t.reason || t.type }}</span>
                <span class="d-item-amt" :class="t.amount > 0 ? 'in' : 'out'">
                  {{ t.amount > 0 ? '+' : '' }}{{ t.amount }}
                </span>
              </div>
            </div>
            <div v-else class="d-empty">暂无积分记录</div>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.users-page { width: 100%; }

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}
.page-head h1 { font-size: 20px; font-weight: 650; letter-spacing: -0.02em; }
.page-head p { margin-top: 4px; font-size: 12.5px; color: var(--text-mute); }

/* 统计卡 */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-bottom: 18px;
}
.stat {
  padding: 14px 16px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
}
.st-label { font-size: 11.5px; color: var(--text-mute); }
.st-value { margin-top: 6px; font-size: 24px; font-weight: 660; letter-spacing: -0.02em; }
.st-value.ok { color: var(--ok); }
.st-value.bad { color: var(--err); }
.st-sub { margin-top: 3px; font-size: 10.5px; color: var(--text-mute); }

/* 工具条 */
.toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.search {
  display: flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 280px;
  height: 34px;
  padding: 0 11px;
  border-radius: 9px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.03);
  color: var(--text-mute);
}
.search-input {
  flex: 1;
  border: 0;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 12.5px;
}
.field-sm {
  height: 34px;
  padding: 0 10px;
  border-radius: 9px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.03);
  color: var(--text-dim);
  font-size: 12.5px;
  outline: none;
}
.tb-hint { margin-left: auto; font-size: 11.5px; color: var(--text-mute); }

/* 表格 */
.table-wrap {
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.014);
  overflow: hidden;
}
.thead,
.trow {
  display: grid;
  grid-template-columns: 1.35fr 76px 92px 92px 60px 96px 108px 168px;
  gap: 12px;
  align-items: center;
  padding: 11px 16px;
}
.thead {
  font-size: 10.5px;
  color: var(--text-dim);
  letter-spacing: 0.02em;
  border-bottom: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.016);
}
.trow {
  border-bottom: 1px solid rgba(255, 255, 255, 0.045);
  font-size: 12.5px;
  cursor: pointer;
  transition: background 0.2s var(--ease);
}
.trow:last-child { border-bottom: 0; }
.trow:hover { background: var(--surface-hover); }

.cell-user { display: flex; align-items: center; gap: 10px; min-width: 0; }
.avatar {
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  border-radius: 9px;
  display: grid;
  place-items: center;
  font-size: 12px;
  font-weight: 640;
  color: #ddd3ff;
  background: linear-gradient(140deg, #7c5cff, #4c2fd6);
  overflow: hidden;
}
.avatar.img { background: #14141d; }
.avatar img { width: 100%; height: 100%; object-fit: cover; }
.u-meta { min-width: 0; }
.u-name {
  font-size: 12.5px;
  font-weight: 550;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.u-email {
  font-size: 10.5px;
  color: var(--text-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.num { text-align: right; font-variant-numeric: tabular-nums; }
.num.strong { font-weight: 620; color: var(--text); }
.num.dim, .dim { color: var(--text-dim); font-size: 11.5px; }
.ops { display: flex; gap: 4px; justify-content: flex-end; }

.act {
  height: 26px;
  padding: 0 9px;
  border-radius: 6px;
  font-size: 11.5px;
  color: var(--text-dim);
  border: 1px solid transparent;
  transition: all 0.2s var(--ease);
  white-space: nowrap;
}
.act:hover { background: var(--surface-hover); color: var(--text); border-color: var(--border); }
.act.accent { color: #c4b5fd; background: rgba(124, 92, 255, 0.12); border-color: rgba(124, 92, 255, 0.26); }
.act.accent:hover { background: rgba(124, 92, 255, 0.2); border-color: rgba(124, 92, 255, 0.45); }
.act.danger:hover { color: var(--err); border-color: rgba(251, 113, 133, 0.3); }

.pill {
  display: inline-block;
  font-size: 10px;
  padding: 1.5px 8px;
  border-radius: 99px;
  border: 1px solid transparent;
}
.pill.ok { color: #6ee7b7; background: rgba(52, 211, 153, 0.1); border-color: rgba(52, 211, 153, 0.28); }
.pill.bad { color: #fda4b3; background: rgba(251, 113, 133, 0.1); border-color: rgba(251, 113, 133, 0.28); }

.empty-row { padding: 40px; text-align: center; font-size: 12.5px; color: var(--text-mute); }

/* 分页 */
.pager { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 14px; }
.pg {
  height: 30px;
  padding: 0 12px;
  border-radius: 8px;
  font-size: 12px;
  color: var(--text-dim);
  border: 1px solid var(--border);
  background: var(--surface);
}
.pg:disabled { opacity: 0.4; cursor: not-allowed; }
.pg-info { font-size: 11.5px; color: var(--text-mute); }

/* 弹窗 / 抽屉 */
.mask {
  position: fixed;
  inset: 0;
  z-index: 8800;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(4, 4, 9, 0.66);
  backdrop-filter: blur(6px);
}

.dialog {
  width: 100%;
  max-width: 400px;
  border-radius: 18px;
  background: #15151e;
  border: 1px solid var(--border-strong);
  box-shadow: 0 32px 80px -28px rgba(0, 0, 0, 0.95);
  animation: pop 0.24s var(--ease) both;
}
/* 不碰 opacity：后台标签页 rAF 被节流时动画会冻在中途，弹窗会停在半透明 */
@keyframes pop {
  from { transform: translateY(10px) scale(0.97); }
  to { transform: none; }
}
.dlg-head { padding: 18px 20px 12px; position: relative; }
.dlg-head h3 { font-size: 15px; font-weight: 630; }
.dlg-head p { margin-top: 3px; font-size: 11.5px; color: var(--text-mute); }
.dlg-body { padding: 4px 20px 0; }
.dlg-body label { display: block; font-size: 11.5px; color: var(--text-dim); margin: 12px 0 6px; }
.amount-row { display: flex; gap: 8px; align-items: center; }
.step {
  width: 46px;
  height: 34px;
  border-radius: 9px;
  font-size: 12px;
  color: var(--text-dim);
  border: 1px solid var(--border);
  background: var(--surface);
}
.step:hover { color: var(--text); border-color: var(--border-strong); }
.field {
  width: 100%;
  height: 34px;
  padding: 0 11px;
  border-radius: 9px;
  font-size: 13px;
  color: var(--text);
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--border);
  outline: none;
  text-align: center;
}
.field:focus { border-color: rgba(124, 92, 255, 0.5); }
.dlg-body .field + .field { text-align: left; }
.quick { display: flex; gap: 6px; margin-top: 8px; flex-wrap: wrap; }
.chip {
  height: 24px;
  padding: 0 9px;
  border-radius: 99px;
  font-size: 11px;
  color: #c4b5fd;
  background: rgba(124, 92, 255, 0.1);
  border: 1px solid rgba(124, 92, 255, 0.24);
}
.chip.neg { color: #fda4b3; background: rgba(251, 113, 133, 0.1); border-color: rgba(251, 113, 133, 0.24); }
.dlg-hint { margin-top: 12px; font-size: 10.5px; line-height: 1.55; color: var(--text-mute); }
.dlg-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 16px 20px 18px;
}

.drawer {
  width: 100%;
  max-width: 480px;
  max-height: 84vh;
  display: flex;
  flex-direction: column;
  border-radius: 18px;
  background: #15151e;
  border: 1px solid var(--border-strong);
  box-shadow: 0 32px 80px -28px rgba(0, 0, 0, 0.95);
  animation: pop 0.24s var(--ease) both;
}
.drawer .dlg-head { border-bottom: 1px solid var(--border); }
.close {
  position: absolute;
  top: 14px;
  right: 14px;
  width: 26px;
  height: 26px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  color: var(--text-mute);
}
.close:hover { color: var(--text); background: var(--surface-hover); }
.drawer-body { padding: 14px 20px 20px; overflow-y: auto; }

.d-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; }
.d-stat {
  padding: 10px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.025);
  border: 1px solid var(--border);
  text-align: center;
}
.d-label { display: block; font-size: 10px; color: var(--text-mute); }
.d-value { display: block; margin-top: 4px; font-size: 16px; font-weight: 620; }

.d-sec-title {
  margin: 16px 0 8px;
  font-size: 11.5px;
  font-weight: 550;
  color: var(--text-dim);
}
.d-list { display: flex; flex-direction: column; gap: 2px; }
.d-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 7px 9px;
  border-radius: 8px;
  font-size: 12px;
}
.d-item:hover { background: var(--surface); }
.d-item-main { color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.d-item-sub { font-size: 10.5px; color: var(--text-mute); flex-shrink: 0; }
.d-item-amt { font-weight: 600; font-variant-numeric: tabular-nums; }
.d-item-amt.in { color: var(--ok); }
.d-item-amt.out { color: var(--warn); }
.d-empty { padding: 14px; text-align: center; font-size: 11.5px; color: var(--text-mute); }

@media (max-width: 1100px) {
  .thead, .trow { grid-template-columns: 1.4fr 70px 84px 84px 52px 160px; }
  .thead span:nth-child(6), .thead span:nth-child(7),
  .trow > div:nth-child(6), .trow > div:nth-child(7) { display: none; }
}
</style>
