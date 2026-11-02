<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useUserStore } from '../stores/user'

const route = useRoute()
const store = useUserStore()
const scrolled = ref(false)
const showUserMenu = ref(false)

const navs = [
  { name: '工作台', to: '/' },
  { name: '我的短剧', to: '/projects' },
  { name: '爆款转绘', to: '/remix' },
  { name: '素材库', to: '/assets' }
]

const activePath = computed(() => route.path)

const isActive = (to: string) => {
  if (to === '/') return activePath.value === '/'
  return activePath.value.startsWith(to)
}

const initial = computed(() => (store.displayName || 'U').charAt(0).toUpperCase())

const logout = () => {
  showUserMenu.value = false
  store.logout()
}

onMounted(() => {
  window.addEventListener('scroll', () => {
    scrolled.value = window.scrollY > 12
  })
  if (store.isLoggedIn) {
    store.fetchProfile()
  }
  // 点击外部关闭菜单
  document.addEventListener('click', (e) => {
    const el = e.target as HTMLElement
    if (!el.closest('.user-wrap')) showUserMenu.value = false
  })
})
</script>

<template>
  <div class="shell">
    <header class="nav" :class="{ solid: scrolled }">
      <div class="nav-inner">
        <!-- 品牌 -->
        <router-link to="/" class="brand">
          <span class="brand-mark">
            <svg viewBox="0 0 24 24" fill="none" width="17" height="17">
              <path
                d="M4 6.5h9.5a3.5 3.5 0 0 1 0 7H4m0 0h11.5a3 3 0 0 1 0 6H4"
                stroke="currentColor"
                stroke-width="2.1"
                stroke-linecap="round"
              />
            </svg>
          </span>
          <span class="brand-name">Frame</span>
        </router-link>

        <!-- 主导航 -->
        <nav class="nav-links">
          <router-link
            v-for="n in navs"
            :key="n.to"
            :to="n.to"
            class="nav-link"
            :class="{ on: isActive(n.to) }"
          >
            {{ n.name }}
          </router-link>
        </nav>

        <!-- 右侧 -->
        <div class="nav-right">
          <!-- 已登录 -->
          <template v-if="store.isLoggedIn">
            <router-link to="/points" class="points" title="积分明细">
              <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
                <path
                  d="M13 2 4.5 13.5H11l-1 8.5 9-12h-6.5L13 2Z"
                  fill="currentColor"
                />
              </svg>
              <span>{{ store.points }}</span>
            </router-link>

            <router-link v-if="store.isAdmin" to="/admin" class="admin-link" title="管理端">
              <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
                <circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="1.7" />
                <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z" stroke="currentColor" stroke-width="1.5" />
              </svg>
            </router-link>

            <div class="user-wrap">
              <button class="avatar" :class="{ open: showUserMenu }" title="账号菜单" @click.stop="showUserMenu = !showUserMenu">
                <span class="av-initial">{{ initial }}</span>
                <svg class="av-caret" viewBox="0 0 24 24" fill="none" width="10" height="10">
                  <path d="m6 9 6 6 6-6" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </button>
            <Transition name="pop">
              <div v-if="showUserMenu" class="user-menu">
                <div class="um-head">
                  <div class="um-avatar">{{ initial }}</div>
                  <div class="um-info">
                    <div class="um-name">{{ store.displayName }}</div>
                    <div class="um-email">{{ store.user?.email }}</div>
                  </div>
                </div>
                <router-link to="/account" class="um-item" @click="showUserMenu = false">
                  <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
                    <circle cx="12" cy="8" r="3.6" stroke="currentColor" stroke-width="1.8" />
                    <path d="M4.5 20a7.5 7.5 0 0 1 15 0" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
                  </svg>
                  个人中心
                </router-link>
                <router-link to="/points" class="um-item" @click="showUserMenu = false">
                  <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
                    <path d="M13 2 4.5 13.5H11l-1 8.5 9-12h-6.5L13 2Z" fill="currentColor" />
                  </svg>
                  积分明细
                </router-link>
                <button class="um-item danger" @click="logout">
                  <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
                    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                  退出登录
                </button>
              </div>
            </Transition>
            </div>
          </template>

          <!-- 未登录：登录 / 注册入口 -->
          <template v-else>
            <router-link :to="{ path: '/login', query: { redirect: $route.fullPath } }" class="guest-login">
              登录
            </router-link>
            <router-link :to="{ path: '/login', query: { redirect: $route.fullPath, mode: 'register' } }" class="btn btn-primary btn-sm">
              免费开始
            </router-link>
          </template>
        </div>
      </div>
    </header>

    <main class="page">
      <router-view />
    </main>
  </div>
</template>

<style scoped>
.shell {
  min-height: 100vh;
  position: relative;
  z-index: 1;
}

/* ---------- 导航 ---------- */
.nav {
  position: sticky;
  top: 0;
  z-index: 100;
  height: var(--nav-h);
  transition: background 0.4s var(--ease), border-color 0.4s var(--ease);
  border-bottom: 1px solid transparent;
}
.nav.solid {
  background: rgba(7, 7, 11, 0.72);
  backdrop-filter: blur(24px) saturate(150%);
  border-bottom-color: var(--border);
}

.nav-inner {
  width: 100%;
  height: 100%;
  padding: 0 40px;
  display: flex;
  align-items: center;
  gap: 44px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  flex-shrink: 0;
}
.brand-mark {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 9px;
  background: var(--grad);
  color: #fff;
  box-shadow: 0 6px 18px -6px rgba(124, 92, 255, 0.75);
}
.brand-name {
  font-size: 16.5px;
  font-weight: 680;
  letter-spacing: -0.03em;
}

.nav-links {
  display: flex;
  align-items: center;
  gap: 4px;
}
.nav-link {
  position: relative;
  padding: 8px 14px;
  border-radius: 9px;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-dim);
  transition: color 0.22s var(--ease), background 0.22s var(--ease);
}
.nav-link:hover {
  color: var(--text);
  background: var(--surface);
}
.nav-link.on {
  color: var(--text);
  background: var(--surface);
}

.nav-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 12px;
}
.points {
  display: flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 11px;
  border-radius: 99px;
  font-size: 12.5px;
  font-weight: 620;
  color: var(--warn);
  background: rgba(251, 191, 36, 0.09);
  border: 1px solid rgba(251, 191, 36, 0.2);
  transition: all 0.22s var(--ease);
}
.points:hover {
  background: rgba(251, 191, 36, 0.15);
  border-color: rgba(251, 191, 36, 0.35);
}

.user-wrap {
  position: relative;
}

/* 访客态 */
.guest-login {
  padding: 6px 14px;
  border-radius: 99px;
  font-size: 13.5px;
  font-weight: 500;
  color: var(--text-dim);
  transition: all 0.22s var(--ease);
}
.guest-login:hover {
  color: var(--text);
  background: var(--surface);
}

.avatar {
  display: flex;
  align-items: center;
  gap: 3px;
  height: 31px;
  padding: 0 8px 0 3px;
  border-radius: 99px;
  color: var(--text-dim);
  background: var(--surface);
  border: 1px solid var(--border);
  transition: all 0.22s var(--ease);
}
.avatar:hover {
  border-color: var(--border-strong);
  color: var(--text);
  background: var(--surface-hover);
}
.avatar.open {
  border-color: rgba(124, 92, 255, 0.5);
  background: var(--accent-soft);
  color: #c4b5fd;
}

.av-initial {
  width: 25px;
  height: 25px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 11.5px;
  font-weight: 650;
  color: #fff;
  background: linear-gradient(140deg, #7c5cff, #4c2fd6);
}
.av-caret {
  opacity: 0.6;
  transition: transform 0.24s var(--ease);
}
.avatar.open .av-caret {
  transform: rotate(180deg);
}

.user-menu {
  position: absolute;
  top: 40px;
  right: 0;
  width: 216px;
  padding: 6px;
  border-radius: var(--r-md);
  background: #16161f;
  border: 1px solid var(--border-strong);
  box-shadow: 0 20px 50px -20px rgba(0, 0, 0, 0.9);
  z-index: 200;
}
.um-head {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 9px 10px 11px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 5px;
}
.um-avatar {
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 12px;
  font-weight: 650;
  color: #fff;
  background: linear-gradient(140deg, #7c5cff, #4c2fd6);
}
.um-info {
  min-width: 0;
}
.um-name {
  font-size: 13px;
  font-weight: 590;
  margin-bottom: 1px;
}
.um-email {
  font-size: 11px;
  color: var(--text-mute);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.um-item {
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
.um-item:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.um-item.danger:hover {
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

.admin-link {
  width: 31px;
  height: 31px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: var(--text-mute);
  background: var(--surface);
  border: 1px solid var(--border);
  transition: all 0.22s var(--ease);
}
.admin-link:hover {
  color: var(--accent);
  border-color: rgba(124, 92, 255, 0.4);
  background: var(--accent-soft);
  transform: rotate(45deg);
}

.page {
  width: 100%;
  padding: 20px 40px 100px;
}

@media (max-width: 860px) {
  .nav-inner {
    padding: 0 18px;
    gap: 20px;
  }
  .nav-links {
    display: none;
  }
  .page {
    padding: 16px 18px 80px;
  }
}
</style>
