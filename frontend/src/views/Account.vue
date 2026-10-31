<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import api from '../api'
import { useUserStore } from '../stores/user'

const store = useUserStore()

const loading = ref(true)
const data = ref<any>(null)
const activeTab = ref<'profile' | 'security'>('profile')

// 资料表单
const profileForm = ref({ nickname: '', avatar: '' })
const savingProfile = ref(false)
const profileMsg = ref<{ type: 'ok' | 'err'; text: string } | null>(null)

// 密码表单
const pwdForm = ref({ oldPassword: '', newPassword: '', confirm: '' })
const savingPwd = ref(false)
const pwdMsg = ref<{ type: 'ok' | 'err'; text: string } | null>(null)

const user = computed(() => data.value?.user)
const stats = computed(() => data.value?.stats || {})
const points = computed(() => data.value?.points || { balance: 0, totalIn: 0, totalOut: 0 })
const recent = computed(() => data.value?.recentTransactions || [])

const initial = computed(() => (user.value?.nickname || user.value?.email || 'U').charAt(0).toUpperCase())

const joinDays = computed(() => {
  if (!user.value?.createdAt) return 0
  return Math.max(1, Math.floor((Date.now() - new Date(user.value.createdAt).getTime()) / 86400000))
})

const avatarChoices = ['🎬', '🎭', '✨', '🔥', '🌙', '🎨', '🚀', '💡']

const load = async () => {
  loading.value = true
  try {
    const res: any = await api.get('/me/overview')
    data.value = res.data
    profileForm.value.nickname = res.data.user.nickname || ''
    profileForm.value.avatar = res.data.user.avatar || ''
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const saveProfile = async () => {
  if (!profileForm.value.nickname.trim()) {
    profileMsg.value = { type: 'err', text: '昵称不能为空' }
    return
  }
  savingProfile.value = true
  profileMsg.value = null
  try {
    await api.put('/me', profileForm.value)
    await store.fetchProfile()
    await load()
    profileMsg.value = { type: 'ok', text: '资料已保存' }
    setTimeout(() => (profileMsg.value = null), 2500)
  } catch (e: any) {
    profileMsg.value = { type: 'err', text: e.response?.data?.message || '保存失败' }
  } finally {
    savingProfile.value = false
  }
}

const changePassword = async () => {
  pwdMsg.value = null
  if (!pwdForm.value.oldPassword) {
    pwdMsg.value = { type: 'err', text: '请输入原密码' }
    return
  }
  if (pwdForm.value.newPassword.length < 6) {
    pwdMsg.value = { type: 'err', text: '新密码至少 6 位' }
    return
  }
  if (pwdForm.value.newPassword !== pwdForm.value.confirm) {
    pwdMsg.value = { type: 'err', text: '两次输入的新密码不一致' }
    return
  }

  savingPwd.value = true
  try {
    await api.put('/me/password', {
      oldPassword: pwdForm.value.oldPassword,
      newPassword: pwdForm.value.newPassword
    })
    pwdMsg.value = { type: 'ok', text: '密码已修改，即将退出重新登录' }
    setTimeout(() => store.logout(), 1600)
  } catch (e: any) {
    pwdMsg.value = { type: 'err', text: e.response?.data?.message || '修改失败' }
  } finally {
    savingPwd.value = false
  }
}

const fmtTime = (iso: string) => {
  const d = new Date(iso)
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

const typeLabel: Record<string, string> = {
  register: '注册赠送',
  recharge: '充值',
  refund: '退还',
  identify: '资产识别',
  image: '图像生成',
  video: '视频生成'
}

onMounted(load)
</script>

<template>
  <div class="account">
    <div class="page-head">
      <div>
        <h1>个人中心</h1>
        <p>管理你的账号资料、密码与创作数据</p>
      </div>
    </div>

    <div v-if="loading" class="loading">加载中…</div>

    <div v-else class="layout">
      <!-- 左：身份卡 -->
      <aside class="side">
        <div class="id-card">
          <div class="id-avatar">
            <span v-if="profileForm.avatar">{{ profileForm.avatar }}</span>
            <span v-else>{{ initial }}</span>
          </div>
          <h2>{{ user?.nickname }}</h2>
          <p class="id-email">{{ user?.email }}</p>
          <div class="id-badges">
            <span v-if="store.isAdmin" class="id-badge admin">管理员</span>
            <span class="id-badge">加入 {{ joinDays }} 天</span>
          </div>
        </div>

        <div class="id-points">
          <div class="ip-label">积分余额</div>
          <div class="ip-value">{{ points.balance }}</div>
          <div class="ip-detail">
            <span>累计获得 {{ points.totalIn }}</span>
            <span>已消耗 {{ points.totalOut }}</span>
          </div>
          <router-link to="/points" class="ip-link">查看明细 →</router-link>
        </div>

        <div class="id-nav">
          <button class="idn-item" :class="{ on: activeTab === 'profile' }" @click="activeTab = 'profile'">
            <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
              <circle cx="12" cy="8" r="3.6" stroke="currentColor" stroke-width="1.7" />
              <path d="M4.5 20a7.5 7.5 0 0 1 15 0" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
            </svg>
            基本资料
          </button>
          <button class="idn-item" :class="{ on: activeTab === 'security' }" @click="activeTab = 'security'">
            <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
              <rect x="4" y="10" width="16" height="10" rx="2.2" stroke="currentColor" stroke-width="1.7" />
              <path d="M8 10V7a4 4 0 0 1 8 0v3" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
            </svg>
            账号安全
          </button>
        </div>
      </aside>

      <!-- 右：内容区 -->
      <div class="main">
        <!-- 基本资料 -->
        <template v-if="activeTab === 'profile'">
          <div class="card">
            <div class="card-head">
              <h3>基本资料</h3>
              <p>昵称和头像会显示在作品分享页</p>
            </div>

            <div class="card-body">
              <div class="profile-grid">
                <!-- 左：可编辑字段 -->
                <div>
                  <div class="field-row">
                    <label>头像</label>
                    <div class="avatar-picker">
                      <button
                        v-for="a in avatarChoices"
                        :key="a"
                        class="ap-item"
                        :class="{ on: profileForm.avatar === a }"
                        @click="profileForm.avatar = profileForm.avatar === a ? '' : a"
                      >
                        {{ a }}
                      </button>
                      <span class="ap-hint">不选则用昵称首字</span>
                    </div>
                  </div>

                  <div class="field-row">
                    <label>昵称</label>
                    <input v-model="profileForm.nickname" class="field" maxlength="20" placeholder="给自己起个名字" />
                    <span class="hint">{{ profileForm.nickname.length }} / 20</span>
                  </div>

                  <div class="field-row">
                    <label>邮箱</label>
                    <input class="field disabled" :value="user?.email" disabled />
                    <span class="hint">邮箱作为登录凭证，不可修改</span>
                  </div>

                  <div v-if="profileMsg" class="msg" :class="profileMsg.type">{{ profileMsg.text }}</div>

                  <div class="card-actions">
                    <button class="btn btn-primary" :disabled="savingProfile" @click="saveProfile">
                      <span v-if="savingProfile" class="spinner"></span>
                      {{ savingProfile ? '保存中' : '保存修改' }}
                    </button>
                  </div>
                </div>

                <!-- 右：账户信息 -->
                <div class="info-panel">
                  <div class="ip-title">账户信息</div>
                  <div class="info-row">
                    <span class="ir-label">用户 ID</span>
                    <span class="ir-value mono">{{ (user?.id || '').slice(0, 8) }}…</span>
                  </div>
                  <div class="info-row">
                    <span class="ir-label">账号状态</span>
                    <span class="ir-value ok">正常</span>
                  </div>
                  <div class="info-row">
                    <span class="ir-label">账号角色</span>
                    <span class="ir-value">{{ store.isAdmin ? '管理员' : '普通用户' }}</span>
                  </div>
                  <div class="info-row">
                    <span class="ir-label">注册时间</span>
                    <span class="ir-value">{{ user?.createdAt ? user.createdAt.slice(0, 10) : '—' }}</span>
                  </div>
                  <div class="info-row">
                    <span class="ir-label">上次登录</span>
                    <span class="ir-value">{{ user?.lastLoginAt ? fmtTime(user.lastLoginAt) : '本次首次' }}</span>
                  </div>
                  <div class="info-row">
                    <span class="ir-label">累计创作</span>
                    <span class="ir-value">{{ stats.projectCount || 0 }} 部短剧</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- 创作数据 + 最近积分（宽屏并排） -->
          <div class="split">
            <!-- 创作数据 -->
            <div class="card">
              <div class="card-head">
                <div>
                  <h3>创作数据</h3>
                  <p>你在这个账号下产出的全部内容</p>
                </div>
              </div>
              <div class="card-body">
                <div class="stat-grid">
                  <div class="stat">
                    <div class="st-value">{{ stats.projectCount || 0 }}</div>
                    <div class="st-label">短剧项目</div>
                  </div>
                  <div class="stat">
                    <div class="st-value">{{ stats.episodeCount || 0 }}</div>
                    <div class="st-label">剧集</div>
                  </div>
                  <div class="stat">
                    <div class="st-value">{{ stats.frameCount || 0 }}</div>
                    <div class="st-label">分镜镜头</div>
                  </div>
                  <div class="stat">
                    <div class="st-value">{{ stats.characterCount || 0 }}</div>
                    <div class="st-label">角色</div>
                  </div>
                  <div class="stat">
                    <div class="st-value">{{ stats.sceneCount || 0 }}</div>
                    <div class="st-label">场景</div>
                  </div>
                  <div class="stat">
                    <div class="st-value">{{ stats.itemCount || 0 }}</div>
                    <div class="st-label">道具</div>
                  </div>
                </div>
              </div>
            </div>

            <!-- 最近积分 -->
            <div class="card">
              <div class="card-head">
                <div>
                  <h3>最近积分变动</h3>
                  <p>最新的收支记录</p>
                </div>
                <router-link to="/points" class="card-link">全部 →</router-link>
              </div>
              <div class="card-body no-pad">
                <div v-if="recent.length === 0" class="empty">暂无积分记录</div>
                <div v-for="t in recent" :key="t.id" class="tx-row">
                  <div class="tx-dot" :class="t.amount > 0 ? 'in' : 'out'"></div>
                  <div class="tx-main">
                    <span class="tx-name">{{ typeLabel[t.type] || t.type }}</span>
                    <span class="tx-time">{{ fmtTime(t.createdAt) }}</span>
                  </div>
                  <span class="tx-amt" :class="t.amount > 0 ? 'in' : 'out'">
                    {{ t.amount > 0 ? '+' : '' }}{{ t.amount }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </template>

        <!-- 账号安全 -->
        <template v-else>
          <div class="card">
            <div class="card-head">
              <h3>修改密码</h3>
              <p>修改成功后需要重新登录</p>
            </div>
            <div class="card-body">
              <div class="profile-grid">
                <div>
                  <div class="field-row">
                    <label>原密码</label>
                    <input v-model="pwdForm.oldPassword" class="field" type="password" placeholder="输入当前密码" />
                  </div>
                  <div class="field-row">
                    <label>新密码</label>
                    <input v-model="pwdForm.newPassword" class="field" type="password" placeholder="至少 6 位" />
                  </div>
                  <div class="field-row">
                    <label>确认新密码</label>
                    <input v-model="pwdForm.confirm" class="field" type="password" placeholder="再输一次" />
                  </div>

                  <div v-if="pwdMsg" class="msg" :class="pwdMsg.type">{{ pwdMsg.text }}</div>

                  <div class="card-actions">
                    <button class="btn btn-primary" :disabled="savingPwd" @click="changePassword">
                      <span v-if="savingPwd" class="spinner"></span>
                      {{ savingPwd ? '提交中' : '修改密码' }}
                    </button>
                  </div>
                </div>

                <!-- 右：密码建议 -->
                <div class="info-panel">
                  <div class="ip-title">密码建议</div>
                  <div class="tip-item">
                    <span class="tip-check" :class="{ ok: pwdForm.newPassword.length >= 6 }"></span>
                    至少 6 位字符
                  </div>
                  <div class="tip-item">
                    <span class="tip-check" :class="{ ok: /[a-zA-Z]/.test(pwdForm.newPassword) }"></span>
                    包含字母
                  </div>
                  <div class="tip-item">
                    <span class="tip-check" :class="{ ok: /[0-9]/.test(pwdForm.newPassword) }"></span>
                    包含数字
                  </div>
                  <div class="tip-item">
                    <span class="tip-check" :class="{ ok: pwdForm.newPassword.length >= 10 }"></span>
                    10 位以上更安全
                  </div>
                  <div class="tip-item">
                    <span class="tip-check" :class="{ ok: pwdForm.newPassword !== '' && pwdForm.newPassword !== pwdForm.oldPassword }"></span>
                    与原密码不同
                  </div>
                  <div class="tip-foot">修改后所有设备都需重新登录</div>
                </div>
              </div>
            </div>
          </div>

          <div class="card">
            <div class="card-head">
              <h3>登录设备</h3>
              <p>当前账号的登录状态</p>
            </div>
            <div class="card-body">
              <div class="device">
                <div class="dev-icon">
                  <svg viewBox="0 0 24 24" fill="none" width="18" height="18">
                    <rect x="3" y="4" width="18" height="12" rx="2" stroke="currentColor" stroke-width="1.7" />
                    <path d="M8 20h8M12 16v4" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
                  </svg>
                </div>
                <div class="dev-info">
                  <div class="dev-name">当前浏览器</div>
                  <div class="dev-meta">正在使用 · 上次登录 {{ user?.lastLoginAt ? fmtTime(user.lastLoginAt) : '本次' }}</div>
                </div>
                <span class="dev-tag">当前</span>
              </div>

              <div class="danger-zone">
                <div class="dz-text">
                  <div class="dz-title">退出登录</div>
                  <div class="dz-desc">清除本机登录凭证，下次需要重新输入密码</div>
                </div>
                <button class="btn btn-ghost btn-sm" @click="store.logout()">退出</button>
              </div>
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.account {
  width: 100%;
  padding-top: 20px;
}

.page-head { margin-bottom: 24px; }
.page-head h1 { font-size: 26px; font-weight: 690; letter-spacing: -0.03em; }
.page-head p { font-size: 13.5px; color: var(--text-mute); margin-top: 3px; }

.loading { padding: 80px; text-align: center; color: var(--text-mute); font-size: 13.5px; }

.layout {
  display: grid;
  grid-template-columns: 292px minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}

/* ---------- 左侧 ---------- */
.side { display: flex; flex-direction: column; gap: 12px; }

.id-card {
  padding: 24px 20px 20px;
  border-radius: var(--r-xl);
  border: 1px solid var(--border);
  background: linear-gradient(160deg, rgba(124, 92, 255, 0.11), rgba(255, 77, 148, 0.05));
  text-align: center;
}
.id-avatar {
  width: 62px;
  height: 62px;
  margin: 0 auto 13px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 26px;
  font-weight: 650;
  color: #fff;
  background: var(--grad);
  box-shadow: 0 10px 28px -10px rgba(124, 92, 255, 0.75);
}
.id-card h2 {
  font-size: 16.5px;
  font-weight: 660;
  letter-spacing: -0.02em;
}
.id-email {
  font-size: 12px;
  color: var(--text-mute);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.id-badges {
  display: flex;
  justify-content: center;
  gap: 6px;
  margin-top: 11px;
  flex-wrap: wrap;
}
.id-badge {
  font-size: 10.5px;
  padding: 2px 9px;
  border-radius: 99px;
  color: var(--text-mute);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.03);
}
.id-badge.admin {
  color: #c4b5fd;
  border-color: rgba(124, 92, 255, 0.35);
  background: rgba(124, 92, 255, 0.15);
}

.id-points {
  padding: 16px 18px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
}
.ip-label { font-size: 11.5px; color: var(--text-mute); }
.ip-value {
  font-size: 30px;
  font-weight: 720;
  letter-spacing: -0.04em;
  margin: 2px 0 6px;
  background: var(--grad);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
}
.ip-detail {
  display: flex;
  gap: 12px;
  font-size: 11px;
  color: var(--text-mute);
}
.ip-link {
  display: block;
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--border);
  font-size: 11.5px;
  color: var(--accent);
}

.id-nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
}
.idn-item {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 10px 12px;
  border-radius: 9px;
  font-size: 13px;
  color: var(--text-dim);
  transition: all 0.2s var(--ease);
  text-align: left;
}
.idn-item:hover { background: var(--surface-hover); color: var(--text); }
.idn-item.on {
  background: var(--accent-soft);
  color: #c4b5fd;
  font-weight: 550;
}

/* ---------- 右侧 ---------- */
.main { display: flex; flex-direction: column; gap: 14px; min-width: 0; }

/* 宽屏下并排 */
.split {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  align-items: stretch;
}

/* 并排卡片里的统计固定 3 列，避免出现 5+1 的孤行 */
.split .stat-grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.split .card {
  display: flex;
  flex-direction: column;
}
.split .card-body {
  flex: 1;
}

.card {
  border-radius: var(--r-xl);
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
}
.card-head {
  padding: 18px 22px 14px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}
.card-head h3 { font-size: 15px; font-weight: 620; letter-spacing: -0.015em; }
.card-head p { font-size: 12px; color: var(--text-mute); margin-top: 2px; }
.card-link { font-size: 12px; color: var(--accent); white-space: nowrap; }

.card-body { padding: 20px 22px 22px; }
.card-body.no-pad { padding: 0; }

.field-row { margin-bottom: 16px; max-width: 640px; }

/* 基本资料两栏：左编辑区 + 右信息栏 */
.profile-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 268px;
  gap: 28px;
  align-items: start;
}

.info-panel {
  padding: 15px 16px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.016);
}
.ip-title {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--text-mute);
  letter-spacing: 0.03em;
  margin-bottom: 11px;
}
.info-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 0;
  border-bottom: 1px dashed rgba(255, 255, 255, 0.055);
}
.info-row:last-child { border-bottom: none; }
.ir-label { font-size: 11.5px; color: var(--text-mute); flex-shrink: 0; }
.ir-value {
  font-size: 12px;
  color: var(--text-dim);
  text-align: right;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ir-value.mono { font-family: 'SF Mono', Menlo, monospace; font-size: 11px; }
.ir-value.ok { color: var(--ok); }

/* 密码建议清单 */
.tip-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 0;
  font-size: 11.5px;
  color: var(--text-mute);
}
.tip-check {
  width: 13px;
  height: 13px;
  flex-shrink: 0;
  border-radius: 50%;
  border: 1.5px solid var(--border-strong);
  position: relative;
  transition: all 0.24s var(--ease);
}
.tip-check.ok {
  background: var(--ok);
  border-color: var(--ok);
}
.tip-check.ok::after {
  content: '';
  position: absolute;
  left: 3.5px;
  top: 1px;
  width: 3px;
  height: 6px;
  border: solid #0a0a10;
  border-width: 0 1.6px 1.6px 0;
  transform: rotate(45deg);
}
.tip-foot {
  margin-top: 11px;
  padding-top: 10px;
  border-top: 1px dashed rgba(255, 255, 255, 0.055);
  font-size: 11px;
  color: var(--text-mute);
  line-height: 1.55;
}
.field-row label {
  display: block;
  font-size: 12px;
  font-weight: 550;
  color: var(--text-dim);
  margin-bottom: 7px;
}
.field {
  width: 100%;
  height: 40px;
  padding: 0 13px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.026);
  font-size: 13.5px;
  color: var(--text);
  transition: all 0.22s var(--ease);
}
.field:focus {
  border-color: rgba(124, 92, 255, 0.5);
  background: rgba(124, 92, 255, 0.05);
  box-shadow: 0 0 0 3px rgba(124, 92, 255, 0.09);
}
.field.disabled {
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.012);
  cursor: not-allowed;
}
.hint {
  display: block;
  font-size: 11px;
  color: var(--text-mute);
  margin-top: 5px;
}

.avatar-picker {
  display: flex;
  align-items: center;
  gap: 7px;
  flex-wrap: wrap;
}
.ap-item {
  width: 38px;
  height: 38px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  font-size: 18px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.022);
  transition: all 0.2s var(--ease);
}
.ap-item:hover {
  border-color: var(--border-strong);
  transform: translateY(-1px);
}
.ap-item.on {
  border-color: rgba(124, 92, 255, 0.6);
  background: var(--accent-soft);
  box-shadow: 0 0 0 3px rgba(124, 92, 255, 0.1);
}
.ap-hint { font-size: 11px; color: var(--text-mute); margin-left: 4px; }

.msg {
  padding: 9px 12px;
  border-radius: 9px;
  font-size: 12.5px;
  margin-bottom: 14px;
}
.msg.ok {
  color: var(--ok);
  background: rgba(52, 211, 153, 0.09);
  border: 1px solid rgba(52, 211, 153, 0.25);
}
.msg.err {
  color: var(--err);
  background: rgba(251, 113, 133, 0.09);
  border: 1px solid rgba(251, 113, 133, 0.25);
}

/* 主操作按钮跟随表单左对齐，避免在宽卡片里飘到最右侧与输入框脱节 */
.card-actions {
  display: flex;
  justify-content: flex-start;
  padding-top: 8px;
}
.card-actions .btn {
  min-width: 128px;
}

/* 统计 */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 10px;
}
.stat {
  padding: 14px 12px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
  text-align: center;
}
.st-value {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.03em;
}
.st-label {
  font-size: 11px;
  color: var(--text-mute);
  margin-top: 2px;
}

/* 流水 */
.empty {
  padding: 36px;
  text-align: center;
  font-size: 12.5px;
  color: var(--text-mute);
}
.tx-row {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 12px 22px;
  border-bottom: 1px solid var(--border);
}
.tx-row:last-child { border-bottom: none; }
.tx-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.tx-dot.in { background: var(--ok); }
.tx-dot.out { background: var(--text-mute); }
.tx-main { flex: 1; display: flex; align-items: baseline; gap: 9px; min-width: 0; }
.tx-name { font-size: 12.5px; }
.tx-time { font-size: 11px; color: var(--text-mute); }
.tx-amt { font-size: 13px; font-weight: 620; }
.tx-amt.in { color: var(--ok); }
.tx-amt.out { color: var(--text-dim); }

/* 设备 */
.device {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 15px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
  margin-bottom: 16px;
}
.dev-icon {
  width: 38px;
  height: 38px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  color: var(--accent);
  background: var(--accent-soft);
  flex-shrink: 0;
}
.dev-info { flex: 1; min-width: 0; }
.dev-name { font-size: 13px; font-weight: 550; }
.dev-meta { font-size: 11px; color: var(--text-mute); margin-top: 1px; }
.dev-tag {
  font-size: 10px;
  padding: 2px 8px;
  border-radius: 99px;
  color: var(--ok);
  background: rgba(52, 211, 153, 0.11);
  border: 1px solid rgba(52, 211, 153, 0.25);
}

.danger-zone {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 15px;
  border-radius: var(--r-md);
  border: 1px solid rgba(251, 113, 133, 0.22);
  background: rgba(251, 113, 133, 0.05);
}
.dz-title { font-size: 13px; font-weight: 550; }
.dz-desc { font-size: 11.5px; color: var(--text-mute); margin-top: 2px; }

@media (max-width: 1180px) {
  .split { grid-template-columns: 1fr; }
  .profile-grid { grid-template-columns: 1fr; gap: 18px; }
  .profile-grid .info-panel { order: -1; }
}

@media (max-width: 860px) {
  .layout { grid-template-columns: 1fr; }
  .side { flex-direction: column; }
  .id-nav { flex-direction: row; }
  .idn-item { flex: 1; justify-content: center; }
  .field-row { max-width: none; }
}
</style>
