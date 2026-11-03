<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import { streamPOST } from '../api/stream'
import PillSelect from '../components/PillSelect.vue'
import PillToggle from '../components/PillToggle.vue'
import CharStylePreview from '../components/CharStylePreview.vue'
import SceneStylePreview from '../components/SceneStylePreview.vue'
import { toast } from '../composables/useToast'
import { confirmDialog } from '../composables/useConfirm'
import { mergeLatestTasks, isTaskRunning } from '../utils/taskState'

interface PillOption {
  value: string
  label: string
  hint?: string
  badge?: string
}

const route = useRoute()
const router = useRouter()
const projectId = route.params.id as string

interface Project { id: string; name: string; description: string; status: string }

const project = ref<Project | null>(null)
const loading = ref(true)
const step = ref(0)

// 已解锁到哪一步。只增不减：从分镜点回资产后，分镜仍要能点回去。
const maxStep = ref(0)
watch(step, (v) => { if (v > maxStep.value) maxStep.value = v }, { immediate: true })

// ---------- 剧本 ----------
const script = ref('')
const episodeId = ref('')
const savingScript = ref(false)

const episodes = ref<any[]>([])
const curEpisode = ref<any | null>(null)

/** 面包屑最后一节：第N集，title 里已写「第N集」时不重复拼 */
const episodeCrumb = computed(() => {
  const ep = curEpisode.value
  if (!ep) return ''
  const n = Number(ep.episodeNumber) || 1
  const t = String(ep.title || '').trim()
  if (!t) return `第${n}集`
  if (/第\s*[0-9０-９一二三四五六七八九十]+\s*集/.test(t)) return t
  return `第${n}集：${t}`
})

// ---------- 资产 ----------
const assets = ref<{ characters: any[]; scenes: any[]; items: any[] }>({
  characters: [], scenes: [], items: []
})
const identifying = ref(false)

// 资产出图工具栏
const genOpts = ref({
  modelId: '',
  resolution: '1K',
  charStyle: 'asset',
  sceneStyle: 'single',
  /** 角色名开关：开启后只有角色参考图左上角显示名字，场景与道具不显示 */
  showName: true
})

const RES_OPTIONS: PillOption[] = [
  { value: '1K', label: '1K', hint: '出图更快，长度够用' },
  { value: '2K', label: '2K', hint: '提示词追加高精度细节要求' }
]

const CHAR_STYLE_OPTIONS: PillOption[] = [
  { value: 'asset', label: '人物资产图', badge: '16:9', hint: '正面全身、背面全身与正面胸像' },
  { value: 'quad', label: '人物四视图', badge: '3:4', hint: '正侧脸特写、正面服装与背面全身' },
  { value: 'concept', label: '人物概念图', badge: '16:9', hint: '整体设计与服饰细节' },
  { value: 'closeup', label: '特写三视图', badge: '16:9', hint: '突出面部表情和细节' }
]

const SCENE_STYLE_BASE = [
  { value: 'single', label: '单一场景视图', hint: '一个平视主视角' },
  { value: 'multi', label: '多角度视图', hint: '主视角 + 俯视 + 局部细节' },
  { value: 'concept', label: '场景概念图', hint: '整体氛围与陈设细节' },
  { value: 'wide', label: '广角全景', hint: '超广角展示空间纵深' }
]

/** 剧本探测出的视觉约束（后端 identify 返回） */
const scriptStyle = ref<{ vertical: boolean; noFace: boolean; desc: string }>({
  vertical: false, noFace: false, desc: ''
})

const sceneAspect = (style: string) =>
  style === 'single' ? (scriptStyle.value.vertical ? '9:16' : '4:3') : '16:9'

const SCENE_STYLE_OPTIONS = computed<PillOption[]>(() =>
  SCENE_STYLE_BASE.map((o) => ({ ...o, badge: sceneAspect(o.value) }))
)

// ---------- 图像 / 视频模型 ----------
const imageModels = ref<any[]>([])
const videoModels = ref<any[]>([])

const modelOptions = computed<PillOption[]>(() =>
  imageModels.value.map((m) => {
    const need = Number(m.needRefImages ?? 0)
    const noKey = m.hasKey === false
    let hint = need > 0 ? `需 ${need} 张参考图 · 仅支持图生图` : '文生图 · 可直接批量'
    if (noKey) hint = '未配置 Token，选了会直接失败'
    return {
      value: m.id,
      label: (m.displayName || m.id) + (noKey ? '（未配 Token）' : ''),
      hint
    }
  })
)

const videoModelOptions = computed<PillOption[]>(() =>
  videoModels.value.map((m) => {
    const dur = Number(m.duration ?? 0)
    const ar = m.aspectRatio ? ` · ${m.aspectRatio}` : ''
    return {
      value: m.id,
      label: m.displayName || m.id,
      hint: (dur > 0 ? `默认 ${dur} 秒${ar}` : '按分镜时长') + (m.hasKey === false ? ' · 未配 Token' : '')
    }
  })
)

const loadModels = async () => {
  try {
    const [im, vm]: any[] = await Promise.all([
      api.get('/models', { params: { category: 'image' } }),
      api.get('/models', { params: { category: 'video' } })
    ])
    imageModels.value = im.data || []
    videoModels.value = vm.data || []
    // 默认选「有 key 且是默认」的那个，避免一进来就选到没配 token 的模型
    if (!genOpts.value.modelId) {
      const def = imageModels.value.find((m: any) => m.isDefault && m.hasKey !== false)
        || imageModels.value.find((m: any) => m.hasKey !== false)
      if (def) genOpts.value.modelId = def.id
    }
    if (!videoModel.value || !videoModels.value.some((m: any) => m.id === videoModel.value)) {
      const def = videoModels.value.find((m: any) => m.isDefault && m.hasKey !== false)
        || videoModels.value.find((m: any) => m.hasKey !== false)
      if (def) videoModel.value = def.id
    }
  } catch {
    /* 模型清单拿不到不阻塞页面 */
  }
}

// ---------- 分镜 ----------
const frames = ref<any[]>([])
// 首帧图功能：重新启用。
// 每段单独出一张首帧 —— 出片时 H3 以它为第一帧，构图、发型、座位都锁在图上，
// 比只给参考图 + 文字描述稳得多；画错了也能单独重出这一段，不用整场重来。
const ffEnabled = ref(true)
const generating = ref(false)
// 逐段生成的进度（SSE 推过来的）
const genProgress = ref({ done: 0, total: 0 })
// 大纲确认弹窗：先看拆分方案，确认后再逐段展开
const outlineOpen = ref(false)
const outlineLoading = ref(false)
const outlineList = ref<any[]>([])
const activeFrame = ref(0)

/** 画幅与视频模型属于这一集，存 episodes 表；时长按每帧描述词单独算 */
const sbRatio = ref<'16:9' | '9:16'>('16:9')
// 场景参考：出片时环境以哪份素材为准
//   image    场景原图（默认）
//   panorama 柱形全景图 —— 场景位换成全景图，房间四周都看得到
//   video    360 全景视频 —— 额外把视频作为参考视频传给 H3（节点 272）
// 从 localStorage 恢复：刷新后不该退回「场景图」。
// 优先级：本集分镜记录的值（loadFrames 时会同步覆盖）> 上次选的 > 默认
const SCENE_REF_KEY = 'frame:sceneRef'
const savedRef = (localStorage.getItem(SCENE_REF_KEY) || '') as 'image' | 'panorama' | 'video' | ''
const sbSceneRef = ref<'image' | 'panorama' | 'video'>(
  savedRef === 'panorama' || savedRef === 'video' || savedRef === 'image' ? savedRef : 'image'
)
watch(sbSceneRef, (v) => localStorage.setItem(SCENE_REF_KEY, v))
const SCENE_REF_OPTIONS: PillOption[] = [
  { value: 'image', label: '场景图', hint: '用场景原图当环境参考（默认）' },
  { value: 'panorama', label: '柱形全景图', hint: '场景位换成全景图，能看到房间四周' },
  { value: 'video', label: '360 全景视频', hint: '额外把全景视频作为参考视频传入，空间约束最强' }
]

const sbDuration = ref('15')
const videoModel = ref('')
const epCfg = { ratio: '', model: '' }

const RATIO_OPTIONS: PillOption[] = [
  { value: '16:9', label: '横屏 16:9', hint: '常规短剧 / 影视' },
  { value: '9:16', label: '竖屏 9:16', hint: '抖音 / 视频号' }
]

const DURATION_OPTIONS: PillOption[] = [
  { value: '15', label: '15 秒上限', hint: '一段最多 15 秒，按台词自动收短' },
  { value: '12', label: '12 秒上限', hint: '节奏更紧，口型更准' },
  { value: '10', label: '10 秒上限', hint: '单段台词少，适合快剪' },
  { value: 'auto', label: '自动', hint: '完全按台词字数估算' }
]

const applyEpisodeConfig = (ep: any) => {
  const ratio = ep?.aspectRatio === '9:16' ? '9:16' : '16:9'
  const model = ep?.videoModel || ''
  epCfg.ratio = ratio
  epCfg.model = model
  sbRatio.value = ratio
  if (model) videoModel.value = model
}

const saveEpisodeConfig = async (patch: Record<string, any>) => {
  if (!episodeId.value) return
  try {
    await api.put(`/episodes/${episodeId.value}/config`, patch)
  } catch {
    /* 配置保存失败不打断当前操作 */
  }
}

watch(sbRatio, (v) => {
  if (v === epCfg.ratio) return
  epCfg.ratio = v
  void saveEpisodeConfig({ aspectRatio: v })
})
watch(videoModel, (v) => {
  if (!v || v === epCfg.model) return
  epCfg.model = v
  void saveEpisodeConfig({ videoModel: v })
})

/** 「auto」→ 0（后端按台词估算）；「15」→ 15 */
const sbSeconds = computed(() => {
  if (sbDuration.value === 'auto') return 0
  const n = parseInt(sbDuration.value, 10)
  return Number.isFinite(n) && n > 0 ? n : 0
})

const totalDuration = computed(() =>
  Math.round(frames.value.reduce((s: number, f: any) => s + (Number(f.duration) || 0), 0))
)
const doneVideoCount = computed(() => frames.value.filter((f) => f.videoUrl).length)
const pendingVideoCount = computed(() => frames.value.filter((f) => !f.videoUrl).length)
const missingImages = computed(() => {
  const all = [...assets.value.characters, ...assets.value.scenes, ...assets.value.items]
  return all.filter((a) => !a.referenceImage).length
})

/** 资产三段（角色/场景/道具）统一成一张表，模板里少写三份重复结构 */
//
// type 必须显式声明成联合类型：数组字面量里的 'character' 会被 TS 推断成
// string，传给 openEdit(type: 'character'|'scene'|'item') 就报 TS2345。
type AssetType = 'character' | 'scene' | 'item'
interface AssetSection {
  key: string
  type: AssetType
  thumbCls: string
  label: string
  cls: string
  empty: string
  desc: string
  list: any[]
}

const assetSections = computed<AssetSection[]>(() => [
  { key: 'characters', type: 'character', thumbCls: 'person', label: '角色', cls: 'role', empty: '剧本里没提到明确的人物名', desc: 'imagePrompt', list: assets.value.characters },
  { key: 'scenes', type: 'scene', thumbCls: 'scene', label: '场景', cls: 'scene', empty: '剧本里没标注场景', desc: 'imagePrompt', list: assets.value.scenes },
  { key: 'items', type: 'item', thumbCls: 'item', label: '道具', cls: 'item', empty: '剧本里没识别到关键道具', desc: 'imagePrompt', list: assets.value.items }
])

/**
 * 这一段的参考图比例：跟着出图样式变。
 * 角色：四视图 3:4，其余 16:9；场景：单一视图跟剧本画幅（竖版 9:16 / 横版 4:3），
 * 多角度与广角 16:9；道具固定 1:1。卡片上的比例标就是给用户确认「出图会是什么画幅」。
 */
const ratioOf = (type: string) => {
  if (type === 'item') return '1:1'
  if (type === 'character') return genOpts.value.charStyle === 'quad' ? '3:4' : '16:9'
  return sceneAspect(genOpts.value.sceneStyle)
}

// ---------- 任务轮询 ----------
const taskMap = ref<Record<string, any>>({})
let pollTimer: number | null = null
const batchRunning = ref(false)
const generatingVideo = ref(false)

// ---------- 整集合成 ----------
// 把已出片的分镜按顺序拼成一条完整短剧。后端用 ffmpeg concat + -c copy，
// 十几段也是秒级，所以走同步接口，点一下转个圈就出结果。
const composing = ref(false)
const composed = ref<{ url: string; segments: number; duration: number; skipped: number[] } | null>(null)

const composeEpisode = async () => {
  if (!episodeId.value || composing.value) return
  composing.value = true
  try {
    const res: any = await api.post(`/episodes/${episodeId.value}/compose`)
    composed.value = res.data
    const sec = Math.round(Number(res.data.duration) || 0)
    toast.success('合成完成', `${res.data.segments} 段 · ${sec} 秒`)
  } catch (e: any) {
    toast.error('合成失败', e.response?.data?.message || e.message)
  } finally {
    composing.value = false
  }
}

const stopPolling = () => {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

const pollTasks = (onDone?: () => void) => {
  if (!episodeId.value) return
  stopPolling()
  // 已完成数量：每多完成一段就刷一次分镜
  // （以前只在"整批全跑完"时刷新 —— 25 段要等几十分钟，
  //   期间已经出好的视频在列表里看不见，用户只能手动刷新页面）
  let doneCount = -1
  pollTimer = window.setInterval(async () => {
    try {
      const res: any = await api.get('/tasks', { params: { episodeId: episodeId.value } })
      const { map, pending } = mergeLatestTasks(res.data || [])
      taskMap.value = map

      const nowDone = Object.values(map).filter(
        (t: any) => t && t.status === 'succeeded'
      ).length
      if (doneCount < 0) {
        doneCount = nowDone // 首次只记基线，不刷
      } else if (nowDone !== doneCount) {
        doneCount = nowDone
        await loadFrames() // 有新的完成 → 把 videoUrl 拉回来
      }

      if (pending === 0) {
        stopPolling()
        batchRunning.value = false
        generatingVideo.value = false
        await loadAssets()
        await loadFrames()
        onDone && onDone()
      }
    } catch {
      /* 轮询失败继续下一次 */
    }
  }, 2000)
}

/**
 * refreshTasks 拉一次任务列表，并把状态同步到 taskMap。
 * 刷新页面 / 切换剧集时调用：否则"生成中"标签和自动刷新都不会生效 ——
 * 用户看到的是图片始终不变，必须手动刷新才能看到新出的视频。
 */
const refreshTasks = async () => {
  if (!episodeId.value) return
  try {
    const res: any = await api.get('/tasks', { params: { episodeId: episodeId.value } })
    const { map, pending } = mergeLatestTasks(res.data || [])
    taskMap.value = map
    if (pending > 0) pollTasks() // 还有在跑的，接着轮询到结束
  } catch {
    /* 拉不到就算了，不影响主流程 */
  }
}

const taskOf = (id: string) => taskMap.value[id]

// ---------- 加载 ----------
const loadAssets = async () => {
  if (!episodeId.value) return
  try {
    const res: any = await api.get('/identify', { params: { episodeId: episodeId.value } })
    assets.value = {
      characters: res.data?.characters || [],
      scenes: res.data?.scenes || [],
      items: res.data?.items || []
    }
    if (res.data?.style) scriptStyle.value = res.data.style
  } catch {
    /* 没识别过就是空 */
  }
}

const loadFrames = async () => {
  if (!episodeId.value) return
  try {
    const res: any = await api.get('/storyboard', { params: { episodeId: episodeId.value } })
    frames.value = res.data?.frames || []
    if (activeFrame.value >= frames.value.length) activeFrame.value = 0
  } catch {
    /* 没有分镜就是空 */
  }
}

const loadFirstFrameGroups = async () => {
  if (!episodeId.value) return
  try {
    const res: any = await api.get(`/episodes/${episodeId.value}/firstframes`)
    ffGroups.value = res.data || []
  } catch {
    ffGroups.value = []
  }
}

const load = async () => {
  loading.value = true
  try {
    const res: any = await api.get(`/projects/${projectId}`)
    project.value = res.data

    const eps: any = await api.get('/episodes', { params: { projectId } })
    episodes.value = eps.data || []
    if (episodes.value.length > 0) {
      // 必须按 URL 上的 episodeId 加载：项目工作台点某一集的「创作」进来时
      // 会带 ?episodeId=xxx，之前这里永远取 episodes[0]，
      // 于是从第 2 集点进来、页面打开的却是第 1 集 ——
      // 用户以为在第 2 集，粘贴的剧本和识别出的资产全写进了第 1 集（实测踩过）。
      const wantId = String(route.query.episodeId || '')
      const ep = episodes.value.find((e: any) => e.id === wantId) || episodes.value[0]
      episodeId.value = ep.id
      curEpisode.value = ep
      script.value = ep.scriptText || ''
      // 换了剧集，上一集的成片不能再显示（否则会挂着别人的片子）
      composed.value = null
      applyEpisodeConfig(ep)
      await Promise.all([loadAssets(), loadFrames(), loadFirstFrameGroups()])
      // 拉一次任务状态：有任务在跑就自动接上轮询，
      // 不用等用户手动刷新才知道视频好了
      await refreshTasks()

      if (frames.value.length > 0) step.value = 2
      else if (assets.value.characters.length > 0) step.value = 1
    }
  } catch (e: any) {
    toast.error('加载失败', e.response?.data?.message || e.message)
  } finally {
    loading.value = false
  }
}

// ---------- 剧本 / 识别 ----------
const ensureEpisode = async (): Promise<string> => {
  if (episodeId.value) {
    await api.put(`/episodes/${episodeId.value}/script`, { scriptText: script.value })
    return episodeId.value
  }
  const res: any = await api.post('/episodes', {
    projectId,
    episodeNumber: 1,
    title: '第一集',
    scriptText: script.value
  })
  episodeId.value = res.data.id
  curEpisode.value = res.data
  // 追加而不是覆盖：覆盖会让下拉里只剩这一集，切不回别的集
  if (!episodes.value.some((e: any) => e.id === res.data.id)) {
    episodes.value = [...episodes.value, res.data]
  }
  return episodeId.value
}

const saveScript = async () => {
  if (savingScript.value) return
  savingScript.value = true
  try {
    await ensureEpisode()
    toast.success('草稿已保存')
  } catch (e: any) {
    toast.error('保存失败', e.response?.data?.message || e.message)
  } finally {
    savingScript.value = false
  }
}

const identify = async () => {
  if (!script.value.trim()) return toast.warning('请先填写剧本')
  identifying.value = true
  try {
    const eid = await ensureEpisode()
    const res: any = await api.post('/identify', { episodeId: eid })
    assets.value = {
      characters: res.data?.characters || [],
      scenes: res.data?.scenes || [],
      items: res.data?.items || []
    }
    if (res.data?.style) scriptStyle.value = res.data.style
    step.value = 1
    toast.success(
      `识别到 ${assets.value.characters.length} 个角色、${assets.value.scenes.length} 个场景、${assets.value.items.length} 个道具`
    )
  } catch (e: any) {
    toast.error('识别失败', e.response?.data?.message || e.message)
  } finally {
    identifying.value = false
  }
}

// ---------- 生成分镜 ----------
/** 先只拿大纲：拆错了在这里就能看出来，不用等整集跑完 */
const generateStoryboard = async () => {
  if (!episodeId.value) return toast.warning('请先识别资产')
  if (frames.value.length > 0) {
    const ok = await confirmDialog({
      title: '重新生成分镜？',
      desc: `当前 ${frames.value.length} 段分镜会被覆盖（已生成的视频保留在任务记录里，但分镜行会重建）。`,
      confirmText: '重新生成',
      danger: true
    })
    if (!ok) return
  }
  outlineLoading.value = true
  try {
    const res: any = await api.post('/storyboard/outline', { episodeId: episodeId.value })
    outlineList.value = res.data?.outline || []
    if (!outlineList.value.length) {
      toast.warning('没拆出段落', '剧本可能太短，或模型没返回有效大纲')
      return
    }
    outlineOpen.value = true
  } catch (e: any) {
    toast.error('大纲生成失败', e.response?.data?.message || e.message)
  } finally {
    outlineLoading.value = false
  }
}

/** 确认大纲后，按这份大纲逐段展开（不再重新规划） */
const runStoryboardStream = async () => {
  outlineOpen.value = false
  generating.value = true
  genProgress.value = { done: 0, total: 0 }
  let warnMsg = ''
  try {
    // 逐段生成：后端先出大纲，再一段段推过来。
    // 前端只负责显示进度 —— 全部完成后统一落库，中途失败不会清掉旧分镜。
    await streamPOST<any>(
      '/storyboard/generate/stream',
      // 场景参考在这里就定下来：提示词的详略按它写，并记到分镜上供出片沿用
      {
        episodeId: episodeId.value,
        duration: sbSeconds.value,
        sceneRef: sbSceneRef.value,
        outline: outlineList.value
      },
      (ev) => {
        if (ev.type === 'outline') {
          genProgress.value = { done: 0, total: ev.count || 0 }
        } else if (ev.type === 'frame') {
          genProgress.value = {
            done: ev.index || genProgress.value.done + 1,
            total: ev.total || genProgress.value.total
          }
        } else if (ev.type === 'error') {
          warnMsg = ev.message || warnMsg
        } else if (ev.type === 'done') {
          if (ev.message) warnMsg = ev.message
        }
      }
    )
    await loadFrames()
    activeFrame.value = 0
    step.value = 2
    // 工具栏显示成这一集实际用的场景参考，避免「显示场景图、实际按全景写的」
    const firstRef = frames.value[0]?.sceneRef
    if (firstRef) sbSceneRef.value = firstRef as any
    const n = frames.value.length
    if (warnMsg) {
      toast.warning(`生成 ${n} 段分镜`, warnMsg)
    } else {
      toast.success(`生成 ${n} 段分镜`)
    }
    await loadFirstFrameGroups()
  } catch (e: any) {
    toast.error('生成失败', e.message)
  } finally {
    generating.value = false
    genProgress.value = { done: 0, total: 0 }
  }
}

const activeFrameData = computed(() => frames.value[activeFrame.value] || null)

const saveFrame = async () => {
  const f = activeFrameData.value
  if (!f) return
  try {
    const res: any = await api.put(`/storyboard/${f.id}`, {
      text: f.text,
      videoPrompt: f.videoPrompt,
      duration: Number(f.duration) || 0
    })
    Object.assign(f, res.data)
    toast.success('已保存')
  } catch (e: any) {
    toast.error('保存失败', e.response?.data?.message || e.message)
  }
}

const regenerateFrame = async () => {
  const f = activeFrameData.value
  if (!f) return
  try {
    const res: any = await api.post(`/storyboard/${f.id}/regenerate`)
    Object.assign(f, res.data)
    toast.success('已重新生成')
  } catch (e: any) {
    toast.error('重生成失败', e.response?.data?.message || e.message)
  }
}

// ---------- 资产出图 ----------
const genPayload = () => ({
  modelId: genOpts.value.modelId || undefined,
  resolution: genOpts.value.resolution,
  charStyle: genOpts.value.charStyle,
  sceneStyle: genOpts.value.sceneStyle
})

const batchGenerateImages = async () => {
  if (!episodeId.value) return
  batchRunning.value = true
  try {
    const res: any = await api.post('/generate/images', { episodeId: episodeId.value, ...genPayload() })
    const count = res.data?.count || 0
    toast.success(`已提交 ${count} 个出图任务`, `消耗 ${res.data?.cost || 0} 积分`)
    pollTasks(() => toast.success('参考图已全部生成'))
  } catch (e: any) {
    batchRunning.value = false
    toast.error('提交失败', e.response?.data?.message || e.message)
  }
}

const regenAssetImage = async (assetType: string, assetId: string) => {
  taskMap.value[assetId] = { status: 'pending', progress: 0 }
  try {
    await api.post('/generate/image', { assetType, assetId, ...genPayload() })
    pollTasks(() => toast.success('参考图已更新'))
  } catch (e: any) {
    delete taskMap.value[assetId]
    toast.error('生成失败', e.response?.data?.message || e.message)
  }
}

// ---------- 场景全景：柱形全景图 → 360 视频 ----------
// 单张场景图只有正前方，过肩/反打镜头全靠模型编，环境和家具就会变；
// 全景图是让模型看清「房间其余角度长什么样」。
const panoramaBusy = ref<Record<string, boolean>>({})
const panoramaVideoBusy = ref<Record<string, boolean>>({})

/** 图片编辑模型：优先 qwen-image-2.1-pe（图生图），没有就退默认图片模型 */
const editModelId = () => {
  const m =
    imageModels.value.find((x: any) => String(x.workflow || '').includes('qwen-image-2.1-pe')) ||
    imageModels.value.find((x: any) => x.isDefault && x.hasKey !== false) ||
    imageModels.value.find((x: any) => x.hasKey !== false)
  return m?.id || ''
}

/** 全景视频工作流：model_id = panorama-to-360video 的那条配置 */
const panoramaVideoModelId = () =>
  videoModels.value.find((x: any) => x.workflow === 'panorama-to-360video')?.id || ''

const makePanorama = async (a: any) => {
  if (panoramaBusy.value[a.id]) return
  if (!a.referenceImage) return toast.warning('先生成场景图', '全景图是在场景图基础上往外扩展的')
  const modelId = editModelId()
  if (!modelId) return toast.warning('没有可用的图片编辑模型', '请到管理端添加图生图模型')
  panoramaBusy.value[a.id] = true
  taskMap.value[a.id] = { status: 'pending', progress: 0 }
  try {
    await api.post('/generate/panorama', { sceneId: a.id, modelId })
    pollTasks(() => toast.success('柱形全景图已生成'))
  } catch (e: any) {
    delete taskMap.value[a.id]
    toast.error('生成全景图失败', e.response?.data?.message || e.message)
  } finally {
    panoramaBusy.value[a.id] = false
  }
}

const makePanoramaVideo = async (a: any) => {
  if (panoramaVideoBusy.value[a.id]) return
  if (!a.panoramaImage) return toast.warning('先生成柱形全景图')
  const modelId = panoramaVideoModelId()
  if (!modelId) return toast.warning('没有配置全景视频工作流', '请在管理端添加 panorama-to-360video')
  panoramaVideoBusy.value[a.id] = true
  taskMap.value[a.id] = { status: 'pending', progress: 0 }
  try {
    await api.post('/generate/panorama-video', { sceneId: a.id, modelId })
    pollTasks(() => toast.success('360 全景视频已生成'))
  } catch (e: any) {
    delete taskMap.value[a.id]
    toast.error('生成全景视频失败', e.response?.data?.message || e.message)
  } finally {
    panoramaVideoBusy.value[a.id] = false
  }
}

// ---------- 资产：编辑 / 删除 / 大图 ----------
const editOpen = ref(false)
const editType = ref<'character' | 'scene' | 'item'>('character')
const editForm = ref<any>({ id: '', name: '', imagePrompt: '', referenceImage: '', identityAnchor: '', voiceTone: '', voiceSample: '' })
// 音色二选一：文字描述 或 参考音频，两者互斥（同时存在会让描述词和音频位打架）
const voiceMode = ref<'text' | 'audio'>('text')
// 切换只是切显示，不清数据 —— 来回切一下不该把填好的东西弄丢。
// 真正「二选一」在保存时做：只提交当前这一栏，另一栏传空。
const setVoiceMode = (m: 'text' | 'audio') => {
  voiceMode.value = m
}
const editSaving = ref(false)
const uploading = ref(false)
const viewerSrc = ref('')

const viewImage = (src?: string) => {
  if (src) viewerSrc.value = src
}

/** 场景卡内嵌视频：悬停播放 / 移出暂停（避免同时播放多个视频占资源） */
const playInline = (e: MouseEvent) => {
  const v = e.target as HTMLVideoElement
  v?.play?.().catch(() => {})
}
const pauseInline = (e: MouseEvent) => {
  const v = e.target as HTMLVideoElement
  v?.pause?.()
}

const openEdit = (type: 'character' | 'scene' | 'item', a: any) => {
  editType.value = type
  editForm.value = {
    id: a.id,
    name: a.name || '',
    imagePrompt: a.imagePrompt || '',
    referenceImage: a.referenceImage || '',
    identityAnchor: a.identityAnchor || '',
    // 二选一：已经有音频就停在「参考音频」，否则停在「文字描述」
    voiceTone: a.voiceTone || '',
    voiceSample: a.voiceSample || ''
  }
  voiceMode.value = a.voiceSample ? 'audio' : 'text'
  editOpen.value = true
}

/** 换参考图：先传到 /upload 拿 URL，保存时才写进资产 */
const uploadRef = async (e: Event) => {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', file)
    fd.append('kind', 'image')
    const res: any = await api.post('/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    editForm.value.referenceImage = res.data?.url || ''
    toast.success('已上传，点保存生效')
  } catch (err: any) {
    toast.error('上传失败', err.response?.data?.message || err.message)
  } finally {
    uploading.value = false
    input.value = ''
  }
}

/** 触发隐藏的 file input（模板里不能直接用 document） */
const pickVoiceFile = () => {
  const el = document.getElementById('voice-file-input') as HTMLInputElement | null
  el?.click()
}

/** 上传音色样本：mp3/wav/m4a，最长 15 秒（上传接口已按扩展名限制 30MB） */
const uploadVoice = async (e: Event) => {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', file)
    fd.append('kind', 'audio')
    const res: any = await api.post('/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    editForm.value.voiceSample = res.data?.url || ''
    toast.success('已上传音色样本', '点保存生效')
  } catch (err: any) {
    toast.error('上传失败', err.response?.data?.message || err.message)
  } finally {
    uploading.value = false
    input.value = ''
  }
}

const saveAsset = async () => {
  const f = editForm.value
  if (!f.id || editSaving.value) return
  if (!f.name.trim()) return toast.warning('名称不能为空')
  editSaving.value = true
  try {
    const payload: Record<string, any> = { name: f.name, imagePrompt: f.imagePrompt }
    // 身份锚定：视频描述词的「人物」行读的就是它，改年龄必须改这里
    if (editType.value === 'character') payload.identityAnchor = f.identityAnchor || ''
    if (f.referenceImage) payload.referenceImage = f.referenceImage
    if (editType.value === 'character') {
      // 二选一在保存时落地：只提交当前选中的那一栏，另一栏显式传空清掉
      if (voiceMode.value === 'audio') {
        payload.voiceTone = ''
        payload.voiceSample = f.voiceSample || ''
      } else {
        payload.voiceTone = f.voiceTone || ''
        payload.voiceSample = ''
      }
    }
    const res: any = await api.put(`/assets/${editType.value}/${f.id}`, payload)
    const list = assetSections.value.find((x) => x.type === editType.value)?.list || []
    const hit = list.find((x: any) => x.id === f.id)
    if (hit) Object.assign(hit, res.data || {})
    toast.success('已保存')
    editOpen.value = false
  } catch (e: any) {
    toast.error('保存失败', e.response?.data?.message || e.message)
  } finally {
    editSaving.value = false
  }
}

const removeAsset = async (type: string, a: any) => {
  const ok = await confirmDialog({
    title: `删除「${a.name}」？`,
    desc: '删除后这一集的分镜里对它的引用会失效，需要重新识别或手动加回。',
    confirmText: '删除',
    danger: true
  })
  if (!ok) return
  try {
    await api.delete(`/assets/${type}/${a.id}`)
    await loadAssets()
    toast.success('已删除')
  } catch (e: any) {
    toast.error('删除失败', e.response?.data?.message || e.message)
  }
}

// ---------- 视频 ----------
const generateOneVideo = async (f: any) => {
  if (!f) return
  const dur = Math.max(1, Math.round(Number(f.duration) || 5))
  taskMap.value[f.id] = { status: 'pending', progress: 0 }
  try {
    await api.post('/generate/video', {
      storyboardId: f.id,
      modelId: videoModel.value || undefined,
      duration: dur,
      aspectRatio: sbRatio.value
      // 不传 sceneRef：出片一律用分镜上记录的那个（生成分镜时定下来的）
    })
    toast.success(`已提交第 ${f.frameIndex} 段视频`, `${dur} 秒 · ${sbRatio.value}`)
    pollTasks()
  } catch (e: any) {
    delete taskMap.value[f.id]
    toast.error('提交失败', e.response?.data?.message || e.message)
  }
}

const generateVideos = async () => {
  if (!frames.value.length) return
  const todo = frames.value.filter((f) => !f.videoUrl)
  if (!todo.length) return toast.info('全部分镜已出片')
  const ok = await confirmDialog({
    title: `生成 ${todo.length} 段视频？`,
    desc: '按每段自己的时长逐条提交，提交后可在本页等待完成。',
    confirmText: '开始生成'
  })
  if (!ok) return

  generatingVideo.value = true
  let submitted = 0
  for (const f of todo) {
    try {
      await api.post('/generate/video', {
        storyboardId: f.id,
        modelId: videoModel.value || undefined,
        duration: Math.max(1, Math.round(Number(f.duration) || 5)),
        aspectRatio: sbRatio.value
        // 同上：用分镜记录的场景参考
      })
      submitted++
    } catch (e: any) {
      // 单个失败不中断整批，但把原因说出来
      if (e.response?.status === 402) {
        toast.error('积分不足', e.response?.data?.message || '')
        break
      }
    }
  }
  if (submitted === 0) {
    generatingVideo.value = false
    toast.error('没有提交成功的任务')
    return
  }
  toast.success(`已提交 ${submitted} 段视频`)
  pollTasks(() => toast.success('视频已全部生成'))
}

// ---------- 首帧规划 ----------
const ffGroups = ref<any[]>([])
const ffPlanOpen = ref(false)
const ffPlanning = ref(false)
const ffGenBusy = ref(false)

const parseIdx = (s: string): number[] => {
  try {
    const v = JSON.parse(s || '[]')
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

const ffGroupOfFrame = (frameIndex: number) => {
  for (const g of ffGroups.value) {
    if (parseIdx(g.frameIndexes).includes(frameIndex)) return g
  }
  return null
}

/** 当前段所属的首帧分组（可能没有：还没规划过） */
const activeGroup = computed(() => {
  const f = activeFrameData.value
  if (!f) return null
  return ffGroupOfFrame(f.frameIndex)
})

/** 覆盖段太多时只显示数量，否则标题会被挤成好几行 */
const coverText = (g: any) => {
  const idx = parseIdx(g?.frameIndexes || '')
  if (!idx.length) return ''
  return idx.length > 6 ? `覆盖 ${idx.length} 段` : `覆盖段 ${idx.join('、')}`
}

/** 当前段所属的那组参考图正在重新生成（详情区要显示等待遮罩） */
const ffRefreshing = computed(() => !!activeGroup.value && isFfBusy(activeGroup.value))

/** 左栏有没有东西可放：有视频、正在生成、或有场景全景参考图 */
const hasMedia = computed(() => {
  const f = activeFrameData.value
  if (!f) return false
  return (
    !!f.videoUrl ||
    isTaskRunning(taskOf(f.id)) ||
    (ffEnabled.value && !!activeGroup.value?.image)
  )
})

const planFirstFrames = async () => {
  if (!episodeId.value || ffPlanning.value) return
  ffPlanning.value = true
  try {
    const res: any = await api.post(`/episodes/${episodeId.value}/firstframe-plan`)
    ffGroups.value = res.data?.groups || []
    ffPlanOpen.value = true
    toast.success(res.message || '规划完成')
  } catch (e: any) {
    toast.error('规划失败', e.response?.data?.message || e.message)
  } finally {
    ffPlanning.value = false
  }
}

const openFirstFramePanel = async () => {
  await loadFirstFrameGroups()
  if (!ffGroups.value.length) {
    toast.info('还没有首帧规划', '先点「首帧规划」生成分组')
    return
  }
  ffPlanOpen.value = true
}

/** 这次正在生成哪几组（用来给缩略图加遮罩，而不是整块都在转圈） */
const ffBusyIds = ref<string[]>([])

/** 某一组是不是正在生成（已失败的不再算「生成中」，否则界面会一直转圈） */
const isFfBusy = (g: any) =>
  !!g && ffBusyIds.value.includes(g.id) && g.taskStatus !== 'failed'

/** 只重出某一组首帧：某段构图不对时单独重来，不用整批重跑 */
const genOneFirstFrame = async (g: any) => {
  // 允许同时重出多组：只有这一组自己在跑时才拦，别人不受影响
  if (!g?.id || ffBusyIds.value.includes(g.id)) return
  ffBusyIds.value = [...ffBusyIds.value, g.id]
  const before = g.image || ''
  try {
    const res: any = await api.post(`/episodes/${episodeId.value}/firstframes/generate`, {
      groupIds: [g.id]
    })
    toast.success(res.message || `第 ${g.sortOrder ?? ''} 组已提交`)
    let tries = 0
    const timer = window.setInterval(async () => {
      tries++
      try {
        const r: any = await api.get(`/episodes/${episodeId.value}/firstframes`)
        ffGroups.value = r.data || []
      } catch { /* 忽略 */ }
      const cur = ffGroups.value.find((x: any) => x.id === g.id)
      const done = !cur || cur.taskStatus === 'failed' || (!!cur.image && cur.image !== before)
      if (done || tries >= 40) {
        window.clearInterval(timer)
        ffBusyIds.value = ffBusyIds.value.filter((id: string) => id !== g.id)
        if (cur?.taskStatus === 'failed') {
          toast.error('这一组生成失败', cur.taskError || '上游报错，可再试一次')
        } else if (cur?.image && cur.image !== before) {
          toast.success('已更新')
        } else {
          toast.warning('还在生成，稍后刷新查看')
        }
      }
    }, 4000)
  } catch (e: any) {
    ffBusyIds.value = ffBusyIds.value.filter((id: string) => id !== g.id)
    toast.error('提交失败', e.response?.data?.message || e.message)
  }
}

const genFirstFrameGroups = async () => {
  if (!episodeId.value || ffGenBusy.value) return
  ffGenBusy.value = true
  ffBusyIds.value = ffGroups.value.map((g: any) => g.id)

  // 生成前的图：重生成时 URL 会换一个，拿它判断「这一组换图了没有」。
  // 只判断「有图」不行 —— 重生成时本来就有旧图，会立刻被当成完成。
  const before = new Map<string, string>(ffGroups.value.map((g: any) => [g.id, g.image || '']))

  try {
    const res: any = await api.post(`/episodes/${episodeId.value}/firstframes/generate`, {})
    toast.success(res.message || '已提交，正在生成')
    let tries = 0
    const timer = window.setInterval(async () => {
      tries++
      try {
        const r: any = await api.get(`/episodes/${episodeId.value}/firstframes`)
        ffGroups.value = r.data || []
      } catch { /* 轮询失败不打断 */ }
      const done = ffGroups.value.filter((g: any) => {
        if (!ffBusyIds.value.includes(g.id)) return true
        // 任务失败也算结束：只等「图变了」的话，失败时图永远不变，
        // 会一直转到 40 次轮询超时（200 秒），用户以为卡死了
        if (g.taskStatus === 'failed') return true
        const prev = before.get(g.id) || ''
        return !!g.image && g.image !== prev
      }).length
      if (done >= ffGroups.value.length || tries >= 40) {
        window.clearInterval(timer)
        // 先取失败清单，下面会把 ffBusyIds 清空
        const failed = ffGroups.value.filter(
          (g: any) => ffBusyIds.value.includes(g.id) && g.taskStatus === 'failed'
        )
        ffGenBusy.value = false
        ffBusyIds.value = []
        const finished = ffGroups.value.filter((g: any) => !!g.image).length
        if (failed.length > 0) {
          toast.error(`首帧图失败 ${failed.length} 组`, failed[0].taskError || '上游报错，可重试')
        } else if (finished > 0) {
          toast.success(`首帧图完成 ${finished}/${ffGroups.value.length}`)
        } else {
          toast.warning('首帧图还在生成，稍后刷新查看')
        }
      }
    }, 5000)
  } catch (e: any) {
    ffGenBusy.value = false
    ffBusyIds.value = []
    toast.error('生成失败', e.response?.data?.message || e.message)
  }
}

/** 改一组的构图描述（景别写不准时手工修一句，比重新规划省事） */
const saveGroupDesc = async (g: any) => {
  try {
    await api.put(`/firstframes/${g.id}`, { description: g.description })
    toast.success('已保存构图描述')
  } catch (e: any) {
    toast.error('保存失败', e.response?.data?.message || e.message)
  }
}

// ---------- 分镜表（整集每一镜的时间轴 / 台词 / 字数 / 语速） ----------
const shotSheetOpen = ref(false)

/**
 * 把描述词拆成镜头列表。
 *
 * 现在后端输出的是「镜号：1 / 景别：中景/老师 / 运镜：固定镜头 /
 * 情绪动作：… / 台词：老师：… / 音效：… / 时长：2s」这种分段写法；
 * 库里还有老数据是「｜镜头1｜ 0–2秒｜中景｜固定机位｜」，
 * 两种都要能读，分镜表和镜头拆解才不会因为换格式就空掉。
 */
const parseFrameShots = (vp: string) => {
  const shots: any[] = []
  const text = vp || ''

  // ---------- H3 结构：【Shot 1｜0–3秒｜双人中景·@老师】+ 五个子项 ----------
  const h3: any[] = []
  let h3cur: any = null
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    // 官方时间轴写法：[Shot 1] 0.00–3.00s，双人中景，主体 @老师
    // 兼容上一版：【Shot 1｜0–3秒｜双人中景·@老师】
    const m =
      line.match(/^\[Shot\s*(\d+)\]\s*([\d.]+)\s*[–—\-~～至]\s*([\d.]+)\s*s[，,]\s*([^，,]*)/) ||
      line.match(/^【Shot\s*(\d+)｜\s*([\d.]+)\s*[–—\-~～至]\s*([\d.]+)\s*秒｜\s*([^】]*)】$/)
    if (m) {
      if (h3cur) h3.push(h3cur)
      const start = parseFloat(m[2])
      const end = parseFloat(m[3])
      // 「双人中景·@老师」→ 景别只取「·」前那半
      let size = (m[4] || '').trim()
      if (size.includes('·')) size = size.split('·')[0].trim()
      h3cur = {
        no: parseInt(m[1], 10) || h3.length + 1,
        start,
        end,
        dur: Math.max(0, end - start),
        size,
        camera: '',
        line: '',
        action: '',
        chars: 0,
        rate: 0
      }
      continue
    }
    if (!h3cur) continue
    if (line.startsWith('【主体】')) {
      h3cur.action = line.slice('【主体】'.length).trim()
    } else if (line.startsWith('【动作】')) {
      const v = line.slice('【动作】'.length).trim()
      h3cur.action = h3cur.action ? h3cur.action + '；' + v : v
    } else if (line.startsWith('【镜头】')) {
      h3cur.camera = line.slice('【镜头】'.length).trim()
    } else {
      // 台词行：(S2) @老师 <d>[中文] 正文</d>
      const dm = line.match(/<d>\s*(?:\[[^\]]*\]\s*)?([\s\S]*?)\s*<\/d>/)
      if (dm) {
        const body = dm[1].trim()
        if (body) h3cur.line = h3cur.line ? h3cur.line + ' ' + body : body
      }
    }
  }
  if (h3cur) h3.push(h3cur)

  if (h3.length) {
    for (const sh of h3) {
      const chars = (sh.line.match(/[\u4e00-\u9fa5A-Za-z0-9]/g) || []).length
      const pauses = (sh.line.match(/——|—{2,}|-{2,}/g) || []).length
      const speak = Math.max(0, sh.dur - pauses * 0.8)
      sh.chars = chars
      sh.rate = speak > 0 && chars > 0 ? chars / speak : 0
    }
    return h3
  }

  // ---------- 上一版平铺格式（镜号/景别/运镜/情绪动作/台词/音效/时长） ----------
  let cur: any = null
  let cursor = 0
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    if (line.startsWith('镜号：')) {
      if (cur) shots.push(cur)
      const no = parseInt(line.replace('镜号：', ''), 10)
      cur = {
        no: Number.isFinite(no) ? no : shots.length + 1,
        start: cursor,
        end: cursor,
        dur: 0,
        size: '',
        camera: '',
        line: '',
        action: '',
        chars: 0,
        rate: 0
      }
      continue
    }
    if (!cur) continue
    if (line.startsWith('景别：')) {
      const v = line.slice(3).trim()
      cur.size = v.includes('/') ? v.split('/')[0] : v
    } else if (line.startsWith('运镜：')) {
      cur.camera = line.slice(3).trim()
    } else if (line.startsWith('情绪/动作：')) {
      cur.action = line.slice('情绪/动作：'.length).trim()
    } else if (line.startsWith('台词：')) {
      const v = line.slice(3).trim()
      if (v && v !== '无') {
        // 「(S1) @老师：<d>[中文] 坐。</d>」→ 只留台词正文，角色单独存。
        // H3 的 <d> 标记和 [中文] 语言名不能留在正文里：
        // 一是界面上会显示成标签，二是字数统计会把「中文」两个字算进语速
        const idx = v.indexOf('：')
        let body = idx > 0 ? v.slice(idx + 1) : v
        body = body
          .replace(/^<d>\s*(\[[^\]]*\]\s*)?/, '')
          .replace(/<\/d>\s*$/, '')
          .trim()
        if (body) cur.line = cur.line ? cur.line + ' ' + body : body
      }
    } else if (line.startsWith('时长：')) {
      const n = parseInt(line.replace('时长：', ''), 10)
      if (Number.isFinite(n) && n > 0) cur.dur = n
    }
  }
  if (cur) shots.push(cur)

  if (shots.length) {
    for (const sh of shots) {
      sh.start = cursor
      sh.end = cursor + (sh.dur || 0)
      cursor = sh.end
      const chars = (sh.line.match(/[\u4e00-\u9fa5A-Za-z0-9]/g) || []).length
      const pauses = (sh.line.match(/——|—{2,}|-{2,}/g) || []).length
      const speak = Math.max(0, sh.dur - pauses * 0.8)
      sh.chars = chars
      sh.rate = speak > 0 && chars > 0 ? chars / speak : 0
    }
    return shots
  }

  // ---------- 镜头表（｜镜头N｜ 0–2秒｜中景｜固定机位｜） ----------
  // 分隔符兼容全角 ｜ 和半角 |：新格式（参考站风格）输出的是半角
  // 「|镜头1| 0-6秒| 双人中景| 固定机位|」，老数据是全角
  let old: any = null
  const bodies: string[][] = []
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    // 【执行要点】是全局规则，不属于任何镜头：
    // 它的正文不以【开头，会被误当成最后一个镜头的动作
    if (line.startsWith('【执行要点】')) break
    const m = line.match(/^[｜|]镜头\s*(\d+)[｜|]\s*([\d.]+)\s*[–—\-~～至]\s*([\d.]+)\s*秒[｜|]\s*([^｜|]*)(?:[｜|]\s*([^｜|]*))?/)
    if (m) {
      if (old) shots.push(old)
      old = {
        no: m[1],
        start: parseFloat(m[2]),
        end: parseFloat(m[3]),
        dur: Math.max(0, parseFloat(m[3]) - parseFloat(m[2])),
        size: (m[4] || '').trim(),
        camera: (m[5] || '').trim(),
        line: '',
        action: '',
        chars: 0,
        rate: 0
      }
      bodies.push([])
      continue
    }
    if (old) bodies[bodies.length - 1].push(line)
  }
  if (old) shots.push(old)

  return shots.map((sh, i) => {
    const body = (bodies[i] || []).join('\n')
    // 台词用贪婪 (.*) 取到行末最后一个 ”：台词内部可能还有引号
    const dl = body.split('\n').map((x) => x.trim()).find((x) => x.includes('台词】'))
    // 台词正文可能在引号里，也可能被 <d>[Chinese] …</d> 包着，统一剥掉
    let line = ''
    if (dl) {
      // 取「台词】」之后第一个冒号后的内容：正文里本身可能有冒号
      // （「三个问题：…」），用 lastIndexOf 会被正文里的冒号截断
      const base = dl.indexOf('台词】')
      const ci = dl.indexOf('：', base + 3)
      line = (ci > 0 ? dl.slice(ci + 1) : '')
        .replace(/^[“"]/, '')
        .replace(/[”"]$/, '')
        .replace(/<\/?d>/g, '')
        .replace(/\[[A-Za-z]+\]/g, '')
        .trim()
    }
    let action = ''
    const ai = body.indexOf('【动作】')
    if (ai >= 0) {
      for (const raw of body.slice(ai + 4).split('\n')) {
        const t = raw.trim().replace(/^（[^）]*）/, '').trim()
        if (t && !t.startsWith('【')) {
          action = t
          break
        }
      }
    } else {
      action =
        body
          .split('\n')
          .map((x) => x.trim())
          .find((x) => x && !x.startsWith('【') && !x.startsWith('｜') && !x.startsWith('|')) || ''
    }
    const chars = (line.match(/[\u4e00-\u9fa5A-Za-z0-9]/g) || []).length
    const pauses = (line.match(/——|—{2,}|-{2,}/g) || []).length
    const speak = Math.max(0, sh.dur - pauses * 0.8)
    return { ...sh, line, action, chars, rate: speak > 0 && chars > 0 ? chars / speak : 0 }
  })
}

const shotSheetRows = computed(() => {
  const rows: any[] = []
  frames.value.forEach((f: any, fi: number) => {
    const shots = parseFrameShots(f.videoPrompt || '')
    const frameNo = f.frameIndex || fi + 1
    if (!shots.length) {
      rows.push({ frame: frameNo, no: '—', start: 0, end: 0, dur: Number(f.duration) || 0, size: '', camera: '', line: '', action: '', chars: 0, rate: 0 })
      return
    }
    shots.forEach((sh) => rows.push({ frame: frameNo, ...sh }))
  })
  return rows
})

/** 语速异常的行（> 5 字/秒 基本念不完，会导致截断/口型错位） */
const slowRows = computed(() => shotSheetRows.value.filter((r) => r.rate > 5).length)

const gotoFrame = (frameNo: number) => {
  const i = frames.value.findIndex((f: any, idx: number) => (f.frameIndex || idx + 1) === frameNo)
  if (i >= 0) {
    activeFrame.value = i
    shotSheetOpen.value = false
  }
}

// ---------- 其它 ----------
const frameShots = computed(() => parseFrameShots(activeFrameData.value?.videoPrompt || ''))
const back = () => router.push('/projects')

/**
 * 某一步能不能点进去。
 * 只按「走过的最远一步」会锁死：从分镜回退到剧本后，成片那格就再也点不动了
 * （maxStep 还停在 2）。有分镜就等于分镜/成片都已解锁。
 */
const canGoStep = (i: number) => i <= maxStep.value || (i >= 2 && frames.value.length > 0)

/** 面包屑里切集：换集要重新拉资产、分镜、首帧分组 */
const switchEpisode = async () => {
  const ep = episodes.value.find((e) => e.id === episodeId.value)
  if (!ep) return
  curEpisode.value = ep
  script.value = ep.scriptText || ''
  applyEpisodeConfig(ep)
  // 集号同步进 URL：刷新、前进后退都停在这一集，复制链接给别人也打开同一集
  router.replace({ path: route.path, query: { ...route.query, episodeId: episodeId.value } })
  activeFrame.value = 0
  ffGroups.value = []
  // 换集先停掉上一集的轮询，避免把别的集的任务状态混过来
  stopPolling()
  taskMap.value = {}
  await Promise.all([loadAssets(), loadFrames(), loadFirstFrameGroups()])
  await refreshTasks()
  step.value = frames.value.length > 0 ? 2 : assets.value.characters.length > 0 ? 1 : 0
}

/** 大图查看时按 Esc 关闭 */
const onKeydown = (e: KeyboardEvent) => {
  if (e.key === 'Escape') viewerSrc.value = ''
}

/**
 * v-autosize：textarea 高度跟随内容自动增长。
 *
 * 首帧规划的描述经常有 5-8 行（28 段的全景描述），固定 3 行高要来回滚动
 * 才能看全，改起来也容易漏行。用 max-height 兜底，超长才出滚动条。
 */
const vAutosize = {
  mounted(el: any) {
    const fit = () => {
      el.style.height = 'auto'
      el.style.height = `${el.scrollHeight}px`
    }
    el.__fit = fit
    el.addEventListener('input', fit)
    fit()
  },
  updated(el: any) {
    el.__fit?.()
  },
  unmounted(el: any) {
    if (el.__fit) el.removeEventListener('input', el.__fit)
  },
}

onMounted(async () => {
  document.addEventListener('keydown', onKeydown)
  await loadModels()
  await load()
})
onUnmounted(() => {
  stopPolling()
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="studio">
    <div v-if="loading" class="loading">加载中…</div>

    <template v-else-if="project">
      <!-- 顶部：返回胶囊 + 面包屑（首页 › 我的项目 › 项目 › 第N集） -->
      <div class="top-bar">
        <button class="back" @click="back">
          <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
            <path d="M15 6l-6 6 6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          返回
        </button>
        <span class="tb-sep"></span>
        <nav class="crumbs">
          <a class="crumb home" @click="router.push('/')">
            <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
              <path d="M4 10.5 12 4l8 6.5V19a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 19v-8.5Z" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
            </svg>
            首页
          </a>
          <span class="crumb-sep">›</span>
          <a class="crumb" @click="router.push('/projects')">我的项目</a>
          <span class="crumb-sep">›</span>
          <a class="crumb" @click="router.push(`/projects/${projectId}`)">{{ project.name }}</a>
          <span class="crumb-sep">›</span>
          <select
            v-if="episodes.length > 1"
            v-model="episodeId"
            class="crumb-sel"
            @change="switchEpisode"
          >
            <option v-for="e in episodes" :key="e.id" :value="e.id">
              第 {{ e.episodeNumber }} 集{{ e.title && !/第\s*\d+\s*集/.test(e.title) ? '：' + e.title : '' }}
            </option>
          </select>
          <span v-else class="crumb on">{{ episodeCrumb || '未命名剧集' }}</span>
        </nav>
      </div>

      <!-- 步骤条 -->
      <div class="stepper">
        <div
          v-for="(s, i) in [
            { key: 'script', label: '剧本', desc: '写或粘贴你的故事' },
            { key: 'assets', label: '资产', desc: '角色场景道具' },
            { key: 'storyboard', label: '分镜', desc: '镜头与描述词' },
            { key: 'video', label: '成片', desc: '批量生成视频' }
          ]"
          :key="s.key"
          class="step"
          :class="{ on: i === step, done: i < step, locked: !canGoStep(i) }"
          @click="canGoStep(i) && (step = i)"
        >
          <div class="step-dot">
            <svg v-if="i < step" viewBox="0 0 24 24" fill="none" width="12" height="12">
              <path d="m5 12.5 4.5 4.5L19 7" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span v-else>{{ i + 1 }}</span>
          </div>
          <div class="step-text">
            <span class="step-label">{{ s.label }}</span>
            <span class="step-desc">{{ s.desc }}</span>
          </div>
        </div>
      </div>

      <!-- ===== 步骤 0：剧本 ===== -->
      <div v-if="step === 0" class="panel rise">
        <div class="panel-head">
          <h2>写下你的故事</h2>
          <p>建议包含「场景：」「人物：」「道具：」标记，识别更准</p>
        </div>

        <div class="editor">
          <div class="editor-bar">
            <div class="dots"><i></i><i></i><i></i></div>
            <span class="editor-file">script.txt</span>
            <span class="editor-count">{{ script.length }} 字</span>
          </div>
          <textarea
            v-model="script"
            class="editor-body"
            placeholder="场景：咨询间 - 白天
人物：生涯规划老师、高中学生
道具：专业介绍册

老师：坐。你说对土木有兴趣，又怕“就是去工地”。先说说你印象里的土木是什么？
学生：盖房子、修路架桥吧，听说毕业常驻工地。"
          ></textarea>
        </div>

        <div class="panel-foot">
          <span class="foot-hint">
            <template v-if="script.length < 50">建议 50 字以上，AI 识别更准</template>
            <template v-else>剧本就绪，可以识别资产了</template>
          </span>
          <div class="foot-actions">
            <button class="btn btn-ghost" :disabled="savingScript" @click="saveScript">
              {{ savingScript ? '保存中' : '保存草稿' }}
            </button>
            <button class="btn btn-primary" :disabled="!script.trim() || identifying" @click="identify">
              <span v-if="identifying" class="spinner"></span>
              {{ identifying ? '识别中' : '识别角色与场景' }}
            </button>
          </div>
        </div>
      </div>

      <!-- ===== 步骤 1：资产 ===== -->
      <div v-else-if="step === 1" class="panel rise">
        <div class="panel-head">
          <div class="ph-row">
            <div>
              <h2>识别到的资产</h2>
              <p>这些是后续所有镜头的形象锚点，确认无误后继续</p>
            </div>
          </div>
        </div>

        <!-- 生成配置工具栏：与竞品同构，先定好出图规格再批量 -->
        <div class="gen-toolbar">
          <div class="gt-left">
            <PillSelect v-model="genOpts.modelId" label="模型" :options="modelOptions" :width="148" />
            <span class="gt-sep"></span>
            <PillSelect v-model="genOpts.resolution" label="分辨率" :options="RES_OPTIONS" />
            <span class="gt-sep"></span>
            <PillSelect
              v-model="genOpts.charStyle"
              label="角色样式"
              :options="CHAR_STYLE_OPTIONS"
              :width="104"
              previewable
            >
              <template #preview="{ option }">
                <CharStylePreview :style="option.value" :title="option.label" :desc="option.hint" />
              </template>
            </PillSelect>
            <PillSelect
              v-model="genOpts.sceneStyle"
              label="场景样式"
              :options="SCENE_STYLE_OPTIONS"
              :width="116"
              previewable
            >
              <template #preview="{ option }">
                <SceneStylePreview
                  :style="option.value"
                  :vertical="scriptStyle.vertical"
                  :title="option.label"
                  :desc="option.hint"
                />
              </template>
            </PillSelect>
            <span class="gt-sep"></span>
            <!-- 场景参考：资产页也能选 —— 场景图/全景图/360 视频都在这一页，
                 选完去生成分镜，提示词的详略就按它写 -->
            <PillSelect v-model="sbSceneRef" label="场景参考" :options="SCENE_REF_OPTIONS" :width="132" />
            <span class="gt-sep"></span>
            <PillToggle
              v-model="genOpts.showName"
              label="角色名"
              hint="开启后，仅角色参考图左上角显示角色名称，场景与道具图不显示"
            />
          </div>
          <div class="gt-right">
            <button
              class="btn btn-ghost btn-sm"
              :disabled="batchRunning || missingImages === 0"
              @click="batchGenerateImages"
            >
              <span v-if="batchRunning" class="spinner"></span>
              <svg v-else viewBox="0 0 24 24" fill="none" width="14" height="14">
                <rect x="3" y="4" width="18" height="16" rx="2.5" stroke="currentColor" stroke-width="1.7" />
                <circle cx="8.5" cy="9.5" r="1.6" fill="currentColor" />
                <path d="m4 17 5-4.5 4 3.5 3-2.5 4 3.5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              {{ batchRunning ? '生成中' : missingImages > 0 ? `生成参考图（${missingImages}）` : '参考图已齐' }}
            </button>
            <button class="btn btn-primary btn-sm" :disabled="generating" @click="generateStoryboard">
              <span v-if="generating" class="spinner"></span>
              {{ generating ? (genProgress.total ? `生成中 ${genProgress.done}/${genProgress.total}` : '生成中…') : '下一步：生成分镜' }}
            </button>
          </div>
        </div>

        <div class="asset-wrap">
          <div class="asset-cols">
          <div v-for="sec in assetSections" :key="sec.key" class="asset-section">
            <div class="asset-title">
              <span class="dot" :class="sec.cls"></span>{{ sec.label }}
              <span class="count">{{ sec.list.length }}</span>
              <span class="sec-ratio">{{ ratioOf(sec.type) }}</span>
            </div>

            <div v-if="sec.list.length" class="asset-grid" :class="{ 'scene-grid': sec.type === 'scene' }">
              <div v-for="a in sec.list" :key="a.id" class="asset-card" :class="{ 'scene-card': sec.type === 'scene' }">
                <!-- 场景卡：左=原场景图卡片，右=柱形全景图 / 360视频 上下两格 -->
                <template v-if="sec.type === 'scene'">
                  <div class="scene-main">
                    <div class="asset-thumb" :class="sec.thumbCls">
                      <img
                        v-if="a.referenceImage"
                        :src="a.referenceImage"
                        :alt="a.name"
                        @click="viewImage(a.referenceImage)"
                      />
                      <template v-else>{{ a.name?.[0] }}</template>
                      <div v-if="isTaskRunning(taskOf(a.id))" class="thumb-mask">
                        <span class="spinner"></span>
                      </div>
                    </div>
                    <div class="asset-body">
                      <div class="ab-head">
                        <h4>
                          <span class="an">{{ a.name }}</span>
                          <span v-if="a.timeOfDay || a.category" class="tod">{{ a.timeOfDay || a.category }}</span>
                        </h4>
                      </div>
                      <p class="asset-prompt">{{ a[sec.desc] || a.identityAnchor || a.spatialAnchor || a.prompt }}</p>
                      <div class="ab-acts">
                        <button class="regen-main" @click="regenAssetImage(sec.type, a.id)">
                          <svg viewBox="0 0 24 24" fill="none" width="11" height="11">
                            <path d="M4 12a8 8 0 0 1 13.7-5.6M20 12a8 8 0 0 1-13.7 5.6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
                            <path d="M18 3v4h-4M6 21v-4h4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
                          </svg>
                          重新生成
                        </button>
                        <button class="icon-btn" title="编辑" @click="openEdit(sec.type, a)">
                          <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
                            <path d="M4 20h4l10-10-4-4L4 16v4Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
                          </svg>
                        </button>
                        <button class="icon-btn danger" title="删除" @click="removeAsset(sec.type, a)">
                          <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
                            <path d="M4 7h16M9.5 7V4.5h5V7M6.5 7l1 13h9l1-13M10 11v5M14 11v5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
                          </svg>
                        </button>
                      </div>
                    </div>
                  </div>

                  <div class="scene-side">
                    <!-- 柱形全景图 -->
                    <div class="scene-panel">
                      <div class="panel-head">
                        <span class="panel-label">柱形全景图</span>
                        <button
                          class="icon-btn"
                          :disabled="panoramaBusy[a.id]"
                          :title="a.panoramaImage ? '重做柱形全景图（不覆盖场景图）' : '把场景图扩展成 360 度柱形全景图'"
                          @click="makePanorama(a)"
                        >
                          <span v-if="panoramaBusy[a.id]" class="spinner"></span>
                          <svg v-else viewBox="0 0 24 24" fill="none" width="12" height="12">
                            <path d="M3 12a9 9 0 0 1 18 0 9 9 0 0 1-18 0Z" stroke="currentColor" stroke-width="1.7" />
                            <path d="M3 12c3 3 15 3 18 0M12 3c-3 3-3 15 0 18" stroke="currentColor" stroke-width="1.5" />
                          </svg>
                        </button>
                      </div>
                      <div v-if="a.panoramaImage" class="panel-media">
                        <img :src="a.panoramaImage" alt="柱形全景图" @click="viewImage(a.panoramaImage)" />
                      </div>
                      <div v-else class="panel-empty">
                        <template v-if="panoramaBusy[a.id]"><span class="spinner"></span>生成中…</template>
                        <template v-else>点击右上角生成<br />柱形全景图</template>
                      </div>
                    </div>

                    <!-- 360 全景视频 -->
                    <div class="scene-panel">
                      <div class="panel-head">
                        <span class="panel-label">360 全景视频</span>
                        <button
                          class="icon-btn"
                          :disabled="panoramaVideoBusy[a.id] || !a.panoramaImage"
                          :title="a.panoramaVideo ? '重做 360 全景视频' : '把柱形全景图转成 360 全景视频'"
                          @click="makePanoramaVideo(a)"
                        >
                          <span v-if="panoramaVideoBusy[a.id]" class="spinner"></span>
                          <svg v-else viewBox="0 0 24 24" fill="none" width="12" height="12">
                            <path d="M4 12a8 8 0 0 1 13.7-5.6M20 12a8 8 0 0 1-13.7 5.6" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
                            <path d="M18 3v4h-4M6 21v-4h4" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
                          </svg>
                        </button>
                      </div>
                      <div v-if="a.panoramaVideo" class="panel-media video">
                        <video
                          :src="a.panoramaVideo"
                          :poster="a.panoramaImage || undefined"
                          muted
                          loop
                          playsinline
                          preload="metadata"
                          @mouseenter="playInline"
                          @mouseleave="pauseInline"
                        ></video>
                        <span class="pano-play" title="点击查看" @click="viewImage(a.panoramaVideo)">▶</span>
                      </div>
                      <div v-else class="panel-empty">
                        <template v-if="panoramaVideoBusy[a.id]"><span class="spinner"></span>生成中…</template>
                        <template v-else-if="!a.panoramaImage">需先生成柱形全景图</template>
                        <template v-else>点击右上角生成<br />360 全景视频</template>
                      </div>
                    </div>
                  </div>
                </template>

                <!-- 角色 / 道具：保持原有卡片结构 -->
                <template v-else>
                  <div class="asset-thumb" :class="sec.thumbCls">
                    <img
                      v-if="a.referenceImage"
                      :src="a.referenceImage"
                      :alt="a.name"
                      @click="viewImage(a.referenceImage)"
                    />
                    <template v-else>{{ a.name?.[0] }}</template>
                    <span v-if="sec.type === 'character' && genOpts.showName" class="thumb-name">{{ a.name }}</span>
                    <div v-if="isTaskRunning(taskOf(a.id))" class="thumb-mask">
                      <span class="spinner"></span>
                    </div>
                  </div>
                  <div class="asset-body">
                    <div class="ab-head">
                      <h4>
                        <span class="an">{{ a.name }}</span>
                        <span v-if="a.timeOfDay || a.category" class="tod">{{ a.timeOfDay || a.category }}</span>
                      </h4>
                    </div>
                    <p class="asset-prompt">{{ a[sec.desc] || a.identityAnchor || a.spatialAnchor || a.prompt }}</p>
                    <div class="ab-acts">

                      <button class="regen-main" @click="regenAssetImage(sec.type, a.id)">
                        <svg viewBox="0 0 24 24" fill="none" width="11" height="11">
                          <path d="M4 12a8 8 0 0 1 13.7-5.6M20 12a8 8 0 0 1-13.7 5.6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
                          <path d="M18 3v4h-4M6 21v-4h4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
                        </svg>
                        重新生成
                      </button>
                      <button class="icon-btn" title="编辑" @click="openEdit(sec.type, a)">
                        <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
                          <path d="M4 20h4l10-10-4-4L4 16v4Z" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
                        </svg>
                      </button>
                      <button class="icon-btn danger" title="删除" @click="removeAsset(sec.type, a)">
                        <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
                          <path d="M4 7h16M9.5 7V4.5h5V7M6.5 7l1 13h9l1-13M10 11v5M14 11v5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
                        </svg>
                      </button>
                    </div>
                  </div>
                </template>
              </div>
            </div>
            <div v-else class="asset-empty">{{ sec.empty }}</div>
          </div>
          </div>
        </div>

        <div class="panel-foot">
          <span class="foot-hint">
            {{ missingImages > 0 ? `还有 ${missingImages} 个资产没出图` : '资产已齐，可以生成分镜' }}
          </span>
          <div class="foot-actions">
            <button class="btn btn-ghost" @click="step = 0">上一步</button>
            <button class="btn btn-ghost" :disabled="identifying" @click="identify">重新识别</button>
          </div>
        </div>
      </div>

      <!-- ===== 步骤 2：分镜工作台 ===== -->
      <div v-else-if="step === 2" class="sb-wrap rise">
        <!-- 工具栏 -->
        <div class="sb-bar">
          <div class="sb-left">
            <PillSelect v-model="sbDuration" label="时长" :options="DURATION_OPTIONS" />
            <PillSelect v-model="sbRatio" label="画幅" :options="RATIO_OPTIONS" />
            <!-- 分镜生成后这里锁死：描述词的详略是按生成时那个选择写的，
                 中途换素材会让提示词和实际喂给模型的素材对不上。
                 要改就回资产页改完重新生成分镜。 -->
            <PillSelect
              v-model="sbSceneRef"
              label="场景参考"
              :options="SCENE_REF_OPTIONS"
              :width="132"
              :disabled="frames.length > 0"
            />
            <PillSelect
              v-if="videoModelOptions.length"
              v-model="videoModel"
              label="视频模型"
              :options="videoModelOptions"
              :width="200"
            />
          </div>
          <div class="sb-right">
            <button class="sb-btn" :disabled="!frames.length" @click="shotSheetOpen = true">分镜表</button>
            <!-- 首帧图功能已下线：视频参考改用「场景图 + 角色 + 道具」，
                 首帧图会锁死构图，反而让每段画面都从同一张图起幅。
                 后端能力保留，把 ffEnabled 改回 true 即可恢复。 -->
            <template v-if="ffEnabled">
              <button class="sb-btn" :disabled="ffPlanning || !frames.length" @click="planFirstFrames">
                {{ ffPlanning ? '规划中…' : '首帧规划' }}
              </button>
              <button
                class="sb-btn"
                :disabled="!ffGroups.length || ffGenBusy"
                title="查看已生成的首帧参考图"
                @click="openFirstFramePanel"
              >
                <span v-if="ffGenBusy" class="spinner"></span>
                {{ ffGenBusy ? '首帧生成中…' : `首帧图（${ffGroups.filter((g: any) => g.image).length}/${ffGroups.length}）` }}
              </button>
            </template>
            <button class="sb-btn" :disabled="generating" @click="generateStoryboard">
              {{ generating ? (genProgress.total ? `生成中 ${genProgress.done}/${genProgress.total}` : '生成中…') : '重新生成' }}
            </button>
            <button
              class="sb-btn primary"
              :disabled="generatingVideo || !pendingVideoCount"
              @click="generateVideos"
            >
              <span v-if="generatingVideo" class="spinner"></span>
              {{ generatingVideo ? '提交中…' : `批量视频（${pendingVideoCount}）` }}
            </button>
          </div>
        </div>

        <div class="workbench">
          <!-- 左：镜头列表 -->
          <div class="wb-col frames-col">
            <div class="wb-head">
              <span>分镜</span>
              <span class="wb-count">{{ frames.length }}</span>
            </div>
            <div class="frames-scroll">
              <div
                v-for="(f, i) in frames"
                :key="f.id"
                class="frame-mini"
                :class="{ on: i === activeFrame }"
                @click="activeFrame = i"
              >
                <span class="fm-idx">{{ String(f.frameIndex || i + 1).padStart(2, '0') }}</span>
                <div class="fm-body">
                  <p>{{ (f.text || '').split('\n').filter((x: string) => x.trim()).slice(0, 2).join(' ').slice(0, 46) }}</p>
                  <div class="fm-tags">
                    <span class="mini-tag dur">{{ Math.round(Number(f.duration) || 0) }}s</span>
                    <span class="mini-tag">{{ parseFrameShots(f.videoPrompt || '').length }} 镜</span>
                    <span v-if="f.videoUrl" class="mini-tag ok">已出片</span>
                    <span v-else-if="isTaskRunning(taskOf(f.id))" class="mini-tag busy">生成中</span>
                  </div>
                </div>
                <img
                  v-if="ffGroupOfFrame(f.frameIndex || i + 1)?.image"
                  class="fm-thumb"
                  :src="ffGroupOfFrame(f.frameIndex || i + 1)?.image || ''"
                  alt=""
                />
              </div>
            </div>
          </div>

          <!-- 中：当前分镜 -->
          <div class="wb-col main-col">
            <template v-if="activeFrameData">
              <div class="fd-head">
                <div class="fd-title">
                  第 {{ activeFrameData.frameIndex }} 段
                  <span class="fd-meta">{{ Math.round(Number(activeFrameData.duration) || 0) }} 秒 · {{ frameShots.length }} 个镜头</span>
                </div>
                <div class="fd-acts">
                  <button class="btn btn-ghost btn-sm" @click="regenerateFrame">重生成</button>
                  <button class="btn btn-ghost btn-sm" :disabled="isTaskRunning(taskOf(activeFrameData.id))" @click="saveFrame">保存</button>
                  <button
                    class="btn btn-primary btn-sm"
                    :disabled="isTaskRunning(taskOf(activeFrameData.id))"
                    @click="generateOneVideo(activeFrameData)"
                  >
                    {{ activeFrameData.videoUrl ? '重出本段' : '出本段视频' }}
                  </button>
                </div>
              </div>

              <div class="main-inner" :class="{ 'with-media': hasMedia, 'with-shots': frameShots.length > 0 }">
                <!-- 左：画面（视频 / 生成中 / 场景全景参考） -->
                <div v-if="hasMedia" class="fd-media">
                  <div v-if="activeFrameData.videoUrl" class="fd-video">
                    <video :src="activeFrameData.videoUrl" controls playsinline></video>
                  </div>
                  <div v-else-if="isTaskRunning(taskOf(activeFrameData.id))" class="fd-busy">
                    <span class="spinner"></span>
                    <span>生成中 {{ taskOf(activeFrameData.id)?.progress || 0 }}%</span>
                  </div>

                  <!-- 首帧参考图已下线（ffEnabled=false），这块不再显示；
                       代码保留，和「首帧规划/首帧图」按钮用同一个开关 -->
                  <div v-if="ffEnabled && (activeGroup?.image || ffRefreshing)" class="fd-ff">
                    <div class="fd-ff-head">
                      <span>场景全景参考</span>
                      <span class="fd-ff-note">{{ ffRefreshing ? '正在生成…' : coverText(activeGroup) }}</span>
                      <button
                        v-if="activeGroup?.image"
                        class="fd-ff-zoom"
                        title="放大查看"
                        @click="viewImage(activeGroup.image)"
                      >
                        <svg viewBox="0 0 24 24" fill="none" width="13" height="13">
                          <path d="M9 4H4v5M15 4h5v5M15 20h5v-5M9 20H4v-5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
                        </svg>
                      </button>
                    </div>
                    <div class="fd-ff-body" :class="{ busy: ffRefreshing }">
                      <img
                        v-if="activeGroup?.image"
                        :src="activeGroup.image"
                        alt=""
                        class="zoomable"
                        @click="viewImage(activeGroup.image)"
                      />
                      <div v-else class="fd-ff-empty">首次生成中，稍等…</div>
                      <div v-if="ffRefreshing" class="fd-ff-mask">
                        <span class="spinner"></span>
                        <span>正在生成新的参考图</span>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- 右：文字（文案 / 描述词 / 镜头拆解） -->
                <div class="fd-text">
                  <div class="prompt-block">
                    <div class="pb-head"><span>文案</span><span class="pb-badge">text</span></div>
                    <textarea v-model="activeFrameData.text" class="fd-textarea short"></textarea>
                  </div>

                  <div class="prompt-block">
                    <div class="pb-head">
                      <span>视频描述词</span>
                      <span class="pb-badge">video_prompt</span>
                    </div>
                    <textarea v-model="activeFrameData.videoPrompt" class="fd-textarea"></textarea>
                  </div>

                </div>

                <!-- 第三栏：镜头拆解（只读，和文案分开看更清楚） -->
                <div v-if="frameShots.length" class="fd-shots">
                  <div class="fd-shots-head">镜头拆解</div>
                  <div v-for="sh in frameShots" :key="sh.no" class="fd-shot">
                    <div class="fd-shot-top">
                      <span class="fd-shot-no">{{ sh.no }}</span>
                      <span class="fd-shot-time">{{ sh.start }}–{{ sh.end }}s</span>
                      <span class="fd-shot-size">{{ sh.size }}</span>
                      <span v-if="sh.camera" class="fd-shot-cam">{{ sh.camera }}</span>
                      <span v-if="sh.rate > 5" class="fd-shot-warn">{{ sh.rate.toFixed(1) }} 字/秒 偏快</span>
                    </div>
                    <p v-if="sh.line" class="fd-shot-line">“{{ sh.line }}”</p>
                    <p v-if="sh.action" class="fd-shot-act">{{ sh.action }}</p>
                  </div>
                </div>
              </div>
            </template>
            <div v-else class="wb-empty">还没有分镜</div>
          </div>

          <!-- 右：资产速查 -->
          <div class="wb-col side-col">
            <div class="wb-head"><span>资产</span></div>
            <div class="side-scroll">
              <div class="side-group">
                <div class="sg-title"><span class="dot role"></span>角色</div>
                <div v-for="c in assets.characters" :key="c.id" class="side-item">
                  <img v-if="c.referenceImage" class="si-thumb" :src="c.referenceImage" alt="" />
                  <div class="si-body">
                    <span class="si-name">@{{ c.name }}</span>
                    <span class="si-desc">{{ c.identityAnchor }}</span>
                  </div>
                </div>
                <div v-if="!assets.characters.length" class="side-empty">无</div>
              </div>
              <div class="side-group">
                <div class="sg-title"><span class="dot scene"></span>场景</div>
                <!-- 场景素材跟着「场景参考」走：选了全景/360 就显示对应素材，
                     否则用户看到的图和实际喂给模型的不是同一个东西 -->
                <div v-for="s in assets.scenes" :key="s.id" class="side-item">
                  <video
                    v-if="sbSceneRef === 'video' && s.panoramaVideo"
                    class="si-thumb si-video"
                    :src="s.panoramaVideo"
                    muted
                    playsinline
                    preload="metadata"
                    title="360 全景视频（悬停播放）"
                    @mouseenter="(e: any) => (e.target as HTMLVideoElement).play()"
                    @mouseleave="(e: any) => (e.target as HTMLVideoElement).pause()"
                  ></video>
                  <img
                    v-else-if="sbSceneRef === 'panorama' && s.panoramaImage"
                    class="si-thumb"
                    :src="s.panoramaImage"
                    alt=""
                    title="柱形全景图"
                  />
                  <img v-else-if="s.referenceImage" class="si-thumb" :src="s.referenceImage" alt="" />
                  <div class="si-body">
                    <span class="si-name">@{{ s.name }}</span>
                    <span class="si-desc">
                      <span v-if="sbSceneRef === 'video' && s.panoramaVideo" class="si-badge">360 视频</span>
                      <span v-else-if="sbSceneRef === 'panorama' && s.panoramaImage" class="si-badge">全景图</span>
                      {{ s.spatialAnchor }}
                    </span>
                  </div>
                </div>
                <div v-if="!assets.scenes.length" class="side-empty">无</div>
              </div>
              <div class="side-group">
                <div class="sg-title"><span class="dot item"></span>道具</div>
                <div v-for="it in assets.items" :key="it.id" class="side-item">
                  <img v-if="it.referenceImage" class="si-thumb" :src="it.referenceImage" alt="" />
                  <div class="si-body">
                    <span class="si-name">@{{ it.name }}</span>
                    <span class="si-desc">{{ it.prompt }}</span>
                  </div>
                </div>
                <div v-if="!assets.items.length" class="side-empty">无</div>
              </div>
            </div>
          </div>

          <!-- 底部 -->
          <div class="wb-foot">
            <div class="wf-left">
              <span class="wf-stat">{{ frames.length }} 段 · 约 {{ totalDuration }} 秒 · 已出片 {{ doneVideoCount }}</span>
            </div>
            <div class="wf-right">
              <button class="btn btn-ghost btn-sm" @click="step = 1">上一步</button>
              <button class="btn btn-primary btn-sm" @click="step = 3">下一步：成片</button>
            </div>
          </div>
        </div>
      </div>

      <!-- ===== 步骤 3：成片 ===== -->
      <div v-else class="panel rise">
        <div class="panel-head">
          <div class="ph-row">
            <div>
              <h2>生成成片</h2>
              <p>{{ frames.length }} 段，共 {{ totalDuration }} 秒 · 每段按自己的时长单独生成</p>
            </div>
            <div class="ph-actions">
              <button class="btn btn-ghost btn-sm" :disabled="composing || !doneVideoCount" @click="composeEpisode">
                <span v-if="composing" class="spinner"></span>
                {{ composing ? '合成中…' : '合成整集' }}
              </button>
              <button class="btn btn-primary btn-sm" :disabled="generatingVideo || !pendingVideoCount" @click="generateVideos">
                <span v-if="generatingVideo" class="spinner"></span>
                {{ generatingVideo ? '提交中…' : pendingVideoCount ? `生成剩余 ${pendingVideoCount} 段` : '已全部出片' }}
              </button>
            </div>
          </div>
        </div>

        <div class="video-body">
          <div class="vsummary">
            <div class="vs-item">
              <span class="vs-label">段数</span>
              <span class="vs-value">{{ frames.length }}</span>
            </div>
            <div class="vs-item">
              <span class="vs-label">总时长</span>
              <span class="vs-value">{{ totalDuration }}s</span>
            </div>
            <div class="vs-item">
              <span class="vs-label">已出片</span>
              <span class="vs-value done">{{ doneVideoCount }} / {{ frames.length }}</span>
            </div>
            <div class="vs-item">
              <span class="vs-label">画幅</span>
              <span class="vs-value">{{ sbRatio }}</span>
            </div>
          </div>

          <!-- 整集合成结果：按分镜顺序拼成的一条完整短剧 -->
          <div v-if="composed" class="composed">
            <div class="composed-head">
              <span class="composed-badge">成片</span>
              <span class="composed-meta">{{ composed.segments }} 段 · {{ Math.round(Number(composed.duration) || 0) }} 秒</span>
              <span v-if="composed.skipped && composed.skipped.length" class="composed-warn">
                跳过第 {{ composed.skipped.join('、') }} 段（未出片）
              </span>
              <a class="composed-dl" :href="composed.url" download>下载</a>
            </div>
            <video :src="composed.url" controls playsinline preload="metadata"></video>
          </div>

          <div class="vlist">
            <div v-for="(f, i) in frames" :key="f.id" class="vitem" :class="{ empty: !f.videoUrl }">
              <div class="vitem-head">
                <span class="vitem-no">第 {{ f.frameIndex }} 段</span>
                <span class="vitem-dur">{{ Math.round(Number(f.duration) || 0) }}s</span>
                <button class="vitem-go" @click="activeFrame = i; step = 2">编辑</button>
              </div>
              <video v-if="f.videoUrl" :src="f.videoUrl" controls playsinline preload="metadata"></video>
              <div v-else class="vitem-empty">
                <span v-if="isTaskRunning(taskOf(f.id))">生成中 {{ taskOf(f.id)?.progress || 0 }}%</span>
                <button v-else class="btn btn-ghost btn-sm" @click="generateOneVideo(f)">出这一段</button>
              </div>
            </div>
          </div>
        </div>

        <div class="panel-foot">
          <span class="foot-hint">
            {{ generatingVideo ? '正在提交，可在此页等待' : pendingVideoCount === 0 ? '全部分镜已出片' : '生成后可单段重抽，无需整片重跑' }}
          </span>
          <div class="foot-actions">
            <button class="btn btn-ghost" @click="step = 2">上一步</button>
          </div>
        </div>
      </div>
    </template>

    <!-- ===== 编辑资产弹窗（角色 / 场景 / 道具） ===== -->
    <Teleport to="body">
      <div v-if="editOpen" class="mask" @click.self="editOpen = false">
        <div class="modal">
          <header class="modal-head">
            <div>
              <h3>编辑{{ editType === 'character' ? '角色' : editType === 'scene' ? '场景' : '道具' }}</h3>
              <span class="modal-sub">改名称与出图提示词后点保存，再重新生成才会生效</span>
            </div>
            <button class="modal-x" @click="editOpen = false">×</button>
          </header>

          <div class="modal-body">
            <div class="ed-row">
              <div class="ed-ref">
                <div class="ed-ref-thumb">
                  <img v-if="editForm.referenceImage" :src="editForm.referenceImage" alt="" />
                  <span v-else>暂无参考图</span>
                </div>
                <label class="ed-upload">
                  <input type="file" accept="image/*" hidden @change="uploadRef" />
                  <span v-if="uploading">上传中…</span>
                  <span v-else>{{ editForm.referenceImage ? '更换参考图' : '上传参考图' }}</span>
                </label>
              </div>

              <div class="ed-fields">
                <label class="ed-field">
                  <span>名称</span>
                  <input v-model="editForm.name" type="text" placeholder="角色 / 场景 / 道具名" />
                </label>
                <label v-if="editType === 'character'" class="ed-field">
                  <span>身份锚定<em class="ed-opt">视频描述词的人物行读这里</em></span>
                  <input
                    v-model="editForm.identityAnchor"
                    type="text"
                    placeholder="如：男，四十二岁，公立中学资深教师，教龄近二十年"
                  />
                </label>
                <div v-if="editType === 'character'" class="ed-field">
                  <span>音色<em class="ed-opt">二选一：文字描述 或 参考音频</em></span>
                  <div class="seg">
                    <button
                      type="button"
                      class="seg-btn"
                      :class="{ on: voiceMode === 'text' }"
                      @click="setVoiceMode('text')"
                    >文字描述</button>
                    <button
                      type="button"
                      class="seg-btn"
                      :class="{ on: voiceMode === 'audio' }"
                      @click="setVoiceMode('audio')"
                    >参考音频</button>
                  </div>

                  <input
                    v-if="voiceMode === 'text'"
                    v-model="editForm.voiceTone"
                    type="text"
                    class="voice-input"
                    placeholder="如：沉稳中年女音，中低音域，语速中等偏慢"
                  />

                  <div v-else class="voice-row">
                    <input
                      id="voice-file-input"
                      type="file"
                      accept="audio/*"
                      style="display: none"
                      @change="uploadVoice"
                    />
                    <button
                      type="button"
                      class="btn btn-ghost btn-sm"
                      :disabled="uploading"
                      @click="pickVoiceFile"
                    >
                      {{ uploading ? '上传中…' : editForm.voiceSample ? '更换音频' : '选择音频' }}
                    </button>
                    <audio v-if="editForm.voiceSample" :src="editForm.voiceSample" controls class="voice-audio"></audio>
                    <span v-else class="ed-hint">未上传 · 最长 15 秒</span>
                    <button
                      v-if="editForm.voiceSample"
                      type="button"
                      class="icon-btn danger"
                      title="移除音色样本"
                      @click="editForm.voiceSample = ''"
                    >
                      <svg viewBox="0 0 24 24" fill="none" width="12" height="12">
                        <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
                      </svg>
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <label class="ed-field block">
              <span>出图提示词</span>
              <textarea v-model="editForm.imagePrompt" rows="8" class="ed-textarea"></textarea>
            </label>
          </div>

          <footer class="modal-foot">
            <span class="modal-tip">提示词只影响这张参考图，不改变分镜描述词</span>
            <button class="btn btn-ghost btn-sm" @click="editOpen = false">取消</button>
            <button class="btn btn-primary btn-sm" :disabled="editSaving" @click="saveAsset">
              {{ editSaving ? '保存中…' : '保存' }}
            </button>
          </footer>
        </div>
      </div>
    </Teleport>

    <!-- ===== 资产大图查看 ===== -->
    <Teleport to="body">
      <div v-if="viewerSrc" class="viewer-mask" @click.self="viewerSrc = ''">
        <button class="viewer-x" @click="viewerSrc = ''">×</button>
        <img :src="viewerSrc" alt="" />
      </div>
    </Teleport>

    <!-- ===== 首帧规划弹窗 ===== -->
    <Teleport to="body">
      <div v-if="ffPlanOpen" class="mask" @click.self="ffPlanOpen = false">
        <div class="modal">
          <header class="modal-head">
            <div>
              <h3>首帧规划</h3>
              <span class="modal-sub">
                <template v-if="ffGenBusy">
                  <span class="ff-busy-dot"></span>正在生成 {{ ffBusyIds.length }} 张，通常 30–60 秒，生成完自动更新
                </template>
                <template v-else>
                  共 {{ ffGroups.length }} 组 · 同场景共用一张全景参考图，镜头由视频模型自己切
                </template>
              </span>
            </div>
            <button class="modal-x" @click="ffPlanOpen = false">×</button>
          </header>

          <div class="modal-body">
            <div v-for="(g, i) in ffGroups" :key="g.id" class="ffg">
              <div class="ffg-thumb" :class="{ busy: isFfBusy(g) }">
                <img v-if="g.image" :src="g.image" alt="" class="zoomable" @click="viewImage(g.image)" />
                <span v-else>未生成</span>
                <div v-if="isFfBusy(g)" class="ffg-mask">
                  <span class="spinner"></span>
                  <span class="ffg-mask-t">生成中</span>
                </div>
                <!-- 失败要有明确落点：不然用户只看到「未生成」，不知道是没点还是跑挂了 -->
                <div v-else-if="g.taskStatus === 'failed' && !g.image" class="ffg-mask fail" :title="g.taskError">
                  <span class="ffg-mask-t">生成失败</span>
                  <span class="ffg-mask-e">{{ g.taskError || '上游报错' }}</span>
                </div>
              </div>
              <div class="ffg-body">
                <div class="ffg-title">
                  第 {{ i + 1 }} 组 · {{ g.sceneName || '未命名场景' }}
                  <span class="ffg-count">{{ parseIdx(g.frameIndexes).length }} 段</span>
                </div>
                <textarea v-model="g.description" v-autosize class="ffg-desc" rows="3"></textarea>
                <div class="ffg-foot">
                  <span class="ffg-frames">覆盖：{{ parseIdx(g.frameIndexes).join('、') }}</span>
                  <span class="ffg-actions">
                    <button class="link-btn" @click="saveGroupDesc(g)">保存描述</button>
                    <button
                      class="link-btn accent"
                      :disabled="ffBusyIds.includes(g.id)"
                      @click="genOneFirstFrame(g)"
                    >
                      {{ ffBusyIds.includes(g.id) ? '生成中…' : g.image ? '重出这张' : '单独生成' }}
                    </button>
                  </span>
                </div>
              </div>
            </div>
            <div v-if="!ffGroups.length" class="modal-empty">还没有规划结果</div>
          </div>

          <footer class="modal-foot">
            <span class="modal-tip">描述里只写空间与人物，不写景别和运镜</span>
            <button class="btn btn-ghost btn-sm" :disabled="ffGenBusy" @click="planFirstFrames">重新规划</button>
            <button class="btn btn-primary btn-sm" :disabled="ffGenBusy || !ffGroups.length" @click="genFirstFrameGroups">
              <span v-if="ffGenBusy" class="spinner"></span>
              {{ ffGenBusy ? '生成中…' : `生成 ${ffGroups.length} 张` }}
            </button>
          </footer>
        </div>
      </div>
    </Teleport>

    <!-- ===== 分镜表弹窗 ===== -->
    <Teleport to="body">
      <div v-if="shotSheetOpen" class="mask" @click.self="shotSheetOpen = false">
        <div class="modal wide">
          <header class="modal-head">
            <div>
              <h3>分镜表</h3>
              <span class="modal-sub">
                共 {{ shotSheetRows.length }} 个镜头 ·
                <span :class="{ warn: slowRows > 0 }">{{ slowRows }} 处语速偏快</span>
                （超过 5 字/秒容易被截断）
              </span>
            </div>
            <button class="modal-x" @click="shotSheetOpen = false">×</button>
          </header>

          <div class="modal-body">
            <table class="sheet">
              <thead>
                <tr>
                  <th>段</th>
                  <th>镜</th>
                  <th>时间轴</th>
                  <th>景别</th>
                  <th>台词</th>
                  <th>字数</th>
                  <th>语速</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(r, i) in shotSheetRows" :key="i" @click="gotoFrame(r.frame)">
                  <td class="dim">{{ r.frame }}</td>
                  <td class="dim">{{ r.no }}</td>
                  <td class="mono">{{ r.dur ? `${r.start}–${r.end}s` : '—' }}</td>
                  <td>{{ r.size || '—' }}</td>
                  <td class="line">{{ r.line || r.action || '—' }}</td>
                  <td class="dim">{{ r.chars || '—' }}</td>
                  <td :class="r.rate > 5 ? 'bad' : r.rate > 0 ? 'ok' : 'dim'">
                    {{ r.rate ? r.rate.toFixed(1) : '—' }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <footer class="modal-foot">
            <span class="modal-tip">点任意一行跳到对应分镜</span>
            <button class="btn btn-ghost btn-sm" @click="shotSheetOpen = false">关闭</button>
          </footer>
        </div>
      </div>
    </Teleport>

    <!-- 大纲确认：先看拆分方案，确认后再逐段展开 -->
    <div v-if="outlineOpen" class="ol-mask" @click.self="outlineOpen = false">
      <div class="ol-panel">
        <div class="ol-head">
          <div>
            <h3>分镜大纲</h3>
            <p>确认拆分方案后再生成，避免整集跑完才发现拆错</p>
          </div>
          <span class="ol-count">{{ outlineList.length }} 个叙事单元</span>
        </div>

        <div class="ol-body">
          <div v-for="it in outlineList" :key="it.index" class="ol-item">
            <span class="ol-idx">{{ it.index }}</span>
            <div class="ol-main">
              <div class="ol-meta">
                <span class="ol-scene">{{ it.scene }}</span>
                <span v-for="r in it.roles || []" :key="r" class="ol-role">@{{ r }}</span>
                <span v-for="x in it.items || []" :key="x" class="ol-item-tag">{{ x }}</span>
              </div>
              <p class="ol-beat">{{ it.beat }}</p>
              <p v-if="it.lines && it.lines.length" class="ol-lines">
                <span v-for="(l, i) in it.lines" :key="i">「{{ l }}」</span>
              </p>
            </div>
          </div>
        </div>

        <div class="ol-foot">
          <button class="btn btn-ghost" @click="outlineOpen = false">取消</button>
          <button class="btn btn-ghost" :disabled="outlineLoading" @click="generateStoryboard">
            {{ outlineLoading ? '重新拆分中…' : '重新拆分' }}
          </button>
          <button class="btn btn-primary" @click="runStoryboardStream">确认，开始生成</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.studio { padding-top: 24px; }

.loading { padding: 80px; text-align: center; color: var(--text-mute); font-size: 14px; }

/* 顶部：返回胶囊 + 面包屑 */
.top-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}
.back {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 30px;
  padding: 0 12px 0 8px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--surface);
  font-size: 12.5px;
  color: var(--text-dim);
  transition: all 0.2s var(--ease);
}
.back:hover { color: var(--text); border-color: var(--border-strong); background: var(--surface-hover); }
.tb-sep { width: 1px; height: 16px; background: var(--border); }
.crumbs { display: flex; align-items: center; gap: 7px; font-size: 12.5px; color: var(--text-mute); }
.crumb {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  transition: color 0.2s var(--ease);
}
.crumb:hover { color: var(--text); }
.crumb.on { color: var(--text); font-weight: 550; cursor: default; }
.crumb-sep { opacity: 0.5; }
.crumb-sel {
  height: 24px;
  padding: 0 6px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  font-size: 12.5px;
  font-weight: 550;
}

/* 步骤条 */
.stepper {
  display: flex;
  align-items: center;
  margin-bottom: 20px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--border);
}
.step {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  cursor: pointer;
  opacity: 0.55;
  transition: opacity 0.28s var(--ease);
}
.step.on, .step.done { opacity: 1; }
.step.locked { cursor: default; opacity: 0.38; }
.step-dot {
  width: 26px;
  height: 26px;
  flex-shrink: 0;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 11.5px;
  font-weight: 640;
  color: var(--text-mute);
  border: 1.5px solid var(--border-strong);
  transition: all 0.3s var(--ease);
}
.step.on .step-dot {
  color: #fff;
  background: var(--grad);
  border-color: transparent;
  box-shadow: 0 0 0 4px rgba(124, 92, 255, 0.14);
}
.step.done .step-dot {
  color: var(--accent);
  border-color: rgba(124, 92, 255, 0.4);
  background: var(--accent-soft);
}
.step-text { display: flex; flex-direction: column; }
.step-label { font-size: 13.5px; font-weight: 570; }
.step-desc { font-size: 11.5px; color: var(--text-mute); }

/* 面板 */
.panel {
  border-radius: var(--r-xl);
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
}
.panel-head { padding: 20px 24px 16px; border-bottom: 1px solid var(--border); }
.panel-head h2 { font-size: 16.5px; font-weight: 640; letter-spacing: -0.02em; }
.panel-head p { font-size: 12.5px; color: var(--text-mute); margin-top: 3px; }
.ph-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; }
.ph-row .btn { flex-shrink: 0; margin-top: 2px; }

/* 按钮 */
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 15px;
  border-radius: 9px;
  font-size: 13px;
  font-weight: 550;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  transition: all 0.22s var(--ease);
}
.btn:hover:not(:disabled) { border-color: var(--border-strong); background: var(--surface-hover); }
.btn:disabled { opacity: 0.45; cursor: not-allowed; }
.btn-primary {
  color: #fff;
  border-color: transparent;
  background: var(--grad);
  box-shadow: 0 8px 20px -10px rgba(124, 92, 255, 0.9);
}
.btn-primary:hover:not(:disabled) { filter: brightness(1.08); }
.btn-ghost { background: transparent; }
.btn-sm { padding: 6px 12px; font-size: 12.5px; }

.spinner {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.35);
  border-top-color: #fff;
  animation: spin 0.7s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

/* 编辑器 */
.editor {
  margin: 20px 24px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  overflow: hidden;
  background: var(--bg-soft);
}
.editor-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 13px;
  border-bottom: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.02);
}
.dots { display: flex; gap: 5px; }
.dots i { width: 8px; height: 8px; border-radius: 50%; background: rgba(255, 255, 255, 0.13); }
.editor-file { font-size: 11.5px; color: var(--text-mute); font-family: 'SF Mono', Menlo, monospace; }
.editor-count { margin-left: auto; font-size: 11px; color: var(--text-mute); }
.editor-body {
  width: 100%;
  min-height: 330px;
  padding: 16px;
  font-size: 13.5px;
  line-height: 1.85;
  font-family: 'SF Mono', Menlo, 'PingFang SC', monospace;
  color: var(--text);
  text-align: left;
  resize: vertical;
}

.panel-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 24px;
  border-top: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.015);
}
.foot-hint { font-size: 12px; color: var(--text-mute); }
.foot-actions { display: flex; gap: 9px; }

/* 资产 */
/* 出图工具栏：一条横条，右侧放「参考图已齐 / 生成分镜」 */
/* 生成配置工具栏：左边规格、右边动作 */
.gen-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.012);
}
.gt-left { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; }
/* 工具栏里的下拉整体收一档：1093 宽下要和右侧两个按钮挤在同一行 */
.gen-toolbar :deep(.pill-group) { gap: 6px; }
.gen-toolbar :deep(.pill-label) { font-size: 12px; }
.gen-toolbar :deep(.pill-btn) { height: 30px; padding: 0 8px 0 10px; font-size: 12px; }
.gt-right { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.gt-right .btn { padding: 6px 10px; }
.gt-sep { width: 1px; height: 18px; background: var(--border); margin: 0; }

.asset-wrap { padding: 16px 24px 20px; }
/* 三段并排：每段一个框，框内卡片网格（对齐参考站：角色 | 场景 | 道具） */
.asset-cols {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  align-items: start;
}
.asset-section {
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.012);
  padding: 12px;
  min-height: 120px;
}
.asset-title {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  font-weight: 600;
  margin-bottom: 10px;
}
.dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
.dot.role { background: #7c5cff; }
.dot.scene { background: #22d3ee; }
.dot.item { background: #fbbf24; }
.count {
  display: inline-grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  padding: 0 6px;
  border-radius: 99px;
  font-size: 10.5px;
  color: var(--text-mute);
  background: var(--surface);
  border: 1px solid var(--border);
}
.sec-ratio {
  margin-left: auto;
  font-size: 10px;
  font-family: 'SF Mono', Menlo, monospace;
  color: var(--text-mute);
  padding: 1px 6px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.05);
}
.asset-grid {
  display: grid;
  /* 130 起：1093 下每段 308px 正好两列（137），1440 下也是两列（195），
     道具 4 张始终排 2×2，不会出现 3+1 的断行 */
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 10px;
}

.asset-card {
  display: flex;
  flex-direction: column;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
  overflow: hidden;
  transition: all 0.26s var(--ease);
}
.asset-card:hover { border-color: var(--border-strong); background: var(--surface-hover); transform: translateY(-2px); }

.asset-thumb {
  position: relative;
  position: relative;
  width: 100%;
  aspect-ratio: 4 / 3;
  display: grid;
  place-items: center;
  font-size: 38px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.9);
  overflow: hidden;
}
.asset-thumb.person { background: linear-gradient(150deg, #7c5cff, #4c2fd6); }
.asset-thumb.scene { background: linear-gradient(150deg, #22d3ee, #0e7490); }
.asset-thumb.item { background: linear-gradient(150deg, #fbbf24, #d97706); aspect-ratio: 1 / 1; }
.asset-thumb img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  cursor: zoom-in;
}
/* 角色名：只贴在角色参考图左上角，方便一眼对号 */
.thumb-name {
  position: absolute;
  top: 6px;
  left: 6px;
  z-index: 2;
  font-size: 10.5px;
  font-weight: 600;
  color: #fff;
  padding: 2px 7px;
  border-radius: 5px;
  background: rgba(8, 8, 14, 0.62);
  backdrop-filter: blur(4px);
}
.thumb-mask {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: rgba(8, 8, 14, 0.7);
  backdrop-filter: blur(3px);
}

.asset-body { padding: 8px 9px 9px; min-width: 0; flex: 1; display: flex; flex-direction: column; }
.ab-head { display: flex; align-items: center; gap: 6px; margin-bottom: 3px; }
.ab-head h4 {
  flex: 1;
  min-width: 0;
  font-size: 12px;
  font-weight: 590;
  display: flex;
  align-items: center;
  gap: 5px;
}
.an {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tod {
  font-size: 9.5px;
  font-weight: 450;
  color: var(--text-mute);
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.06);
  flex-shrink: 0;
}
.asset-prompt {
  font-size: 10.5px;
  color: var(--text-mute);
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  flex: 1;
}
.ab-acts { display: flex; align-items: center; gap: 4px; margin-top: 8px; }
.regen-main {
  flex: 1;
  min-width: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  height: 24px;
  border-radius: 6px;
  font-size: 10px;
  white-space: nowrap;
  color: var(--text-dim);
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid transparent;
  transition: all 0.2s var(--ease);
}
.regen-main:hover { color: var(--text); border-color: var(--border-strong); background: rgba(255, 255, 255, 0.09); }
.icon-btn {
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border-radius: 6px;
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid transparent;
  transition: all 0.2s var(--ease);
}
.icon-btn:hover { color: var(--text); border-color: var(--border-strong); background: rgba(255, 255, 255, 0.09); }
.icon-btn.danger:hover { color: var(--err); border-color: rgba(248, 113, 113, 0.4); background: rgba(248, 113, 113, 0.1); }

.asset-empty {
  font-size: 12px;
  color: var(--text-mute);
  padding: 14px;
  border-radius: var(--r-md);
  border: 1px dashed var(--border);
  text-align: center;
}

/* 编辑资产弹窗 */
.ed-row { display: flex; gap: 16px; padding: 12px 0; }
.ed-ref { width: 128px; flex-shrink: 0; }
.ed-ref-thumb {
  width: 128px;
  height: 170px;
  border-radius: 8px;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.04);
  display: grid;
  place-items: center;
  font-size: 10.5px;
  color: var(--text-mute);
}
.ed-ref-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ed-upload {
  display: block;
  margin-top: 8px;
  height: 28px;
  line-height: 26px;
  text-align: center;
  border-radius: 7px;
  border: 1px dashed var(--border-strong);
  font-size: 11px;
  color: var(--text-dim);
  cursor: pointer;
  transition: all 0.2s var(--ease);
}
.ed-upload:hover { color: var(--text); border-color: var(--accent); background: var(--accent-soft); }
.ed-fields { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 10px; }
.ed-field { display: flex; flex-direction: column; gap: 5px; font-size: 11.5px; color: var(--text-mute); }
.ed-field.block { padding: 6px 0 14px; }
.ed-field input,
.ed-textarea {
  width: 100%;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-soft);
  color: var(--text);
  font-size: 12px;
  line-height: 1.7;
}
.ed-textarea { font-family: 'SF Mono', Menlo, 'PingFang SC', monospace; font-size: 11.5px; resize: vertical; }

/* 大图查看 */
.viewer-mask {
  position: fixed;
  inset: 0;
  z-index: 9200;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px;
  background: rgba(4, 4, 9, 0.9);
  backdrop-filter: blur(6px);
}
.viewer-mask img { max-width: 100%; max-height: 100%; border-radius: 10px; display: block; }
.viewer-x {
  position: absolute;
  top: 20px;
  right: 22px;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  font-size: 20px;
  color: var(--text-dim);
  background: rgba(255, 255, 255, 0.08);
}
.viewer-x:hover { color: var(--text); background: rgba(255, 255, 255, 0.16); }

/* ===== 分镜工作台 ===== */
.sb-wrap { display: flex; flex-direction: column; gap: 12px; }
.sb-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 10px 12px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
}
.sb-left, .sb-right { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.sb-btn {
  padding: 6px 12px;
  border-radius: 8px;
  font-size: 12.5px;
  font-weight: 550;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-dim);
  transition: all 0.2s var(--ease);
  white-space: nowrap;
}
.sb-btn:hover:not(:disabled) { color: var(--text); border-color: var(--border-strong); background: var(--surface-hover); }
.sb-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.sb-btn.primary {
  color: #fff;
  border-color: transparent;
  background: var(--grad);
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.sb-btn.primary:hover:not(:disabled) { filter: brightness(1.08); }

.workbench {
  display: grid;
  grid-template-columns: 220px 1fr 200px;
  grid-template-rows: 1fr auto;
  gap: 12px;
  height: calc(100vh - 350px);
  min-height: 460px;
}
.wb-col {
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.wb-head {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 11px 13px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
  font-weight: 600;
  flex-shrink: 0;
}
.wb-count {
  margin-left: auto;
  font-size: 10.5px;
  color: var(--text-mute);
  padding: 1px 7px;
  border-radius: 99px;
  background: rgba(255, 255, 255, 0.05);
}
.frames-scroll { flex: 1; overflow-y: auto; padding: 7px; }
.frame-mini {
  display: flex;
  gap: 9px;
  padding: 9px 10px;
  border-radius: 9px;
  cursor: pointer;
  transition: all 0.22s var(--ease);
  border: 1px solid transparent;
}
.frame-mini:hover { background: var(--surface-hover); }
.frame-mini.on { background: var(--accent-soft); border-color: rgba(124, 92, 255, 0.32); }
.fm-idx { font-size: 10.5px; font-weight: 680; color: var(--accent); flex-shrink: 0; padding-top: 1px; }
.fm-body { min-width: 0; flex: 1; }
.fm-body p {
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--text-dim);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.fm-tags { margin-top: 5px; display: flex; gap: 4px; flex-wrap: wrap; }
.mini-tag { font-size: 10px; padding: 1px 5px; border-radius: 3px; color: var(--text-mute); background: rgba(255, 255, 255, 0.06); }
.mini-tag.dur { color: #c4b5fd; background: rgba(124, 92, 255, 0.16); }
.mini-tag.ok { color: #6ee7b7; background: rgba(16, 185, 129, 0.16); }
.mini-tag.busy { color: #fcd34d; background: rgba(251, 191, 36, 0.16); }
.fm-thumb { width: 34px; height: 46px; border-radius: 5px; object-fit: cover; flex-shrink: 0; align-self: center; }

.main-col { min-width: 0; }
.fd-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 11px 14px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.fd-title { font-size: 13px; font-weight: 600; }
.fd-meta { font-size: 11.5px; font-weight: 450; color: var(--text-mute); margin-left: 8px; }
.fd-acts { display: flex; gap: 7px; }
.main-inner {
  flex: 1;
  overflow-y: auto;
  padding: 12px;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  align-content: start;
}
/* 有画面时左右分栏：左边看画面、右边改文字，不再上下堆着留大片空白 */
.main-inner.with-media {
  grid-template-columns: minmax(200px, 300px) minmax(0, 1fr);
  align-items: start;
}
/* 有镜头拆解时它单独占一栏：文案/描述词一列，拆解一列 */
.main-inner.with-shots {
  grid-template-columns: minmax(0, 1fr) minmax(200px, 260px);
}
.main-inner.with-media.with-shots {
  grid-template-columns: minmax(160px, 240px) minmax(0, 1fr) minmax(200px, 260px);
}
.fd-media {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  position: sticky;
  top: 0;
}
.fd-text { display: flex; flex-direction: column; gap: 12px; min-width: 0; }

.fd-video { border-radius: var(--r-md); overflow: hidden; border: 1px solid var(--border); background: #000; }
.fd-video video { width: 100%; max-height: 340px; display: block; }
.fd-busy {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 22px;
  border-radius: var(--r-md);
  border: 1px dashed var(--border);
  font-size: 12.5px;
  color: var(--text-mute);
}
.fd-ff { border-radius: var(--r-md); border: 1px solid var(--border); overflow: hidden; background: var(--bg-soft); }
.fd-ff-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  font-size: 11.5px;
  color: var(--text-dim);
  border-bottom: 1px solid var(--border);
}
.fd-ff-note { color: var(--text-mute); }
.fd-ff-body { position: relative; }
.fd-ff-zoom {
  width: 20px;
  height: 20px;
  display: grid;
  place-items: center;
  border-radius: 5px;
  color: var(--text-mute);
  background: rgba(255, 255, 255, 0.05);
  transition: all 0.2s var(--ease);
}
.fd-ff-zoom:hover { color: var(--text); background: rgba(255, 255, 255, 0.12); }
.fd-ff img.zoomable { cursor: zoom-in; }
.fd-ff-mask {
  position: absolute;
  inset: 0;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 8px;
  background: rgba(8, 8, 14, 0.6);
  backdrop-filter: blur(2px);
  font-size: 11px;
  color: var(--text-dim);
  text-align: center;
}
.fd-ff-mask .spinner { width: 18px; height: 18px; }
.fd-ff-empty {
  padding: 46px 12px;
  text-align: center;
  font-size: 11px;
  color: var(--text-mute);
}
.fd-ff img {
  display: block;
  margin: 10px auto;
  width: auto;
  max-width: calc(100% - 16px);
  max-height: 420px;
  object-fit: contain;
  border-radius: 6px;
}

.prompt-block { border-radius: var(--r-md); border: 1px solid var(--border); background: var(--bg-soft); overflow: hidden; }
.pb-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 13px;
  border-bottom: 1px solid var(--border);
  font-size: 11.5px;
  font-weight: 600;
  color: var(--text-dim);
}
.pb-badge {
  font-size: 10px;
  font-family: 'SF Mono', Menlo, monospace;
  color: var(--text-mute);
  padding: 1px 6px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.05);
}
.fd-textarea {
  width: 100%;
  min-height: 300px;
  padding: 13px;
  font-family: 'SF Mono', Menlo, 'PingFang SC', monospace;
  font-size: 11.5px;
  line-height: 1.8;
  color: var(--text-dim);
  background: transparent;
  resize: vertical;
  display: block;
}
.fd-textarea.short { min-height: 80px; }

.fd-shots { border-radius: var(--r-md); border: 1px solid var(--border); overflow: hidden; }
.fd-shots-head { padding: 9px 13px; font-size: 11.5px; font-weight: 600; color: var(--text-dim); border-bottom: 1px solid var(--border); }
.fd-shot { padding: 10px 13px; border-bottom: 1px solid var(--border); }
.fd-shot:last-child { border-bottom: none; }
.fd-shot-top { display: flex; align-items: center; gap: 8px; font-size: 11px; color: var(--text-mute); }
.fd-shot-no { color: var(--accent); font-weight: 600; }
.fd-shot-time { font-family: 'SF Mono', Menlo, monospace; }
.fd-shot-size { padding: 0 6px; border-radius: 3px; background: rgba(255, 255, 255, 0.06); }
.fd-shot-cam { opacity: 0.8; }
.fd-shot-warn { margin-left: auto; color: var(--warn); }
.fd-shot-line { font-size: 12px; color: var(--text); margin-top: 5px; line-height: 1.6; }
.fd-shot-act { font-size: 11.5px; color: var(--text-mute); margin-top: 3px; line-height: 1.55; }

.side-scroll { flex: 1; overflow-y: auto; padding: 11px; }
.side-group { margin-bottom: 16px; }
.sg-title { display: flex; align-items: center; gap: 6px; font-size: 11px; font-weight: 600; color: var(--text-dim); margin-bottom: 7px; }
.side-item { display: flex; gap: 7px; padding: 6px 8px; border-radius: 7px; background: rgba(255, 255, 255, 0.022); margin-bottom: 5px; }
.si-thumb { width: 26px; height: 34px; border-radius: 4px; object-fit: cover; flex-shrink: 0; }
.si-body { min-width: 0; flex: 1; }
.si-name { display: block; font-size: 11.5px; font-weight: 550; color: var(--accent); margin-bottom: 1px; }
.si-desc {
  display: -webkit-box;
  font-size: 10.5px;
  color: var(--text-mute);
  line-height: 1.45;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.side-empty { font-size: 11px; color: var(--text-mute); padding: 4px 8px; }

.wb-foot {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border);
  background: var(--surface);
}
.wf-stat { font-size: 12px; color: var(--text-mute); }
.wf-right { display: flex; gap: 8px; }
.wb-empty { flex: 1; display: grid; place-items: center; color: var(--text-mute); font-size: 12.5px; }

/* 成片 */
.ph-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }

/* 整集合成的成片卡片：黑底播放器 + 一条信息栏 */
.composed {
  margin-bottom: 18px;
  border-radius: var(--r-md);
  border: 1px solid var(--border-strong);
  background: #000;
  overflow: hidden;
}
.composed-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  background: var(--bg-soft);
  border-bottom: 1px solid var(--border);
}
.composed-badge {
  font-size: 11px;
  font-weight: 570;
  color: var(--accent);
  background: var(--accent-soft);
  padding: 2px 8px;
  border-radius: 5px;
}
.composed-meta { font-size: 12px; color: var(--text-dim); }
.composed-warn { font-size: 12px; color: var(--warn); }
.composed-dl {
  margin-left: auto;
  font-size: 12px;
  color: var(--text-dim);
  text-decoration: none;
  border: 1px solid var(--border);
  padding: 3px 10px;
  border-radius: 6px;
  transition: color 0.15s, border-color 0.15s;
}
.composed-dl:hover { color: var(--text); border-color: var(--border-strong); }
.composed video { display: block; width: 100%; max-height: 520px; background: #000; }

.video-body { padding: 20px 24px; }
.vsummary {
  display: flex;
  gap: 26px;
  padding: 15px 18px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
  margin-bottom: 18px;
}
.vs-item { display: flex; flex-direction: column; gap: 2px; }
.vs-label { font-size: 10.5px; color: var(--text-mute); }
.vs-value { font-size: 17px; font-weight: 680; letter-spacing: -0.02em; }
.vs-value.done { color: var(--ok); }

.vlist { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; }
.vitem { border-radius: var(--r-md); border: 1px solid var(--border); background: rgba(255, 255, 255, 0.018); overflow: hidden; }
.vitem-head { display: flex; align-items: center; gap: 8px; padding: 8px 11px; border-bottom: 1px solid var(--border); }
.vitem-no { font-size: 11.5px; font-weight: 600; }
.vitem-dur { font-size: 10.5px; color: var(--text-mute); }
.vitem-go { margin-left: auto; font-size: 10.5px; color: var(--accent); }
.vitem video { width: 100%; display: block; background: #000; }
.vitem-empty { display: grid; place-items: center; min-height: 120px; font-size: 11.5px; color: var(--text-mute); }

/* ===== 弹窗 ===== */
.mask {
  position: fixed;
  inset: 0;
  z-index: 9100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(4, 4, 9, 0.72);
  backdrop-filter: blur(4px);
}
.modal {
  width: 100%;
  max-width: 720px;
  max-height: 86vh;
  display: flex;
  flex-direction: column;
  border-radius: var(--r-xl);
  border: 1px solid var(--border-strong);
  background: var(--bg-elev);
  box-shadow: 0 30px 80px -30px rgba(0, 0, 0, 0.9);
  overflow: hidden;
}
.modal.wide { max-width: 1000px; }
.modal-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 20px 14px;
  border-bottom: 1px solid var(--border);
}
.modal-head h3 { font-size: 15.5px; font-weight: 640; }
.modal-sub { font-size: 11.5px; color: var(--text-mute); }
.modal-sub .warn { color: var(--warn); }
.modal-x {
  width: 26px;
  height: 26px;
  border-radius: 7px;
  font-size: 17px;
  line-height: 1;
  color: var(--text-mute);
  display: grid;
  place-items: center;
}
.modal-x:hover { color: var(--text); background: var(--surface-hover); }
.modal-body { flex: 1; overflow-y: auto; padding: 6px 20px; }
.modal-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  justify-content: flex-end;
  padding: 13px 20px 15px;
  border-top: 1px solid var(--border);
}
.modal-tip { flex: 1; font-size: 11px; color: var(--text-mute); }
.modal-empty { padding: 30px; text-align: center; font-size: 12px; color: var(--text-mute); }

.ffg { display: flex; gap: 14px; padding: 14px 0; border-bottom: 1px solid var(--border); }
.ffg:last-child { border-bottom: none; }
.ffg-thumb {
  width: 88px;
  height: 140px;
  flex: 0 0 88px;
  border-radius: 8px;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.04);
  display: grid;
  place-items: center;
  font-size: 11px;
  color: var(--text-mute);
}
.ffg-thumb { position: relative; }
.ffg-thumb img.zoomable { cursor: zoom-in; }
.ffg-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
/* 生成中：缩略图上盖一层，避免「点了没反应」的错觉 */
.ffg-mask {
  position: absolute;
  inset: 0;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 7px;
  background: rgba(8, 8, 14, 0.66);
  backdrop-filter: blur(3px);
  font-size: 10.5px;
  color: var(--text-dim);
}
.ffg-mask .spinner { width: 16px; height: 16px; }
/* 失败态：红字 + 截断的错误原因，鼠标悬停看完整内容 */
.ffg-mask.fail { background: rgba(40, 12, 12, 0.78); color: var(--err); }
.ffg-mask.fail .ffg-mask-t { font-weight: 570; }
.ffg-mask-e {
  max-width: 96px;
  font-size: 9.5px;
  line-height: 1.4;
  color: var(--text-mute);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.ff-busy-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
  margin-right: 6px;
  vertical-align: 1px;
  animation: ff-pulse 1.2s ease-in-out infinite;
}
@keyframes ff-pulse {
  0%, 100% { opacity: 0.3; }
  50% { opacity: 1; }
}
.ffg-body { flex: 1; min-width: 0; }
.ffg-title { font-size: 12.5px; color: var(--text); margin-bottom: 6px; }
.ffg-count { margin-left: 6px; font-size: 11px; padding: 1px 7px; border-radius: 99px; color: var(--warn); background: rgba(251, 191, 36, 0.12); }
.ffg-desc {
  width: 100%;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-soft);
  font-size: 11.5px;
  line-height: 1.6;
  color: var(--text-dim);
  /* 高度由 v-autosize 按内容算；手动拖拽会和自动增高打架，关掉 */
  resize: none;
  /* 兜底：描述再长也不让弹窗被撑爆，超出才出滚动条 */
  min-height: 64px;
  max-height: 320px;
  overflow-y: auto;
}
.ffg-foot { display: flex; align-items: center; gap: 10px; margin-top: 6px; }
.ffg-frames { flex: 1; font-size: 11px; color: var(--text-mute); }
.link-btn { font-size: 11px; color: var(--accent); }

.sheet { width: 100%; border-collapse: collapse; font-size: 11.5px; }
.sheet th {
  position: sticky;
  top: 0;
  text-align: left;
  padding: 8px 8px;
  font-size: 10.5px;
  font-weight: 600;
  color: var(--text-mute);
  background: var(--bg-elev);
  border-bottom: 1px solid var(--border);
}
.sheet td { padding: 7px 8px; border-bottom: 1px solid var(--border); color: var(--text-dim); vertical-align: top; }
.sheet tbody tr { cursor: pointer; }
.sheet tbody tr:hover td { background: var(--surface-hover); }
.sheet .dim { color: var(--text-mute); }
.sheet .mono { font-family: 'SF Mono', Menlo, monospace; }
.sheet .line { max-width: 380px; }
.sheet .ok { color: var(--ok); }
.sheet .bad { color: var(--err); font-weight: 600; }

/* 1101–1360：工作台还是三栏，中间只剩 ~760，三列会把文案挤到 200 出头，
   这段改成「画面在文案上方、拆解占右栏」，内容一个不少，文案也够宽 */
@media (max-width: 1360px) and (min-width: 1101px) {
  .main-inner.with-media.with-shots {
    grid-template-columns: minmax(0, 1fr) minmax(200px, 240px);
    grid-template-areas:
      "media shots"
      "text  shots";
  }
  .main-inner.with-media.with-shots .fd-media { grid-area: media; position: static; }
  .main-inner.with-media.with-shots .fd-text { grid-area: text; }
  .main-inner.with-media.with-shots .fd-shots { grid-area: shots; }
}
@media (max-width: 1100px) {
  .workbench { grid-template-columns: 1fr; height: auto; }
  /* 窄屏没有第二栏的空间，画面回到文字上方 */
  .main-inner.with-media { grid-template-columns: minmax(0, 1fr); }
  .fd-media { position: static; }
  .frames-col, .side-col { display: none; }
  .main-col { min-height: 420px; }
}
@media (max-width: 720px) {
  .stepper { overflow-x: auto; gap: 14px; }
  .step-text { display: none; }
  .panel-head, .panel-foot { padding-left: 16px; padding-right: 16px; }
  .editor { margin: 16px; }
  .asset-wrap { padding: 16px 16px 4px; }
  .vsummary { flex-wrap: wrap; gap: 18px; }
}
/* ===== 场景卡：左=原场景图，右=柱形全景图 / 360视频 上下排列 ===== */
.scene-grid { grid-template-columns: repeat(auto-fill, minmax(420px, 1fr)); }
.scene-card {
  flex-direction: row;
  align-items: stretch;
  gap: 10px;
  padding: 8px;
  background: rgba(255, 255, 255, 0.012);
}
.scene-main {
  display: flex;
  flex-direction: column;
  width: 190px;
  flex: none;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.018);
  overflow: hidden;
}
.scene-side {
  flex: 1;
  min-width: 0;
  display: grid;
  grid-template-rows: 1fr 1fr;
  gap: 8px;
}
.scene-panel {
  display: flex;
  flex-direction: column;
  border-radius: var(--r-md);
  border: 1px dashed var(--border);
  background: rgba(255, 255, 255, 0.015);
  overflow: hidden;
  min-height: 0;
}
.scene-panel .panel-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--border);
  flex: none;
}
.panel-label {
  flex: 1;
  font-size: 10.5px;
  color: var(--text-mute);
  letter-spacing: 0.3px;
}
.panel-media {
  flex: 1;
  min-height: 0;
  position: relative;
  background: rgba(0, 0, 0, 0.25);
  display: grid;
  place-items: center;
  overflow: hidden;
}
.panel-media img,
.panel-media video {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.panel-media img { cursor: zoom-in; }
.panel-media.video video { cursor: pointer; }
.pano-play {
  position: absolute;
  inset: 0;
  margin: auto;
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.5);
  border: 1px solid rgba(255, 255, 255, 0.55);
  color: #fff;
  font-size: 11px;
  cursor: pointer;
  backdrop-filter: blur(4px);
}
.panel-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 8px;
  font-size: 10.5px;
  line-height: 1.5;
  text-align: center;
  color: var(--text-mute);
}
/* 窄屏：场景卡回退为竖排，三块纵向堆叠 */
@media (max-width: 720px) {
  .scene-card { flex-direction: column; }
  .scene-main { width: 100%; }
  .scene-side { grid-template-rows: auto auto; }
  .scene-panel { min-height: 120px; }
}
@supports not (aspect-ratio: 1) {
  .scene-main .asset-thumb { padding-top: 75%; }
}

.si-video {
  object-fit: cover;
  background: #000;
}
.si-badge {
  display: inline-block;
  margin-right: 4px;
  padding: 0 4px;
  border-radius: 3px;
  background: rgba(75, 139, 245, 0.16);
  color: #7aa7ff;
  font-size: 10px;
  line-height: 15px;
}
/* 大纲确认弹窗 */
.ol-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 300;
}
.ol-panel {
  width: min(720px, 92vw);
  max-height: 82vh;
  display: flex;
  flex-direction: column;
  background: var(--panel, #17181c);
  border: 1px solid var(--line, #2a2d34);
  border-radius: 12px;
  overflow: hidden;
}
.ol-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 18px 12px;
  border-bottom: 1px solid var(--line, #2a2d34);
}
.ol-head h3 { margin: 0 0 4px; font-size: 15px; }
.ol-head p { margin: 0; font-size: 12px; color: var(--text-dim, #8a8f98); }
.ol-count {
  flex: none;
  font-size: 12px;
  color: var(--text-dim, #8a8f98);
  background: rgba(255, 255, 255, 0.04);
  border-radius: 999px;
  padding: 3px 10px;
}
.ol-body {
  flex: 1;
  overflow-y: auto;
  padding: 12px 18px;
}
.ol-item {
  display: flex;
  gap: 10px;
  padding: 10px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}
.ol-item:last-child { border-bottom: none; }
.ol-idx {
  flex: none;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: rgba(75, 139, 245, 0.16);
  color: #7aa7ff;
  font-size: 11px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.ol-main { flex: 1; min-width: 0; }
.ol-meta { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.ol-scene { font-size: 12px; color: #7aa7ff; }
.ol-role {
  font-size: 11px;
  color: var(--text-dim, #8a8f98);
}
.ol-item-tag {
  font-size: 11px;
  color: #8a8f98;
  border: 1px solid var(--line, #2a2d34);
  border-radius: 4px;
  padding: 0 4px;
}
.ol-beat { margin: 5px 0 0; font-size: 13px; line-height: 1.55; }
.ol-lines {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--text-dim, #8a8f98);
  line-height: 1.6;
}
.ol-lines span { margin-right: 4px; }
.ol-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 18px;
  border-top: 1px solid var(--line, #2a2d34);
}
.voice-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.voice-audio {
  height: 32px;
  flex: 1;
  min-width: 160px;
}
.ed-hint {
  font-size: 12px;
  color: var(--text-dim, #8a8f98);
}
.ed-opt {
  margin-left: 6px;
  font-style: normal;
  font-size: 11px;
  color: var(--text-dim, #8a8f98);
}
.seg {
  display: inline-flex;
  gap: 2px;
  padding: 2px;
  border-radius: 7px;
  background: rgba(255, 255, 255, 0.05);
  margin-bottom: 8px;
}
.seg-btn {
  border: none;
  background: transparent;
  color: var(--text-dim, #8a8f98);
  font-size: 12px;
  padding: 4px 12px;
  border-radius: 5px;
  cursor: pointer;
}
.seg-btn.on {
  background: rgba(75, 139, 245, 0.18);
  color: #7aa7ff;
}
.voice-input { width: 100%; }
.ffg-actions {
  display: inline-flex;
  align-items: center;
  gap: 12px;
}
.link-btn.accent {
  color: #7aa7ff;
}
.link-btn.accent:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
