import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '../api'

interface User {
  id: string
  email: string
  nickname: string
  avatar: string
}

export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('token') || '')
  const user = ref<User | null>(JSON.parse(localStorage.getItem('user') || 'null'))
  const points = ref(0)
  const loading = ref(false)

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.email === 'admin@frame.local')
  const displayName = computed(() => user.value?.nickname || user.value?.email?.split('@')[0] || '用户')

  function setAuth(t: string, u: User, p?: number) {
    token.value = t
    user.value = u
    localStorage.setItem('token', t)
    localStorage.setItem('user', JSON.stringify(u))
    if (typeof p === 'number') points.value = p
  }

  function clear() {
    token.value = ''
    user.value = null
    points.value = 0
    localStorage.removeItem('token')
    localStorage.removeItem('user')
  }

  async function register(email: string, password: string, nickname?: string) {
    loading.value = true
    try {
      const res: any = await api.post('/auth/register', { email, password, nickname })
      setAuth(res.data.token, res.data.user, res.data.points)
      return res
    } finally {
      loading.value = false
    }
  }

  async function login(email: string, password: string) {
    loading.value = true
    try {
      const res: any = await api.post('/auth/login', { email, password })
      setAuth(res.data.token, res.data.user, res.data.points)
      return res
    } finally {
      loading.value = false
    }
  }

  async function fetchProfile() {
    if (!token.value) return
    try {
      const res: any = await api.get('/me')
      user.value = res.data.user
      points.value = res.data.points
      localStorage.setItem('user', JSON.stringify(res.data.user))
    } catch (e) {
      // token 失效由拦截器处理
    }
  }

  async function fetchPoints() {
    if (!token.value) return
    try {
      const res: any = await api.get('/me/points')
      points.value = res.data.balance
    } catch (e) {
      /* ignore */
    }
  }

  function logout() {
    clear()
    window.location.href = '/login'
  }

  return {
    token, user, points, loading,
    isLoggedIn, isAdmin, displayName,
    setAuth, clear, register, login, logout,
    fetchProfile, fetchPoints
  }
})
