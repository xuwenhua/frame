<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useUserStore } from '../stores/user'

const router = useRouter()
const route = useRoute()
const store = useUserStore()

const mode = ref<'login' | 'register'>(
  route.query.mode === 'register' ? 'register' : 'login'
)
const form = ref({ email: '', password: '', nickname: '' })
const error = ref('')
const submitting = ref(false)

const isRegister = computed(() => mode.value === 'register')

const canSubmit = computed(() => {
  if (!form.value.email.includes('@')) return false
  if (form.value.password.length < 6) return false
  return true
})

const switchMode = () => {
  mode.value = isRegister.value ? 'login' : 'register'
  error.value = ''
}

const submit = async () => {
  if (!canSubmit.value || submitting.value) return
  error.value = ''
  submitting.value = true

  try {
    if (isRegister.value) {
      await store.register(form.value.email, form.value.password, form.value.nickname)
    } else {
      await store.login(form.value.email, form.value.password)
    }
    const redirect = (route.query.redirect as string) || '/'
    router.push(redirect)
  } catch (e: any) {
    error.value = e.response?.data?.message || '操作失败，请重试'
  } finally {
    submitting.value = false
  }
}

const fillDemo = () => {
  form.value.email = 'admin@frame.local'
  form.value.password = 'admin123'
  mode.value = 'login'
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-bg"></div>

    <div class="auth-card">
      <!-- 品牌 -->
      <div class="brand">
        <span class="brand-mark">
          <svg viewBox="0 0 24 24" fill="none" width="18" height="18">
            <path d="M4 6.5h9.5a3.5 3.5 0 0 1 0 7H4m0 0h11.5a3 3 0 0 1 0 6H4" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" />
          </svg>
        </span>
        <span class="brand-name">Frame</span>
      </div>

      <h1>{{ isRegister ? '创建账号' : '欢迎回来' }}</h1>
      <p class="sub">{{ isRegister ? '注册即送 100 积分，够拍第一部短剧' : '登录后继续你的创作' }}</p>

      <div class="form">
        <div class="field-group">
          <label>邮箱</label>
          <input
            v-model="form.email"
            class="field"
            type="email"
            placeholder="you@example.com"
            @keyup.enter="submit"
          />
        </div>

        <div v-if="isRegister" class="field-group">
          <label>昵称 <span class="opt">选填</span></label>
          <input v-model="form.nickname" class="field" placeholder="怎么称呼你" @keyup.enter="submit" />
        </div>

        <div class="field-group">
          <label>密码</label>
          <input
            v-model="form.password"
            class="field"
            type="password"
            placeholder="至少 6 位"
            @keyup.enter="submit"
          />
        </div>

        <div v-if="error" class="error">{{ error }}</div>

        <button class="btn btn-primary submit" :disabled="!canSubmit || submitting" @click="submit">
          <span v-if="submitting" class="spinner"></span>
          {{ submitting ? '处理中' : isRegister ? '注册' : '登录' }}
        </button>
      </div>

      <div class="switch">
        {{ isRegister ? '已有账号？' : '还没有账号？' }}
        <button class="link" @click="switchMode">{{ isRegister ? '去登录' : '去注册' }}</button>
      </div>

      <button class="demo" @click="fillDemo">试用管理员账号</button>
    </div>
  </div>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  position: relative;
  overflow: hidden;
}

.auth-bg {
  position: fixed;
  inset: 0;
  pointer-events: none;
  background:
    radial-gradient(800px 500px at 25% 15%, rgba(124, 92, 255, 0.22), transparent 62%),
    radial-gradient(700px 450px at 78% 80%, rgba(255, 77, 148, 0.13), transparent 60%);
}

.auth-card {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 400px;
  padding: 36px 32px 28px;
  border-radius: var(--r-xl);
  background: rgba(16, 16, 24, 0.86);
  border: 1px solid var(--border-strong);
  backdrop-filter: blur(24px);
  box-shadow: 0 40px 90px -30px rgba(0, 0, 0, 0.9);
}

.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 26px;
}
.brand-mark {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border-radius: 9px;
  background: var(--grad);
  color: #fff;
  box-shadow: 0 8px 22px -8px rgba(124, 92, 255, 0.75);
}
.brand-name {
  font-size: 17px;
  font-weight: 680;
  letter-spacing: -0.03em;
}

h1 {
  font-size: 25px;
  font-weight: 700;
  letter-spacing: -0.035em;
}
.sub {
  font-size: 13.5px;
  color: var(--text-mute);
  margin: 6px 0 26px;
  line-height: 1.6;
}

.field-group {
  margin-bottom: 15px;
}
.field-group label {
  display: block;
  font-size: 12px;
  font-weight: 550;
  color: var(--text-dim);
  margin-bottom: 6px;
}
.opt {
  font-weight: 400;
  color: var(--text-mute);
  font-size: 11px;
}

.field {
  width: 100%;
  height: 44px;
  padding: 0 14px;
  border-radius: 11px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.03);
  font-size: 14px;
  color: var(--text);
  transition: all 0.24s var(--ease);
}
.field:focus {
  border-color: rgba(124, 92, 255, 0.55);
  background: rgba(124, 92, 255, 0.06);
  box-shadow: 0 0 0 3px rgba(124, 92, 255, 0.1);
}

.error {
  padding: 9px 12px;
  border-radius: 9px;
  font-size: 12.5px;
  color: var(--err);
  background: rgba(251, 113, 133, 0.1);
  border: 1px solid rgba(251, 113, 133, 0.25);
  margin-bottom: 14px;
}

.submit {
  width: 100%;
  height: 45px;
  margin-top: 6px;
  font-size: 14.5px;
}

.switch {
  text-align: center;
  font-size: 13px;
  color: var(--text-mute);
  margin-top: 20px;
}
.link {
  color: var(--accent);
  font-weight: 550;
  margin-left: 4px;
}
.link:hover {
  text-decoration: underline;
}

.demo {
  display: block;
  width: 100%;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-mute);
  transition: color 0.2s var(--ease);
}
.demo:hover {
  color: var(--text-dim);
}
</style>
