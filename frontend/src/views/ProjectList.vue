<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import api from '../api'

const router = useRouter()
const route = useRoute()

interface Project {
  id: string
  name: string
  description: string
  status: string
  createdAt: string
}

const projects = ref<Project[]>([])
const loading = ref(true)
const keyword = ref('')
const showCreate = ref(false)
const creating = ref(false)
const form = ref({ name: '', description: '' })

const filtered = computed(() => {
  if (!keyword.value.trim()) return projects.value
  const k = keyword.value.toLowerCase()
  return projects.value.filter(
    (p) =>
      p.name.toLowerCase().includes(k) ||
      (p.description || '').toLowerCase().includes(k)
  )
})

const statusMap: Record<string, { label: string; cls: string }> = {
  draft: { label: '草稿', cls: 'badge-draft' },
  in_progress: { label: '制作中', cls: 'badge-active' },
  completed: { label: '已完成', cls: 'badge-done' }
}

const load = async () => {
  loading.value = true
  try {
    const res: any = await api.get('/projects', { params: { page: 1, size: 100 } })
    projects.value = res.data?.list || []
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const create = async () => {
  if (!form.value.name.trim()) return
  creating.value = true
  try {
    const res: any = await api.post('/projects', form.value)
    showCreate.value = false
    form.value = { name: '', description: '' }
    router.push(`/projects/${res.data.id}`)
  } catch (e) {
    console.error(e)
  } finally {
    creating.value = false
  }
}

const open = (id: string) => router.push(`/projects/${id}`)

const timeAgo = (iso: string) => {
  const d = new Date(iso)
  const diff = Date.now() - d.getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return '刚刚'
  if (m < 60) return `${m} 分钟前`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时前`
  return `${Math.floor(h / 24)} 天前`
}

onMounted(() => {
  load()
  if (route.query.idea) {
    showCreate.value = true
    form.value.description = String(route.query.idea)
  }
})
</script>

<template>
  <div class="works-page">
    <!-- 头部 -->
    <div class="head">
      <div>
        <h1>我的短剧</h1>
        <p>{{ projects.length }} 部作品</p>
      </div>
      <div class="head-right">
        <div class="search">
          <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
            <circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="1.9" />
            <path d="m16.5 16.5 4 4" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
          </svg>
          <input v-model="keyword" placeholder="搜索作品" />
        </div>
        <button class="btn btn-primary" @click="showCreate = true">
          <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
            <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
          新建短剧
        </button>
      </div>
    </div>

    <!-- 加载 -->
    <div v-if="loading" class="grid">
      <div v-for="i in 6" :key="i" class="card skeleton"></div>
    </div>

    <!-- 空状态 -->
    <div v-else-if="projects.length === 0" class="blank">
      <div class="blank-art">
        <svg viewBox="0 0 24 24" fill="none" width="34" height="34">
          <rect x="2.5" y="5" width="19" height="14" rx="2.5" stroke="currentColor" stroke-width="1.5" />
          <path d="M10 9.5v5l4.5-2.5L10 9.5Z" fill="currentColor" />
        </svg>
      </div>
      <h3>还没有短剧</h3>
      <p>写一句话，把它拍出来</p>
      <button class="btn btn-primary" @click="showCreate = true">开始第一部</button>
    </div>

    <!-- 卡片墙 -->
    <div v-else class="grid">
      <!-- 新建卡 -->
      <button class="card create" @click="showCreate = true">
        <div class="create-icon">
          <svg viewBox="0 0 24 24" fill="none" width="20" height="20">
            <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </div>
        <span>新建短剧</span>
      </button>

      <!-- 项目卡 -->
      <div
        v-for="(p, i) in filtered"
        :key="p.id"
        class="card project rise"
        :style="{ animationDelay: `${i * 0.04}s` }"
        @click="open(p.id)"
      >
        <div class="thumb">
          <div class="thumb-grad" :style="{ '--h': (i * 47) % 360 + 'deg' }"></div>
          <div class="thumb-num">{{ String(i + 1).padStart(2, '0') }}</div>
          <span class="badge badge-dot" :class="statusMap[p.status]?.cls || 'badge-draft'">
            {{ statusMap[p.status]?.label || '草稿' }}
          </span>
          <div class="thumb-play">
            <svg viewBox="0 0 24 24" fill="none" width="18" height="18">
              <path d="M9 7.5v9l7-4.5-7-4.5Z" fill="currentColor" />
            </svg>
          </div>
        </div>
        <div class="meta">
          <h3>{{ p.name }}</h3>
          <p>{{ p.description || '暂无简介' }}</p>
          <span class="time">{{ timeAgo(p.createdAt) }}</span>
        </div>
      </div>
    </div>

    <!-- 新建弹窗 -->
    <Transition name="fade">
      <div v-if="showCreate" class="modal-mask" @click.self="showCreate = false">
        <div class="modal">
          <h3>新建短剧</h3>
          <p class="modal-sub">先起个名字，后面随时能改</p>

          <label class="label">作品名称</label>
          <input v-model="form.name" class="field" placeholder="比如：总裁的隐婚妻" @keyup.enter="create" />

          <label class="label">一句话简介</label>
          <textarea
            v-model="form.description"
            class="field"
            rows="3"
            placeholder="想讲一个什么样的故事"
          ></textarea>

          <div class="modal-actions">
            <button class="btn btn-ghost" @click="showCreate = false">取消</button>
            <button class="btn btn-primary" :disabled="!form.name.trim() || creating" @click="create">
              <span v-if="creating" class="spinner"></span>
              {{ creating ? '创建中' : '创建' }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.works-page {
  padding-top: 28px;
}

/* 头部 */
.head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 32px;
}
.head h1 {
  font-size: 30px;
  font-weight: 690;
  letter-spacing: -0.035em;
}
.head p {
  font-size: 13.5px;
  color: var(--text-mute);
  margin-top: 3px;
}
.head-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.search {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 42px;
  padding: 0 15px;
  border-radius: 99px;
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--text-mute);
  transition: all 0.24s var(--ease);
}
.search:focus-within {
  border-color: rgba(124, 92, 255, 0.4);
  color: var(--text-dim);
}
.search input {
  width: 150px;
  font-size: 13.5px;
  color: var(--text);
}

/* 网格 */
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(232px, 1fr));
  gap: 18px;
}

.card {
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
  text-align: left;
  transition: all 0.34s var(--ease);
  display: flex;
  flex-direction: column;
}

/* 新建卡 */
.card.create {
  min-height: 100%;
  border-style: dashed;
  border-color: rgba(255, 255, 255, 0.12);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  font-size: 13.5px;
  color: var(--text-mute);
}
.card.create:hover {
  border-color: rgba(124, 92, 255, 0.45);
  background: var(--accent-soft);
  color: var(--text);
  transform: translateY(-3px);
}
.create-icon {
  width: 44px;
  height: 44px;
  border-radius: 13px;
  display: grid;
  place-items: center;
  background: var(--surface);
  border: 1px solid var(--border);
}

/* 项目卡 */
.card.project {
  cursor: pointer;
}
.card.project:hover {
  transform: translateY(-4px);
  border-color: var(--border-strong);
  box-shadow: 0 24px 50px -22px rgba(0, 0, 0, 0.85);
}

.thumb {
  position: relative;
  aspect-ratio: 16 / 11;
  overflow: hidden;
  background: #101018;
  flex-shrink: 0;
}
.thumb-grad {
  position: absolute;
  inset: 0;
  background: linear-gradient(
    150deg,
    hsl(calc(250 + var(--h)) 72% 26%) 0%,
    hsl(calc(280 + var(--h)) 66% 15%) 45%,
    #0a0a10 100%
  );
  transition: transform 0.6s var(--ease);
}
.card.project:hover .thumb-grad {
  transform: scale(1.06);
}
.thumb-num {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  font-size: 52px;
  font-weight: 750;
  letter-spacing: -0.05em;
  color: rgba(255, 255, 255, 0.09);
}
.thumb .badge {
  position: absolute;
  top: 11px;
  left: 11px;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(10px);
}

.thumb-play {
  position: absolute;
  bottom: 12px;
  right: 12px;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: #fff;
  background: rgba(255, 255, 255, 0.13);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(255, 255, 255, 0.18);
  opacity: 0;
  transform: scale(0.85);
  transition: all 0.3s var(--ease);
}
.card.project:hover .thumb-play {
  opacity: 1;
  transform: scale(1);
}

.meta {
  padding: 13px 14px 15px;
  flex: 1;
}
.meta h3 {
  font-size: 14px;
  font-weight: 590;
  letter-spacing: -0.012em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.meta p {
  font-size: 12.5px;
  color: var(--text-mute);
  margin-top: 3px;
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.time {
  display: block;
  font-size: 11.5px;
  color: var(--text-mute);
  margin-top: 9px;
  opacity: 0.7;
}

/* 骨架 */
.skeleton {
  min-height: 300px;
  background: linear-gradient(
    100deg,
    rgba(255, 255, 255, 0.025) 30%,
    rgba(255, 255, 255, 0.055) 50%,
    rgba(255, 255, 255, 0.025) 70%
  );
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
}
@keyframes shimmer {
  to {
    background-position: -200% 0;
  }
}

/* 空状态 */
.blank {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 100px 20px;
  text-align: center;
}
.blank-art {
  width: 72px;
  height: 72px;
  border-radius: 22px;
  display: grid;
  place-items: center;
  color: var(--text-mute);
  background: var(--surface);
  border: 1px solid var(--border);
  margin-bottom: 22px;
}
.blank h3 {
  font-size: 17px;
  font-weight: 620;
  margin-bottom: 6px;
}
.blank p {
  font-size: 13.5px;
  color: var(--text-mute);
  margin-bottom: 22px;
}

/* 弹窗 */
.modal-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgba(0, 0, 0, 0.66);
  backdrop-filter: blur(8px);
}
.modal {
  width: 100%;
  max-width: 430px;
  padding: 28px;
  border-radius: var(--r-xl);
  background: #12121a;
  border: 1px solid var(--border-strong);
  box-shadow: 0 40px 90px -30px rgba(0, 0, 0, 0.9);
}
.modal h3 {
  font-size: 19px;
  font-weight: 660;
  letter-spacing: -0.025em;
}
.modal-sub {
  font-size: 13px;
  color: var(--text-mute);
  margin: 5px 0 22px;
}
.label {
  display: block;
  font-size: 12.5px;
  font-weight: 540;
  color: var(--text-dim);
  margin-bottom: 7px;
}
.field {
  margin-bottom: 16px;
}
.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 6px;
}

/* 过渡 */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.26s var(--ease);
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
.fade-enter-active .modal {
  animation: rise 0.34s var(--ease);
}

@media (max-width: 720px) {
  .head {
    flex-direction: column;
    align-items: stretch;
  }
  .head-right {
    flex-direction: column;
    align-items: stretch;
  }
  .search input {
    width: 100%;
  }
}
</style>
