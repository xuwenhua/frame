import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '../layouts/AppLayout.vue'
import AdminLayout from '../layouts/AdminLayout.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('../views/Login.vue'),
      meta: { public: true }
    },

    // ===== 创作端 =====
    {
      path: '/',
      component: AppLayout,
      children: [
        {
          path: '',
          name: 'Home',
          component: () => import('../views/Home.vue'),
          meta: { public: true }
        },
        { path: 'projects', name: 'Projects', component: () => import('../views/ProjectList.vue') },
        // 项目工作台：剧集管理 / 项目角色库 / 场景库 / 物品库 / 总角色场景库
        { path: 'projects/:id', name: 'ProjectManage', component: () => import('../views/ProjectManage.vue') },
        // 创作页（原项目详情）：剧本 → 资产 → 分镜 → 成片
        { path: 'projects/:id/create', name: 'ProjectDetail', component: () => import('../views/ProjectDetail.vue') },
        { path: 'points', name: 'Points', component: () => import('../views/Points.vue') },
        { path: 'account', name: 'Account', component: () => import('../views/Account.vue') },
        {
          path: 'remix',
          name: 'Remix',
          component: () => import('../views/Placeholder.vue'),
          props: { title: '爆款转绘', desc: '粘贴竞品视频链接，一键拆解复刻' }
        },
        {
          path: 'assets',
          name: 'Assets',
          component: () => import('../views/Placeholder.vue'),
          props: { title: '素材库', desc: '所有角色、场景、道具的参考图' }
        }
      ]
    },

    // ===== 管理端 =====
    {
      path: '/admin',
      component: AdminLayout,
      meta: { requiresAdmin: true },
      children: [
        {
          path: '',
          name: 'AdminDashboard',
          component: () => import('../views/admin/Dashboard.vue'),
          meta: { title: '仪表盘' }
        },
        {
          path: 'models',
          name: 'AdminModels',
          component: () => import('../views/admin/Models.vue'),
          meta: { title: '模型管理' }
        },
        {
          path: 'users',
          name: 'AdminUsers',
          component: () => import('../views/admin/Users.vue'),
          meta: { title: '用户管理' }
        },
        {
          path: 'tasks',
          name: 'AdminTasks',
          component: () => import('../views/admin/Tasks.vue'),
          meta: { title: '任务监控' }
        },
        {
          path: 'points',
          name: 'AdminPoints',
          component: () => import('../views/admin/Settings.vue'),
          props: { group: 'points', title: '积分与计费', desc: '各环节的积分扣费规则' },
          meta: { title: '积分与计费' }
        },
        {
          path: 'site',
          name: 'AdminSite',
          component: () => import('../views/admin/Settings.vue'),
          props: { group: 'general', title: '站点设置', desc: '站点名称、标语与全局限制' },
          meta: { title: '站点设置' }
        }
      ]
    }
  ],
  scrollBehavior: () => ({ top: 0 })
})

// 全局守卫
router.beforeEach((to) => {
  const token = localStorage.getItem('token')
  const userStr = localStorage.getItem('user')
  const user = userStr ? JSON.parse(userStr) : null

  // 公开页
  // 公开页：首页可匿名浏览，登录页未登录可访问
  const isPublic = to.matched.some((r) => r.meta.public)
  if (isPublic) {
    // 已登录还去登录页 → 回首页
    if (token && to.name === 'Login') {
      return { path: '/' }
    }
    return true
  }

  // 需要登录
  if (!token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }

  // 需要管理员
  if (to.matched.some((r) => r.meta.requiresAdmin)) {
    if (user?.email !== 'admin@frame.local') {
      return { path: '/' }
    }
  }

  return true
})

export default router
