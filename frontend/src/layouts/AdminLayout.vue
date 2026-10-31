<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useUserStore } from '../stores/user'

const route = useRoute()
const router = useRouter()
const store = useUserStore()
const collapsed = ref(false)
const showUserMenu = ref(false)

const logout = () => {
  showUserMenu.value = false
  store.logout()
}

onMounted(() => {
  if (store.isLoggedIn) store.fetchProfile()
  document.addEventListener('click', (e) => {
    const el = e.target as HTMLElement
    if (!el.closest('.admin-user')) showUserMenu.value = false
  })
})

const navs = [
  {
    group: '概览',
    items: [{ name: '仪表盘', to: '/admin', icon: 'dashboard' }]
  },
  {
    group: '运营',
    items: [
      { name: '用户管理', to: '/admin/users', icon: 'users' },
      { name: '任务监控', to: '/admin/tasks', icon: 'tasks' }
    ]
  },
  {
    group: '配置',
    items: [
      { name: '模型管理', to: '/admin/models', icon: 'model' },
      { name: '积分与计费', to: '/admin/points', icon: 'points' },
      { name: '站点设置', to: '/admin/site', icon: 'site' }
    ]
  }
]

const icons: Record<string, string> = {
  dashboard: 'M3 3h7v7H3V3Zm0 11h7v7H3v-7Zm11-11h7v7h-7V3Zm0 11h7v7h-7v-7Z',
  model: 'M12 3 3 7.5 12 12l9-4.5L12 3ZM3 12l9 4.5L21 12M3 16.5 12 21l9-4.5',
  points: 'M13 2 4.5 13.5H11l-1 8.5 9-12h-6.5L13 2Z',
  site: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Zm0 0v18M3 12h18',
  users: 'M9 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Zm7 0a3 3 0 1 0 0-6M2.5 20c0-3.3 2.9-5.5 6.5-5.5s6.5 2.2 6.5 5.5M17 14.6c2.6.4 4.5 2.3 4.5 5.4',
  tasks: 'M4 6h16M4 12h16M4 18h10m4 0 2 2 3-4'
}

const isActive = (to: string) => {
  if (to === '/admin') return route.path === '/admin'
  return route.path.startsWith(to)
}
</script>

<template>
  <div class="admin-shell">
    <!-- 侧边栏 -->
    <aside class="side" :class="{ mini: collapsed }">
      <div class="side-top">
        <div class="brand">
          <span class="brand-mark">
            <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
              <path d="M4 6.5h9.5a3.5 3.5 0 0 1 0 7H4m0 0h11.5a3 3 0 0 1 0 6H4" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" />
            </svg>
          </span>
          <span v-if="!collapsed" class="brand-text">
            <span class="brand-name">Frame</span>
            <span class="brand-sub">管理端</span>
          </span>
        </div>
      </div>

      <nav class="side-nav">
        <div v-for="g in navs" :key="g.group" class="nav-group">
          <div v-if="!collapsed" class="group-label">{{ g.group }}</div>
          <router-link
            v-for="item in g.items"
            :key="item.to"
            :to="item.to"
            class="nav-item"
            :class="{ on: isActive(item.to) }"
            :title="collapsed ? item.name : ''"
          >
            <svg viewBox="0 0 24 24" fill="none" width="16" height="16" class="ni-icon">
              <path :d="icons[item.icon]" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span v-if="!collapsed">{{ item.name }}</span>
          </router-link>
        </div>
      </nav>

      <div class="side-foot">
        <button class="collapse-btn" @click="collapsed = !collapsed">
          <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
            <path :d="collapsed ? 'M9 6l6 6-6 6' : 'M15 6l-6 6 6 6'" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>
    </aside>

    <!-- 主区 -->
    <div class="main">
      <header class="topbar">
        <div class="crumb">
          <span class="crumb-root">管理端</span>
          <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
            <path d="M9 6l6 6-6 6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
          </svg>
          <span class="crumb-cur">{{ route.meta.title || '' }}</span>
        </div>
        <div class="topbar-right">
          <router-link to="/" class="to-site">
            返回站点
            <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
              <path d="M7 17 17 7m0 0H9m8 0v8" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </router-link>
          <div class="admin-user">
            <button class="au-trigger" :class="{ open: showUserMenu }" @click.stop="showUserMenu = !showUserMenu">
              <span class="au-avatar">A</span>
              <span class="au-name">admin</span>
              <svg class="au-caret" viewBox="0 0 24 24" fill="none" width="10" height="10">
                <path d="m6 9 6 6 6-6" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </button>
            <Transition name="pop">
              <div v-if="showUserMenu" class="au-menu">
                <div class="aum-head">
                  <div class="aum-name">{{ store.displayName }}</div>
                  <div class="aum-email">{{ store.user?.email }}</div>
                </div>
                <router-link to="/" class="aum-item">
                  <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
                    <path d="M3 12 12 3l9 9M5 10v10h14V10" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                  回到创作端
                </router-link>
                <button class="aum-item danger" @click="logout">
                  <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
                    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                  退出登录
                </button>
              </div>
            </Transition>
          </div>
        </div>
      </header>

      <main class="content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<style scoped>
.admin-shell {
  display: flex;
  height: 100vh;
  background: #0a0a10;
  position: relative;
  z-index: 1;
}

/* 侧边栏 */
.side {
  width: 216px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: #0d0d13;
  border-right: 1px solid var(--border);
  transition: width 0.28s var(--ease);
}
.side.mini { width: 62px; }

.side-top {
  padding: 18px 14px 12px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
}
.brand-mark {
  width: 27px;
  height: 27px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: var(--grad);
  color: #fff;
}
.brand-text { display: flex; flex-direction: column; line-height: 1.25; }
.brand-name { font-size: 14px; font-weight: 680; letter-spacing: -0.02em; }
.brand-sub { font-size: 10.5px; color: var(--text-mute); }

.side-nav {
  flex: 1;
  overflow-y: auto;
  padding: 6px 9px;
}
.nav-group { margin-bottom: 16px; }
.group-label {
  font-size: 10.5px;
  font-weight: 620;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--text-mute);
  padding: 0 8px;
  margin-bottom: 5px;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 9px;
  height: 36px;
  padding: 0 10px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-dim);
  transition: all 0.2s var(--ease);
  margin-bottom: 1px;
}
.nav-item:hover { background: var(--surface); color: var(--text); }
.nav-item.on {
  background: rgba(124, 92, 255, 0.14);
  color: #c4b5fd;
}
.ni-icon { flex-shrink: 0; }

.side-foot {
  padding: 10px 9px 14px;
  border-top: 1px solid var(--border);
}
.collapse-btn {
  width: 100%;
  height: 32px;
  display: grid;
  place-items: center;
  border-radius: 7px;
  color: var(--text-mute);
  transition: all 0.2s var(--ease);
}
.collapse-btn:hover { background: var(--surface); color: var(--text); }

/* 主区 */
.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.topbar {
  height: 56px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  border-bottom: 1px solid var(--border);
  background: rgba(13, 13, 19, 0.6);
  backdrop-filter: blur(16px);
}
.crumb {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--text-mute);
}
.crumb-cur { color: var(--text); font-weight: 550; }

.topbar-right { display: flex; align-items: center; gap: 16px; }
.to-site {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12.5px;
  color: var(--text-mute);
  transition: color 0.2s var(--ease);
}
.to-site:hover { color: var(--text); }

.admin-user {
  display: flex;
  align-items: center;
  gap: 7px;
  padding-left: 16px;
  border-left: 1px solid var(--border);
}
.au-avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 11px;
  font-weight: 650;
  color: #fff;
  background: linear-gradient(140deg, #7c5cff, #4c2fd6);
}
.au-name { font-size: 12.5px; color: var(--text-dim); }

.admin-user {
  position: relative;
  display: flex;
  align-items: center;
}

.au-trigger {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 34px;
  padding: 0 9px 0 4px;
  border-radius: 99px;
  border: 1px solid transparent;
  transition: all 0.2s var(--ease);
}
.au-trigger:hover {
  background: var(--surface);
  border-color: var(--border);
}
.au-trigger.open {
  background: var(--accent-soft);
  border-color: rgba(124, 92, 255, 0.45);
}
.au-caret {
  opacity: 0.6;
  transition: transform 0.24s var(--ease);
}
.au-trigger.open .au-caret {
  transform: rotate(180deg);
}

.au-menu {
  position: absolute;
  top: 42px;
  right: 0;
  width: 208px;
  padding: 6px;
  border-radius: var(--r-md);
  background: #16161f;
  border: 1px solid var(--border-strong);
  box-shadow: 0 20px 50px -20px rgba(0, 0, 0, 0.9);
  z-index: 200;
}
.aum-head {
  padding: 9px 11px 11px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 5px;
}
.aum-name {
  font-size: 13px;
  font-weight: 590;
  margin-bottom: 1px;
}
.aum-email {
  font-size: 11px;
  color: var(--text-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.aum-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 11px;
  border-radius: 7px;
  font-size: 12.5px;
  color: var(--text-dim);
  text-align: left;
  transition: all 0.18s var(--ease);
}
.aum-item:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.aum-item.danger:hover {
  color: var(--err);
  background: rgba(251, 113, 133, 0.09);
}

.pop-enter-active,
.pop-leave-active {
  transition: all 0.2s var(--ease);
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(-6px) scale(0.97);
}

.content {
  flex: 1;
  overflow-y: auto;
  padding: 28px 32px 48px;
}
.content > * {
  margin-left: auto;
  margin-right: auto;
  width: 100%;
}

@media (max-width: 800px) {
  .side { position: absolute; z-index: 200; height: 100%; }
  .side.mini { width: 0; overflow: hidden; border: none; }
}
</style>
