import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 300000
})

// 请求拦截器：自动带 token
api.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => Promise.reject(error)
)

// 响应拦截器：401 自动跳登录
api.interceptors.response.use(
  (response) => response.data,
  (error) => {
    const status = error.response?.status
    const url = error.config?.url || ''

    // 登录/注册接口的 401 不跳转
    const isAuthApi = url.includes('/auth/')

    if (status === 401 && !isAuthApi) {
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      if (!window.location.pathname.startsWith('/login')) {
        window.location.href = '/login'
      }
    }

    return Promise.reject(error)
  }
)

export default api
