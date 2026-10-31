<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import api from '../api'
import { useUserStore } from '../stores/user'

const router = useRouter()
const store = useUserStore()
const idea = ref('')

interface Project {
  id: string
  name: string
  description: string
  status: string
  createdAt: string
}

const recent = ref<Project[]>([])
const loadingRecent = ref(true)

const sparks = [
  '霸道总裁的隐婚妻子，其实是顶级设计师',
  '外卖小哥捡到一部手机，里面是豪门恩怨',
  '重生回到十年前，我这次要先买房',
  '婆婆逼我离婚，转头发现我是甲方老板'
]

const flowSteps = [
  { n: '01', t: '剧本', d: '一句话或一段文本，扩写成可分镜的剧本', c: '#7c5cff' },
  { n: '02', t: '识别', d: '自动抽出角色、场景、道具，结构化锁定形象', c: '#ff4d94' },
  { n: '03', t: '分镜', d: '两次 LLM 生成人读脚本 + 机读描述词', c: '#ffa14d' },
  { n: '04', t: '成片', d: '一键批量出片，13 秒竖屏直接发抖音', c: '#22d3ee' }
]

const useSpark = (s: string) => {
  idea.value = s
}

const start = () => {
  // 未登录先去登录，登录后回到创建页
  if (!store.isLoggedIn) {
    router.push({ path: '/login', query: { redirect: '/projects' } })
    return
  }
  router.push({ path: '/projects', query: { idea: idea.value } })
}

const timeAgo = (iso: string) => {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return '刚刚'
  if (m < 60) return `${m} 分钟前`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时前`
  return `${Math.floor(h / 24)} 天前`
}

onMounted(async () => {
  // 访客不拉取作品列表
  if (!store.isLoggedIn) {
    loadingRecent.value = false
    return
  }
  try {
    const res: any = await api.get('/projects', { params: { page: 1, size: 5 } })
    recent.value = res.data?.list || []
  } catch (e) {
    console.error(e)
  } finally {
    loadingRecent.value = false
  }
})
</script>

<template>
  <div class="home">
    <!-- ============ Hero ============ -->
    <section class="hero">
      <div class="hero-glow"></div>

      <div class="hero-badge rise">
        <span class="pulse"></span>
        AI 短剧创作引擎
      </div>

      <h1 class="hero-title rise" style="animation-delay: 0.06s">
        一句话，
        <span class="grad-text">拍成一部短剧</span>
      </h1>

      <p class="hero-sub rise" style="animation-delay: 0.12s">
        从灵感到成片只需四步 —— 写剧本、认角色、生画面、出视频。<br />
        中间所有脏活，交给我们。
      </p>

      <!-- 创作输入 -->
      <div class="composer rise" style="animation-delay: 0.18s">
        <textarea
          v-model="idea"
          class="composer-input"
          rows="2"
          placeholder="描述你想拍的短剧，比如：都市职场新人在会议上反杀小人..."
        ></textarea>
        <div class="composer-bar">
          <div class="composer-hint">
            <svg viewBox="0 0 24 24" fill="none" width="14" height="14" style="flex-shrink:0">
              <path d="M12 3v18M3 12h18" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
            </svg>
            <span>按题材生成，或直接粘贴已有剧本</span>
          </div>
          <button class="btn btn-primary" :disabled="!idea.trim()" @click="start">
            开始创作
            <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
              <path d="M5 12h13m0 0-5.5-5.5M18 12l-5.5 5.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>
      </div>

      <!-- 灵感快捷 -->
      <div class="sparks rise" style="animation-delay: 0.24s">
        <span class="sparks-label">试试这些</span>
        <button v-for="s in sparks" :key="s" class="spark" @click="useSpark(s)">
          {{ s }}
        </button>
      </div>
    </section>

    <!-- ============ 流程 ============ -->
    <section class="flow">
      <div v-for="(step, i) in flowSteps" :key="step.n" class="flow-step">
        <div class="flow-num" :style="{ color: step.c }">{{ step.n }}</div>
        <div class="flow-body">
          <h3>{{ step.t }}</h3>
          <p>{{ step.d }}</p>
        </div>
        <div v-if="i < flowSteps.length - 1" class="flow-line"></div>
      </div>
    </section>

    <!-- ============ 我的作品 ============ -->
    <section class="works">
      <div class="works-head">
        <div>
          <h2>{{ store.isLoggedIn ? '最近的短剧' : '开始你的第一部' }}</h2>
          <p>{{ store.isLoggedIn ? '继续上次没做完的，或者开一部新的' : '从一句话到成片，全程 AI 代劳' }}</p>
        </div>
        <router-link v-if="store.isLoggedIn" to="/projects" class="works-all">
          全部作品
          <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
            <path d="M5 12h13m0 0-5.5-5.5M18 12l-5.5 5.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </router-link>
      </div>

      <div class="works-grid">
        <!-- 访客：引导注册 -->
        <div v-if="!store.isLoggedIn" class="guest-cta">
          <div class="gc-icon">
            <svg viewBox="0 0 24 24" fill="none" width="22" height="22">
              <path d="M12 3 3 7.5 12 12l9-4.5L12 3ZM3 12l9 4.5L21 12M3 16.5 12 21l9-4.5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </div>
          <div class="gc-text">
            <h3>登录后开始创作</h3>
            <p>注册即送 100 积分，够拍第一部短剧</p>
          </div>
          <router-link :to="{ path: '/login', query: { redirect: '/projects', mode: 'register' } }" class="btn btn-primary btn-sm">
            免费注册
          </router-link>
        </div>

        <template v-else>
          <router-link to="/projects" class="work-card new">
            <div class="new-icon">
              <svg viewBox="0 0 24 24" fill="none" width="22" height="22">
                <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
              </svg>
            </div>
            <span>新建短剧</span>
          </router-link>

          <router-link
            v-for="(p, i) in recent"
            :key="p.id"
            :to="`/projects/${p.id}`"
            class="work-card proj rise"
            :style="{ animationDelay: `${i * 0.05}s` }"
          >
            <div class="proj-cover">
              <div class="proj-grad" :style="{ '--h': (i * 52) % 360 + 'deg' }"></div>
              <span class="proj-idx">{{ String(i + 1).padStart(2, '0') }}</span>
            </div>
            <div class="proj-meta">
              <h4>{{ p.name }}</h4>
              <span>{{ timeAgo(p.createdAt) }}</span>
            </div>
          </router-link>

          <div v-if="!loadingRecent && recent.length === 0" class="work-card empty">
            <div class="empty-art">
              <svg viewBox="0 0 24 24" fill="none" width="30" height="30">
                <rect x="2.5" y="5" width="19" height="14" rx="2.5" stroke="currentColor" stroke-width="1.6" />
                <path d="M10 9.5v5l4.5-2.5L10 9.5Z" fill="currentColor" />
              </svg>
            </div>
            <span class="empty-text">还没有作品</span>
            <span class="empty-sub">从上面输入一句话开始</span>
          </div>
        </template>
      </div>
    </section>
  </div>
</template>

<style scoped>
.home {
  padding-top: 40px;
}

/* ---------- Hero ---------- */
.hero {
  position: relative;
  text-align: center;
  padding: 64px 0 40px;
}
.hero-glow {
  position: absolute;
  top: -120px;
  left: 50%;
  transform: translateX(-50%);
  width: 760px;
  height: 380px;
  pointer-events: none;
  background: radial-gradient(closest-side, rgba(124, 92, 255, 0.22), transparent 72%);
  filter: blur(28px);
}

.hero-badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 30px;
  padding: 0 14px 0 11px;
  border-radius: 99px;
  font-size: 12.5px;
  font-weight: 520;
  letter-spacing: 0.01em;
  color: var(--text-dim);
  background: var(--surface);
  border: 1px solid var(--border);
}
.pulse {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ok);
  animation: pulse 2.2s infinite;
}
@keyframes pulse {
  0% { box-shadow: 0 0 0 0 rgba(52, 211, 153, 0.55); }
  70% { box-shadow: 0 0 0 7px rgba(52, 211, 153, 0); }
  100% { box-shadow: 0 0 0 0 rgba(52, 211, 153, 0); }
}

.hero-title {
  margin: 22px 0 0;
  font-size: clamp(38px, 6vw, 66px);
  font-weight: 700;
  line-height: 1.08;
  letter-spacing: -0.045em;
}

.hero-sub {
  margin: 20px auto 0;
  max-width: 480px;
  font-size: 15.5px;
  line-height: 1.75;
  color: var(--text-dim);
}

/* ---------- 创作输入 ---------- */
.composer {
  max-width: 660px;
  margin: 40px auto 0;
  padding: 8px;
  border-radius: var(--r-xl);
  background: rgba(255, 255, 255, 0.032);
  border: 1px solid var(--border);
  backdrop-filter: blur(20px);
  transition: border-color 0.3s var(--ease), box-shadow 0.3s var(--ease);
}
.composer:focus-within {
  border-color: rgba(124, 92, 255, 0.42);
  box-shadow: 0 0 0 4px rgba(124, 92, 255, 0.09), 0 22px 60px -24px rgba(124, 92, 255, 0.4);
}

.composer-input {
  width: 100%;
  padding: 16px 18px 8px;
  font-size: 15.5px;
  line-height: 1.6;
  color: var(--text);
  text-align: left;
}

.composer-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 8px 6px 18px;
}
.composer-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--text-mute);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.composer-bar .btn {
  flex-shrink: 0;
}

/* ---------- 灵感 ---------- */
.sparks {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 8px;
  margin-top: 26px;
}
.sparks-label {
  font-size: 12.5px;
  color: var(--text-mute);
  margin-right: 2px;
}
.spark {
  max-width: 260px;
  padding: 7px 14px;
  border-radius: 99px;
  font-size: 12.5px;
  color: var(--text-dim);
  background: var(--surface);
  border: 1px solid var(--border);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: all 0.24s var(--ease);
}
.spark:hover {
  color: var(--text);
  background: var(--surface-hover);
  border-color: var(--border-strong);
  transform: translateY(-1px);
}

/* ---------- 流程 ---------- */
.flow {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 0;
  margin-top: 88px;
  padding: 34px 8px;
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}

.flow-step {
  position: relative;
  display: flex;
  gap: 13px;
  padding: 0 22px;
}
.flow-num {
  font-size: 12px;
  font-weight: 680;
  letter-spacing: 0.04em;
  padding-top: 3px;
  opacity: 0.85;
}
.flow-body h3 {
  font-size: 14.5px;
  font-weight: 600;
  letter-spacing: -0.01em;
  margin-bottom: 4px;
}
.flow-body p {
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-mute);
}
.flow-line {
  position: absolute;
  right: 0;
  top: 4px;
  bottom: 4px;
  width: 1px;
  background: var(--border);
}

/* ---------- 作品 ---------- */
.works {
  margin-top: 72px;
}
.works-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  margin-bottom: 24px;
}
.works-head h2 {
  font-size: 21px;
  font-weight: 660;
  letter-spacing: -0.025em;
}
.works-head p {
  font-size: 13.5px;
  color: var(--text-mute);
  margin-top: 3px;
}
.works-all {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 13px;
  color: var(--text-dim);
  transition: color 0.22s var(--ease);
}
.works-all:hover {
  color: var(--text);
}

.works-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 16px;
}

/* 访客引导 */
.guest-cta {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 22px 24px;
  border-radius: var(--r-lg);
  border: 1px solid rgba(124, 92, 255, 0.25);
  background: linear-gradient(120deg, rgba(124, 92, 255, 0.13), rgba(255, 77, 148, 0.06));
}
.gc-icon {
  width: 46px;
  height: 46px;
  flex-shrink: 0;
  border-radius: 14px;
  display: grid;
  place-items: center;
  color: #c4b5fd;
  background: rgba(124, 92, 255, 0.16);
  border: 1px solid rgba(124, 92, 255, 0.3);
}
.gc-text { flex: 1; min-width: 0; }
.gc-text h3 {
  font-size: 15px;
  font-weight: 620;
  letter-spacing: -0.015em;
  margin-bottom: 3px;
}
.gc-text p {
  font-size: 12.5px;
  color: var(--text-mute);
}

.work-card {
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  transition: all 0.34s var(--ease);
}

.work-card.new {
  aspect-ratio: 4 / 5;
  align-items: center;
  justify-content: center;
  gap: 11px;
  border-style: dashed;
  border-color: rgba(255, 255, 255, 0.13);
  color: var(--text-mute);
  font-size: 13.5px;
  font-weight: 520;
}
.work-card.new:hover {
  border-color: rgba(124, 92, 255, 0.45);
  background: var(--accent-soft);
  color: var(--text);
  transform: translateY(-3px);
}
.new-icon {
  width: 46px;
  height: 46px;
  border-radius: 14px;
  display: grid;
  place-items: center;
  background: var(--surface);
  border: 1px solid var(--border);
}

/* 项目卡 */
.work-card.proj {
  cursor: pointer;
}
.work-card.proj:hover {
  transform: translateY(-4px);
  border-color: var(--border-strong);
  box-shadow: 0 24px 50px -22px rgba(0, 0, 0, 0.85);
}

.proj-cover {
  position: relative;
  aspect-ratio: 16 / 10;
  overflow: hidden;
  background: #101018;
}
.proj-grad {
  position: absolute;
  inset: 0;
  background: linear-gradient(
    145deg,
    hsl(calc(248 + var(--h)) 70% 27%) 0%,
    hsl(calc(282 + var(--h)) 64% 15%) 48%,
    #0a0a10 100%
  );
  transition: transform 0.6s var(--ease);
}
.work-card.proj:hover .proj-grad {
  transform: scale(1.06);
}
.proj-idx {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  font-size: 40px;
  font-weight: 750;
  letter-spacing: -0.05em;
  color: rgba(255, 255, 255, 0.11);
}
.proj-meta {
  padding: 11px 13px 13px;
}
.proj-meta h4 {
  font-size: 13.5px;
  font-weight: 590;
  letter-spacing: -0.012em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.proj-meta span {
  display: block;
  font-size: 11.5px;
  color: var(--text-mute);
  margin-top: 3px;
}

.work-card.empty {
  aspect-ratio: 4 / 5;
  align-items: center;
  justify-content: center;
  gap: 9px;
  cursor: default;
}
.empty-art {
  width: 60px;
  height: 60px;
  border-radius: 18px;
  display: grid;
  place-items: center;
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--border);
  margin-bottom: 4px;
}
.empty-text {
  font-size: 13.5px;
  color: var(--text-dim);
}
.empty-sub {
  font-size: 12px;
  color: var(--text-mute);
}

@media (max-width: 900px) {
  .flow {
    grid-template-columns: repeat(2, 1fr);
    gap: 26px 0;
  }
  .flow-line {
    display: none;
  }
  .flow-step {
    padding: 0 12px;
  }
}
</style>
