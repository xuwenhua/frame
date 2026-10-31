<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import api from '../../api'
import { toast } from '../../composables/useToast'
import { confirmDialog } from '../../composables/useConfirm'

interface TestResult {
  ok: boolean
  text: string
  detail: string
  at: number
}

interface ModelItem {
  id: string
  provider: string
  category: string
  displayName: string
  modelId: string
  baseUrl: string
  apiKeyMasked: string
  hasKey: boolean
  params: string
  creditsPerCall: number
  creditsPerUnit: number
  isDefault: boolean
  enabled: boolean
  sortOrder: number
  remark: string
  callCount: number
  failedCount: number
}

const list = ref<ModelItem[]>([])
const loading = ref(true)
const activeTab = ref('llm')
const editing = ref<ModelItem | null>(null)
const showEditor = ref(false)
const saving = ref(false)
const testing = ref('')
/** 每个模型最近一次测试结果，key = 模型 id */
const testResult = ref<Record<string, TestResult>>({})

const tabs = [
  { key: 'llm', label: '文本模型', desc: '剧本拆分 · 资产识别' },
  { key: 'image', label: '图像模型', desc: '角色 · 场景 · 道具出图' },
  { key: 'video', label: '视频模型', desc: '分镜转视频' },
  { key: 'tts', label: '语音模型', desc: '配音 · 对口型' }
]

const providers = [
  { value: 'rh-proxy', label: 'RH 中转 · 网关', baseUrl: 'http://localhost' },
  { value: 'openai', label: 'OpenAI', baseUrl: 'https://api.openai.com/v1' },
  { value: 'doubao', label: '豆包 · 火山方舟', baseUrl: 'https://ark.cn-beijing.volces.com/api/v3' },
  { value: 'runninghub', label: 'RunningHub', baseUrl: 'https://www.runninghub.cn/openapi/v2' },
  { value: 'minimax', label: 'MiniMax', baseUrl: 'https://api.minimax.chat/v1' },
  { value: 'custom', label: '自定义 / 兼容 OpenAI', baseUrl: '' }
]

interface RHWorkflow {
  name: string
  label: string
  kind: string
  desc: string
  requiredImg: number
  hasAspect: boolean
}

const rhWorkflows = ref<RHWorkflow[]>([])

const loadWorkflows = async () => {
  try {
    const res: any = await api.get('/admin/rh/workflows')
    rhWorkflows.value = res.data || []
  } catch (e) {
    console.error(e)
  }
}

/** 队列等待上限选项（毫秒） */
const queueOptions = [
  { value: 300000, label: '5 分钟' },
  { value: 900000, label: '15 分钟（默认）' },
  { value: 1800000, label: '30 分钟' },
  { value: 3600000, label: '1 小时' },
  { value: 0, label: '不限，一直等' }
]

const aspectOptions = ['9:16', '16:9', '1:1', '2:3', '3:2', '3:4', '4:3', '21:9']

const filtered = computed(() => list.value.filter((m) => m.category === activeTab.value))

const catCount = (cat: string) => list.value.filter((m) => m.category === cat).length

const emptyForm = () => ({
  id: '',
  provider: 'openai',
  category: activeTab.value,
  displayName: '',
  modelId: '',
  baseUrl: 'https://api.openai.com/v1',
  apiKey: '',
  params: '{}',
  creditsPerCall: 0,
  creditsPerUnit: 0,
  isDefault: false,
  enabled: true,
  sortOrder: 0,
  remark: '',
  // RH 中转专用（保存时序列化进 params）
  rhWorkflow: '',
  rhAspect: '9:16',
  rhQueue: 900000,
  rhDuration: 10,
  // 图像模型需要几张参考图：0=文生图，>0=图生图
  needRefImages: 0
})

const form = ref<any>(emptyForm())

const isRH = computed(() => form.value.provider === 'rh-proxy')

/** 当前分类对应的工作流候选 */
const rhKind = computed(() => (form.value.category === 'video' ? 'video' : 'image'))
const rhOptions = computed(() => rhWorkflows.value.filter((w) => w.kind === rhKind.value))
const currentRH = computed(() => rhWorkflows.value.find((w) => w.name === form.value.rhWorkflow))

/** 从模型配置里读一个参数 */
const paramOf = (m: ModelItem, key: string) => {
  try {
    const p = JSON.parse(m.params || '{}')
    return p[key]
  } catch {
    return undefined
  }
}

/** RH 模型在卡片上显示的工作流名 */
const rhLabel = (m: ModelItem) => {
  const name = paramOf(m, 'workflow')
  if (!name) return '未选工作流'
  return rhWorkflows.value.find((w) => w.name === name)?.label || name
}

const load = async () => {
  loading.value = true
  try {
    const res: any = await api.get('/admin/models')
    list.value = res.data || []
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  form.value = emptyForm()
  form.value.category = activeTab.value
  editing.value = null
  showEditor.value = true
}

const openEdit = (m: ModelItem) => {
  editing.value = m
  const p = (() => {
    try {
      return JSON.parse(m.params || '{}')
    } catch {
      return {}
    }
  })()
  form.value = {
    ...m,
    apiKey: '',
    rhWorkflow: p.workflow || '',
    rhAspect: p.aspectRatio || '9:16',
    rhQueue: typeof p.queueTimeoutMs === 'number' ? p.queueTimeoutMs : 900000,
    rhDuration: typeof p.duration === 'number' ? p.duration : 10,
    needRefImages: typeof p.needRefImages === 'number' ? p.needRefImages : (p.workflow === 'qwen-image-2.1-pe' ? 2 : 0)
  }
  showEditor.value = true
}

/** 选中 RH 中转时自动带出默认工作流与 Base URL */
const applyRHHints = () => {
  if (form.value.baseUrl === '' || form.value.baseUrl === 'https://api.openai.com/v1') {
    form.value.baseUrl = 'http://localhost'
  }
  if (!form.value.rhWorkflow) {
    const first = rhOptions.value[0]
    if (first) {
      form.value.rhWorkflow = first.name
      form.value.modelId = first.name
      if (!form.value.displayName) form.value.displayName = `RH · ${first.label}`
    }
  }
}

const onProviderChange = () => {
  const p = providers.find((x) => x.value === form.value.provider)
  if (p && p.baseUrl) form.value.baseUrl = p.baseUrl
  if (form.value.provider === 'rh-proxy') applyRHHints()
}

/** 选择工作流：模型 ID 直接等于工作流名 */
const onRHWorkflowChange = () => {
  const w = currentRH.value
  if (!w) return
  form.value.modelId = w.name
  if (!w.hasAspect) form.value.rhAspect = ''
  form.value.needRefImages = w.requiredImg || 0
  if (!form.value.displayName || form.value.displayName.startsWith('RH · ')) {
    form.value.displayName = `RH · ${w.label}`
  }
}

const save = async () => {
  if (!form.value.displayName.trim()) {
    toast.warning('信息不完整', '请填写显示名称')
    return
  }
  if (isRH.value && !form.value.rhWorkflow) {
    toast.warning('还没选工作流', 'RH 中转必须指定一个工作流才能出图/出片')
    return
  }
  if (!isRH.value && !form.value.modelId.trim()) {
    toast.warning('信息不完整', '模型 ID 是必填项')
    return
  }

  // RH 中转：工作流等参数统一序列化进 params，后端按此拼 inputs
  const payload: any = { ...form.value }
  if (isRH.value) {
    payload.modelId = form.value.rhWorkflow
    const p: any = { workflow: form.value.rhWorkflow, queueTimeoutMs: form.value.rhQueue }
    if (form.value.rhAspect) p.aspectRatio = form.value.rhAspect
    if (form.value.category === 'video' && form.value.rhDuration) p.duration = Number(form.value.rhDuration)
    if (form.value.category === 'image') p.needRefImages = Number(form.value.needRefImages) || 0
    payload.params = JSON.stringify(p)
  } else if (form.value.category === 'image') {
    // 其它厂商的图像模型：把参考图数量并进原 JSON
    let p: any = {}
    try {
      p = JSON.parse(form.value.params || '{}')
    } catch {
      p = {}
    }
    p.needRefImages = Number(form.value.needRefImages) || 0
    payload.params = JSON.stringify(p)
  }
  delete payload.rhWorkflow
  delete payload.rhAspect
  delete payload.rhQueue
  delete payload.rhDuration
  delete payload.needRefImages

  saving.value = true
  try {
    if (editing.value) {
      await api.put(`/admin/models/${editing.value.id}`, payload)
    } else {
      await api.post('/admin/models', payload)
    }
    showEditor.value = false
    toast.success(editing.value ? '模型已更新' : '模型已添加', form.value.displayName)
    await load()
  } catch (e: any) {
    toast.error('保存失败', e.response?.data?.message || e.message)
  } finally {
    saving.value = false
  }
}

const remove = async (m: ModelItem) => {
  const ok = await confirmDialog({
    title: `删除「${m.displayName}」？`,
    desc: '删除后该模型将无法被创作流程调用，已生成的内容不受影响。此操作不可撤销。',
    confirmText: '删除',
    danger: true
  })
  if (!ok) return
  try {
    await api.delete(`/admin/models/${m.id}`)
    toast.success('已删除', m.displayName)
    delete testResult.value[m.id]
    await load()
  } catch (e: any) {
    toast.error('删除失败', e.response?.data?.message || e.message)
  }
}

const setDefault = async (m: ModelItem) => {
  try {
    await api.post(`/admin/models/${m.id}/default`)
    toast.success('已设为默认模型', `${m.displayName} 将用于${categoryName(m.category)}环节`)
    await load()
  } catch (e: any) {
    toast.error('操作失败', e.response?.data?.message || e.message)
  }
}

const toggleEnabled = async (m: ModelItem) => {
  try {
    await api.put(`/admin/models/${m.id}`, { enabled: !m.enabled })
    toast.info(m.enabled ? '模型已停用' : '模型已启用', m.displayName)
    await load()
  } catch (e: any) {
    toast.error('操作失败', e.response?.data?.message || e.message)
  }
}

const categoryName = (cat: string) => tabs.find((t) => t.key === cat)?.label || cat

/** 把后端原始报错翻成一句人话，原始信息留在 title 里可查 */
const humanizeError = (raw: string): string => {
  const s = raw || ''
  // RH 中转网关的业务错误码
  if (/Token 无效|code -2|token invalid/i.test(s)) return '网关 Token 无效，检查「网关 Token」是否填对'
  if (/排队超时|code -5/i.test(s)) return '排队超时，把「排队上限」调大或设为不限'
  if (/队列已满|code -4/i.test(s)) return '网关队列已满，稍后重试'
  if (/上游执行错误|code -6/i.test(s)) return '上游工作流执行失败，检查提示词与参考图'
  if (/taskId|code -3/i.test(s)) return '任务不存在或已过期（结果 URL 仅 24 小时有效）'

  if (/401|Authentication|invalid.*key|api key/i.test(s)) return '密钥无效或已过期（401），请检查 API Key'
  if (/403|permission|forbidden/i.test(s)) return '没有该模型的访问权限（403）'
  if (/404|not found|model.*不存在/i.test(s)) return '模型不存在，检查模型 ID 与 Base URL（404）'
  if (/429|rate limit|quota|额度/i.test(s)) return '请求过于频繁或额度已用尽（429）'
  if (/timeout|deadline exceeded|超时/i.test(s)) return '请求超时，检查网络或 Base URL 是否可达'
  if (/connection refused|no such host|dial tcp|EOF/i.test(s)) return '连接不上该地址，检查 Base URL 是否正确'
  if (/5\d\d/.test(s)) return '对方服务端异常（5xx），稍后重试'
  // 去掉前缀和噪声括号，截断兜底
  const cleaned = s.replace(/^连通性测试失败[:：]\s*/, '').replace(/\{[^}]*\}/g, '').trim()
  return cleaned.length > 90 ? cleaned.slice(0, 90) + '…' : cleaned || s.slice(0, 90)
}

const test = async (m: ModelItem) => {
  if (testing.value) return
  testing.value = m.id
  const isGW = m.provider === 'rh-proxy'
  const tid = toast.loading(
    `正在${isGW ? '检测网关' : '测试'}「${m.displayName}」`,
    isGW ? '请求 /health 查看账号池状态' : '发送一次轻量请求，文本模型最长等待 30 秒'
  )

  try {
    const res: any = await api.post(`/admin/models/${m.id}/test`)
    const d = res.data || {}
    const ok = res.code === 0 && d.ok !== false
    toast.close(tid)

    if (ok) {
      // 文本模型有真实延迟与回复；RH 中转返回账号池；其他类型只做了配置校验
      const parts: string[] = []
      if (isGW) {
        parts.push(rhLabel(m))
        if (Array.isArray(d.accounts)) parts.push(`${d.accounts.length} 个账号在线`)
      } else if (d.modelId) {
        parts.push(d.modelId)
      }
      if (d.latencyMs != null) parts.push(`${d.latencyMs} ms`)
      if (d.tokens) parts.push(`${d.tokens} tokens`)
      if (d.reply) parts.push(`回复「${d.reply}」`)

      const detail = parts.length ? parts.join(' · ') : res.message
      toast.success(`「${m.displayName}」${isGW ? '网关正常' : '连通正常'}`, detail, 4000)

      testResult.value[m.id] = {
        ok: true,
        text: d.latencyMs != null ? `${isGW ? '网关正常' : '连通正常'} · ${d.latencyMs}ms` : '配置完整',
        detail,
        at: Date.now()
      }
      await load()
    } else {
      // 后端用 code!=0 + HTTP 200 表示测试未通过，需区分「配置没填」和「连不通」
      const msg = res.message || '测试未通过'
      if (/未配置/.test(msg)) {
        toast.warning(`「${m.displayName}」配置不完整`, `${msg}，点击「编辑」补齐后再测试`)
        testResult.value[m.id] = { ok: false, text: msg, detail: msg, at: Date.now() }
      } else {
        const friendly = humanizeError(msg)
        toast.error(`「${m.displayName}」连接失败`, friendly, 6000)
        testResult.value[m.id] = { ok: false, text: '连接失败', detail: `${friendly}\n\n原始信息：${msg}`, at: Date.now() }
      }
    }
  } catch (e: any) {
    toast.close(tid)
    const raw = e.response?.data?.message || e.message || '未知错误'
    const friendly = humanizeError(raw)
    toast.error(`「${m.displayName}」连接失败`, friendly, 6000)
    testResult.value[m.id] = { ok: false, text: '连接失败', detail: `${friendly}\n\n原始信息：${raw}`, at: Date.now() }
  } finally {
    testing.value = ''
  }
}

const sinceText = (at: number) => {
  const s = Math.floor((Date.now() - at) / 1000)
  if (s < 10) return '刚刚'
  if (s < 60) return `${s} 秒前`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分钟前`
  return `${Math.floor(m / 60)} 小时前`
}

onMounted(async () => {
  await Promise.all([load(), loadWorkflows()])
})
</script>

<template>
  <div class="models-page">
    <!-- 头部 -->
    <div class="page-head">
      <div>
        <h1>模型管理</h1>
        <p>配置各环节使用的 AI 模型、接入参数与计费规则</p>
      </div>
      <button class="btn btn-primary" @click="openCreate">
        <svg viewBox="0 0 24 24" fill="none" width="15" height="15">
          <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
        新增模型
      </button>
    </div>

    <!-- 分类 Tab -->
    <div class="tabs">
      <button
        v-for="t in tabs"
        :key="t.key"
        class="tab"
        :class="{ on: activeTab === t.key }"
        @click="activeTab = t.key"
      >
        <div class="tab-main">
          <span class="tab-label">{{ t.label }}</span>
          <span class="tab-count">{{ catCount(t.key) }}</span>
        </div>
        <span class="tab-desc">{{ t.desc }}</span>
      </button>
    </div>

    <!-- 列表 -->
    <div v-if="loading" class="loading">加载中…</div>

    <div v-else-if="filtered.length === 0" class="blank">
      <p>这个分类下还没有配置模型</p>
      <button class="btn btn-ghost btn-sm" @click="openCreate">添加第一个</button>
    </div>

    <div v-else class="model-list">
      <div v-for="m in filtered" :key="m.id" class="model-card" :class="{ disabled: !m.enabled }">
        <!-- 左：信息 -->
        <div class="mc-left">
          <div class="mc-head">
            <span class="mc-name">{{ m.displayName }}</span>
            <span v-if="m.isDefault" class="pill default">默认</span>
            <span v-if="!m.enabled" class="pill off">已停用</span>
            <span class="pill provider">{{ m.provider }}</span>
            <span
              v-if="testResult[m.id]"
              class="test-pill"
              :class="testResult[m.id].ok ? 'ok' : 'bad'"
              :title="testResult[m.id].detail"
            >
              <span class="tp-dot"></span>
              {{ testResult[m.id].text }}
              <span class="tp-time">{{ sinceText(testResult[m.id].at) }}</span>
            </span>
          </div>
          <div class="mc-id">{{ m.modelId }}</div>
          <div class="mc-url">{{ m.baseUrl || '未配置 Base URL' }}</div>

          <div class="mc-meta">
            <span v-if="m.provider === 'rh-proxy'" class="meta-item">
              <span class="mi-label">工作流</span>
              <span class="mi-value accent">{{ rhLabel(m) }}</span>
            </span>
            <span class="meta-item">
              <span class="mi-label">密钥</span>
              <span class="mi-value" :class="{ ok: m.hasKey, no: !m.hasKey }">
                {{ m.hasKey ? m.apiKeyMasked : '未配置' }}
              </span>
            </span>
            <span class="meta-item">
              <span class="mi-label">计费</span>
              <span class="mi-value">
                {{ m.creditsPerCall > 0 ? `${m.creditsPerCall} 积分/次` : m.creditsPerUnit > 0 ? `${m.creditsPerUnit} 积分/单位` : '免费' }}
              </span>
            </span>
            <span class="meta-item">
              <span class="mi-label">调用</span>
              <span class="mi-value">{{ m.callCount }} 次</span>
            </span>
          </div>

          <div v-if="m.remark" class="mc-remark">{{ m.remark }}</div>
        </div>

        <!-- 右：操作 -->
        <div class="mc-actions">
          <button
            class="act accent"
            :disabled="!!testing"
            :title="
              m.category === 'llm'
                ? '发一次真实请求验证密钥与地址'
                : m.provider === 'rh-proxy'
                  ? '请求网关 /health，查看账号池状态'
                  : '检查密钥与地址是否齐全'
            "
            @click="test(m)"
          >
            <span v-if="testing === m.id" class="spinner" style="width: 11px; height: 11px"></span>
            <template v-else>{{ m.category === 'llm' ? '测试连通' : m.provider === 'rh-proxy' ? '检测网关' : '校验配置' }}</template>
          </button>
          <button v-if="!m.isDefault" class="act" @click="setDefault(m)">设默认</button>
          <button class="act" @click="toggleEnabled(m)">{{ m.enabled ? '停用' : '启用' }}</button>
          <button class="act" @click="openEdit(m)">编辑</button>
          <button class="act danger" @click="remove(m)">删除</button>
        </div>
      </div>
    </div>

    <!-- 编辑弹窗 -->
    <Transition name="fade">
      <div v-if="showEditor" class="mask" @click.self="showEditor = false">
        <div class="drawer">
          <div class="drawer-head">
            <h3>{{ editing ? '编辑模型' : '新增模型' }}</h3>
            <button class="close" @click="showEditor = false">
              <svg viewBox="0 0 24 24" fill="none" width="17" height="17">
                <path d="M6 6l12 12M18 6 6 18" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
              </svg>
            </button>
          </div>

          <div class="drawer-body">
            <div class="frow">
              <label>显示名称 <span class="req">*</span></label>
              <input v-model="form.displayName" class="field" placeholder="比如：GPT-4o" />
            </div>

            <div class="frow two">
              <div>
                <label>厂商</label>
                <select v-model="form.provider" class="field" @change="onProviderChange">
                  <option v-for="p in providers" :key="p.value" :value="p.value">{{ p.label }}</option>
                </select>
              </div>
              <div>
                <label>分类</label>
                <select v-model="form.category" class="field">
                  <option v-for="t in tabs" :key="t.key" :value="t.key">{{ t.label }}</option>
                </select>
              </div>
            </div>

            <!-- RH 中转：工作流配置 -->
            <template v-if="isRH">
              <div class="rh-tip">
                <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
                  <path
                    d="M12 8.5v5M12 16.8v.1M10.3 3.9 2.6 17.2a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"
                    stroke="currentColor"
                    stroke-width="1.8"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                </svg>
                <span>走 RunningHub 中转网关：<code>/task/add</code> 提交 → 每 5 秒轮询 → 取产物。结果 URL 仅 24 小时有效，需及时转存。</span>
              </div>

              <div class="frow">
                <label>工作流 <span class="req">*</span></label>
                <select v-model="form.rhWorkflow" class="field" @change="onRHWorkflowChange">
                  <option value="" disabled>选择工作流</option>
                  <option v-for="w in rhOptions" :key="w.name" :value="w.name">
                    {{ w.label }} · {{ w.name }}
                  </option>
                </select>
                <span v-if="currentRH" class="hint">
                  {{ currentRH.desc }}<template v-if="currentRH.requiredImg > 0">；需 {{ currentRH.requiredImg }} 张参考图</template>
                </span>
                <span v-else-if="rhOptions.length === 0" class="hint">当前分类暂无可用工作流</span>
              </div>

              <div class="frow two">
                <div v-if="currentRH?.hasAspect">
                  <label>画幅比例</label>
                  <select v-model="form.rhAspect" class="field">
                    <option v-for="a in aspectOptions" :key="a" :value="a">{{ a }}</option>
                  </select>
                </div>
                <div v-if="form.category === 'video'">
                  <label>单镜时长（秒）</label>
                  <input v-model.number="form.rhDuration" class="field" type="number" min="1" max="60" />
                </div>
                <div>
                  <label>排队上限</label>
                  <select v-model.number="form.rhQueue" class="field">
                    <option v-for="q in queueOptions" :key="q.value" :value="q.value">{{ q.label }}</option>
                  </select>
                </div>
              </div>
            </template>

            <!-- 其他厂商：模型 ID -->
            <div v-else class="frow">
              <label>模型 ID <span class="req">*</span></label>
              <input v-model="form.modelId" class="field mono" placeholder="gpt-4o" />
              <span class="hint">请求 API 时传给 model 字段的值</span>
            </div>

            <div class="frow">
              <label>Base URL</label>
              <input
                v-model="form.baseUrl"
                class="field mono"
                :placeholder="isRH ? 'http://localhost' : 'https://api.openai.com/v1'"
              />
              <span v-if="isRH" class="hint">中转网关根地址，不带 /task 等路径</span>
            </div>

            <div class="frow">
              <label>{{ isRH ? '网关 Token' : 'API Key' }}</label>
              <input
                v-model="form.apiKey"
                class="field mono"
                type="password"
                :placeholder="editing ? '留空则不修改' : isRH ? 'Bearer token' : 'sk-...'"
              />
              <span class="hint">保存后仅显示后 4 位</span>
            </div>

            <!-- 图像模型：是否需要参考图 -->
            <div v-if="form.category === 'image'" class="frow">
              <label>参考图数量</label>
              <input v-model.number="form.needRefImages" class="field" type="number" min="0" max="4" />
              <span class="hint">
                0 = 文生图，出图时不需要参考图；2 = 图生图，必须选 2 张。
                <template v-if="isRH">选工作流时会自动带出（图编辑工作流为 2）。</template>
              </span>
            </div>

            <div v-if="!isRH" class="frow">
              <label>能力参数 (JSON)</label>
              <textarea v-model="form.params" class="field mono" rows="4" placeholder='{"temperature":0.7,"maxTokens":4096}'></textarea>
            </div>

            <div class="frow two">
              <div>
                <label>每次调用积分</label>
                <input v-model.number="form.creditsPerCall" class="field" type="number" min="0" />
              </div>
              <div>
                <label>每单位积分</label>
                <input v-model.number="form.creditsPerUnit" class="field" type="number" min="0" />
                <span class="hint">按张/秒计费用这个</span>
              </div>
            </div>

            <div class="frow">
              <label>备注</label>
              <input v-model="form.remark" class="field" placeholder="这个模型的用途说明" />
            </div>

            <div class="frow switches">
              <label class="sw">
                <input v-model="form.isDefault" type="checkbox" />
                <span>设为该类默认</span>
              </label>
              <label class="sw">
                <input v-model="form.enabled" type="checkbox" />
                <span>启用</span>
              </label>
            </div>
          </div>

          <div class="drawer-foot">
            <button class="btn btn-ghost" @click="showEditor = false">取消</button>
            <button class="btn btn-primary" :disabled="saving" @click="save">
              <span v-if="saving" class="spinner"></span>
              {{ saving ? '保存中' : '保存' }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.models-page {
  width: 100%;
}

.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 22px;
}
.page-head h1 { font-size: 22px; font-weight: 680; letter-spacing: -0.025em; }
.page-head p { font-size: 13px; color: var(--text-mute); margin-top: 3px; }

/* Tabs */
.tabs {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 9px;
  margin-bottom: 18px;
}
.tab {
  padding: 11px 13px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
  text-align: left;
  transition: all 0.24s var(--ease);
}
.tab:hover { border-color: var(--border-strong); }
.tab.on {
  border-color: rgba(124, 92, 255, 0.45);
  background: var(--accent-soft);
}
.tab-main { display: flex; align-items: center; gap: 7px; margin-bottom: 2px; }
.tab-label { font-size: 13px; font-weight: 580; }
.tab-count {
  min-width: 17px;
  height: 17px;
  padding: 0 5px;
  border-radius: 99px;
  display: inline-grid;
  place-items: center;
  font-size: 10px;
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.06);
}
.tab.on .tab-count { color: #c4b5fd; background: rgba(124, 92, 255, 0.2); }
.tab-desc { font-size: 10.5px; color: var(--text-mute); }

.loading { padding: 60px; text-align: center; color: var(--text-mute); font-size: 13px; }
.blank {
  padding: 60px 20px;
  text-align: center;
  border-radius: var(--r-lg);
  border: 1px dashed var(--border);
  color: var(--text-mute);
  font-size: 13px;
}
.blank p { margin-bottom: 14px; }

/* 模型卡 */
.model-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(380px, 1fr));
  gap: 10px;
}
.model-card {
  display: flex;
  gap: 16px;
  padding: 16px 18px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
  transition: all 0.24s var(--ease);
}
.model-card:hover { border-color: var(--border-strong); background: var(--surface); }
.model-card.disabled { opacity: 0.5; }

.mc-left { flex: 1; min-width: 0; }
.mc-head { display: flex; align-items: center; gap: 7px; margin-bottom: 4px; flex-wrap: wrap; }
.mc-name { font-size: 14.5px; font-weight: 620; letter-spacing: -0.015em; }
.pill {
  font-size: 10px;
  font-weight: 550;
  padding: 1px 7px;
  border-radius: 99px;
  border: 1px solid var(--border);
  color: var(--text-mute);
}
.pill.default {
  color: #c4b5fd;
  background: rgba(124, 92, 255, 0.16);
  border-color: rgba(124, 92, 255, 0.32);
}
.pill.off { color: var(--err); border-color: rgba(251, 113, 133, 0.3); background: rgba(251, 113, 133, 0.1); }
.pill.provider { font-family: 'SF Mono', Menlo, monospace; font-size: 9.5px; }

/* 最近测试结果 */
.test-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 10px;
  font-weight: 550;
  padding: 1.5px 8px;
  border-radius: 99px;
  border: 1px solid transparent;
  max-width: 260px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  cursor: help;
}
.test-pill.ok {
  color: #6ee7b7;
  background: rgba(52, 211, 153, 0.1);
  border-color: rgba(52, 211, 153, 0.28);
}
.test-pill.bad {
  color: #fda4b3;
  background: rgba(251, 113, 133, 0.1);
  border-color: rgba(251, 113, 133, 0.28);
}
.tp-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: currentColor;
  flex-shrink: 0;
  box-shadow: 0 0 0 2.5px color-mix(in srgb, currentColor 18%, transparent);
}
.test-pill.ok .tp-dot { animation: pulse 2.4s ease-in-out infinite; }
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
.tp-time {
  font-weight: 400;
  opacity: 0.6;
  font-size: 9.5px;
  margin-left: 1px;
}

.mc-id {
  font-size: 11.5px;
  font-family: 'SF Mono', Menlo, monospace;
  color: var(--text-dim);
  margin-bottom: 1px;
}
.mc-url {
  font-size: 11px;
  font-family: 'SF Mono', Menlo, monospace;
  color: var(--text-mute);
  margin-bottom: 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-meta { display: flex; gap: 18px; row-gap: 5px; flex-wrap: wrap; }
.meta-item { display: flex; align-items: center; gap: 5px; }
.mi-label { font-size: 10.5px; color: var(--text-mute); }
.mi-value { font-size: 11.5px; color: var(--text-dim); font-weight: 520; }
.mi-value.ok { color: var(--ok); font-family: 'SF Mono', Menlo, monospace; }
.mi-value.no { color: var(--err); }

.mc-remark {
  margin-top: 9px;
  font-size: 11.5px;
  color: var(--text-mute);
  padding: 6px 10px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.022);
  display: inline-block;
}

.mc-actions {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex-shrink: 0;
  justify-content: center;
  min-width: 72px;
  padding-left: 16px;
  border-left: 1px solid var(--border);
}
.act {
  height: 26px;
  padding: 0 11px;
  border-radius: 6px;
  font-size: 11.5px;
  color: var(--text-dim);
  border: 1px solid transparent;
  transition: all 0.2s var(--ease);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
}
.act:hover { background: var(--surface-hover); color: var(--text); border-color: var(--border); }
.act.danger:hover { color: var(--err); border-color: rgba(251, 113, 133, 0.3); }
.act:disabled { opacity: 0.5; cursor: not-allowed; }
.act.accent {
  color: #c4b5fd;
  background: rgba(124, 92, 255, 0.12);
  border-color: rgba(124, 92, 255, 0.26);
}
.act.accent:hover:not(:disabled) {
  background: rgba(124, 92, 255, 0.2);
  border-color: rgba(124, 92, 255, 0.45);
  color: #ddd3ff;
}

/* 弹窗 */
.mask {
  position: fixed;
  inset: 0;
  z-index: 300;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgba(0, 0, 0, 0.7);
  backdrop-filter: blur(6px);
}
.drawer {
  width: 100%;
  max-width: 520px;
  max-height: 88vh;
  display: flex;
  flex-direction: column;
  border-radius: var(--r-xl);
  background: #12121a;
  border: 1px solid var(--border-strong);
  box-shadow: 0 40px 90px -30px rgba(0, 0, 0, 0.9);
}
.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 22px 14px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.drawer-head h3 { font-size: 16px; font-weight: 650; letter-spacing: -0.02em; }
.close {
  width: 28px;
  height: 28px;
  border-radius: 7px;
  display: grid;
  place-items: center;
  color: var(--text-mute);
  transition: all 0.2s var(--ease);
}
.close:hover { background: var(--surface); color: var(--text); }

.drawer-body { flex: 1; overflow-y: auto; padding: 20px 22px; }
.drawer-foot {
  display: flex;
  justify-content: flex-end;
  gap: 9px;
  padding: 14px 22px 18px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

.frow { margin-bottom: 15px; }
.frow.two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.frow label {
  display: block;
  font-size: 12px;
  font-weight: 550;
  color: var(--text-dim);
  margin-bottom: 6px;
}
.req { color: var(--err); }
.field {
  width: 100%;
  height: 38px;
  padding: 0 12px;
  border-radius: 9px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.025);
  font-size: 13px;
  color: var(--text);
  transition: all 0.22s var(--ease);
}
textarea.field { height: auto; padding: 10px 12px; line-height: 1.65; }
.field:focus { border-color: rgba(124, 92, 255, 0.5); background: rgba(124, 92, 255, 0.05); }
.field.mono { font-family: 'SF Mono', Menlo, monospace; font-size: 12px; }
select.field { cursor: pointer; }
select.field option { background: #1a1a24; }
.hint { display: block; font-size: 10.5px; color: var(--text-mute); margin-top: 4px; }

/* RH 中转提示条 */
.rh-tip {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding: 10px 12px;
  margin-bottom: 15px;
  border-radius: 10px;
  font-size: 11.5px;
  line-height: 1.6;
  color: #c4b5fd;
  background: rgba(124, 92, 255, 0.09);
  border: 1px solid rgba(124, 92, 255, 0.22);
}
.rh-tip svg { flex-shrink: 0; margin-top: 2px; }
.rh-tip code {
  font-family: 'SF Mono', Menlo, monospace;
  font-size: 10.5px;
  padding: 1px 4px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.07);
}

.mi-value.accent { color: #c4b5fd; }

.switches { display: flex; gap: 20px; }
.sw {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  color: var(--text-dim);
  cursor: pointer;
  margin-bottom: 0;
}
.sw input[type='checkbox'] {
  width: 15px;
  height: 15px;
  accent-color: #7c5cff;
  cursor: pointer;
}

.fade-enter-active, .fade-leave-active { transition: opacity 0.24s var(--ease); }
.fade-enter-from, .fade-leave-to { opacity: 0; }
.fade-enter-active .drawer { animation: rise 0.3s var(--ease); }

@media (max-width: 720px) {
  .tabs { grid-template-columns: repeat(2, 1fr); }
  .model-card { flex-direction: column; gap: 12px; }
  .mc-actions { flex-direction: row; flex-wrap: wrap; }
  .frow.two { grid-template-columns: 1fr; }
}
</style>
