<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import { toast as notify } from '../composables/useToast'
import { confirmDialog } from '../composables/useConfirm'

const route = useRoute()
const router = useRouter()
const projectId = String(route.params.id || '')

type TabKey = 'episodes' | 'characters' | 'scenes' | 'items'
const tab = ref<TabKey>('episodes')

const project = ref<any>(null)
const episodes = ref<any[]>([])
const loading = ref(true)

// 三类项目资产库
const lib = ref<{ characters: any[]; scenes: any[]; items: any[] }>({
  characters: [], scenes: [], items: []
})

// 总角色场景库（抽屉）
const drawerOpen = ref(false)
const drawerTab = ref<'characters' | 'scenes' | 'items'>('characters')
const globalLib = ref<{ characters: any[]; scenes: any[]; items: any[] }>({
  characters: [], scenes: [], items: []
})
const globalLoading = ref(false)

// 新增 / 编辑弹窗
const editor = ref({
  open: false,
  mode: 'create' as 'create' | 'edit',
  kind: 'character' as 'character' | 'scene' | 'item',
  id: '',
  name: '',
  prompt: '',
  referenceImage: '',
  voiceTone: '',
  saving: false,
  uploading: false
})

const KIND_LABEL: Record<string, string> = { character: '角色', scene: '场景', item: '物品' }
const kindLabel = (k: string) => KIND_LABEL[k] || '资产'

const TABS: { key: TabKey; label: string }[] = [
  { key: 'episodes', label: '剧集管理' },
  { key: 'characters', label: '项目角色库' },
  { key: 'scenes', label: '项目场景库' },
  { key: 'items', label: '项目物品库' }
]

const toast = (msg: string) => {
  if (/失败|不足|错误|请先|不能/.test(msg)) notify.error(msg)
  else notify.success(msg)
}

const statusText = (s: string) => {
  switch (s) {
    case 'storyboard': return '已分镜'
    case 'video': return '已出片'
    case 'done': return '已完成'
    default: return '待制作'
  }
}

/** 剧集标题：title 里已经写了「第N集」就不再拼前缀，避免「第1集 - 第一集」 */
const epTitle = (e: any) => {
  const n = Number(e.episodeNumber) || 1
  const t = String(e.title || '').trim()
  if (!t) return `第${n}集`
  if (/第\s*[0-9０-９一二三四五六七八九十]+\s*集/.test(t)) return t
  return `第${n}集 - ${t}`
}

const currentList = computed(() => {
  if (tab.value === 'characters') return lib.value.characters
  if (tab.value === 'scenes') return lib.value.scenes
  if (tab.value === 'items') return lib.value.items
  return []
})

const currentKind = computed(() => {
  if (tab.value === 'characters') return 'character'
  if (tab.value === 'scenes') return 'scene'
  return 'item'
})

const load = async () => {
  loading.value = true
  try {
    const p: any = await api.get(`/projects/${projectId}`)
    project.value = p.data
    await Promise.all([loadEpisodes(), loadAssets()])
  } catch (e: any) {
    toast(e.response?.data?.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const loadEpisodes = async () => {
  const res: any = await api.get('/episodes', { params: { projectId } })
  episodes.value = res.data || []
}

const loadAssets = async () => {
  const res: any = await api.get(`/projects/${projectId}/assets`)
  lib.value = {
    characters: res.data?.characters || [],
    scenes: res.data?.scenes || [],
    items: res.data?.items || []
  }
}

const loadGlobal = async () => {
  globalLoading.value = true
  try {
    const res: any = await api.get('/library/assets', { params: { projectId } })
    globalLib.value = {
      characters: res.data?.characters || [],
      scenes: res.data?.scenes || [],
      items: res.data?.items || []
    }
  } catch (e: any) {
    toast(e.response?.data?.message || '总库加载失败')
  } finally {
    globalLoading.value = false
  }
}

// ---------- 剧集 ----------
const createEpisode = async () => {
  const next = episodes.value.length + 1
  try {
    const res: any = await api.post('/episodes', {
      projectId,
      episodeNumber: next,
      title: `第${next}集`
    })
    const ep = res.data
    episodes.value.push(ep)
    toast('已新增剧集')
    // 新建后直接进创作页，省一次点击
    router.push(`/projects/${projectId}/create?episodeId=${ep.id}`)
  } catch (e: any) {
    toast(e.response?.data?.message || '新增失败')
  }
}

const openEpisode = (ep: any) => {
  router.push(`/projects/${projectId}/create?episodeId=${ep.id}`)
}

// ---------- 资产编辑 ----------
const openCreate = () => {
  editor.value = {
    open: true, mode: 'create', kind: currentKind.value as any,
    id: '', name: '', prompt: '', referenceImage: '', voiceTone: '',
    saving: false, uploading: false
  }
}

const openEdit = (kind: string, a: any) => {
  editor.value = {
    open: true, mode: 'edit', kind: kind as any,
    id: a.id, name: a.name || '', prompt: a.imagePrompt || '',
    referenceImage: a.referenceImage || '', voiceTone: a.voiceTone || '',
    saving: false, uploading: false
  }
}

const pickImage = () => {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = 'image/*'
  input.onchange = async () => {
    const file = input.files?.[0]
    if (!file) return
    if (file.size > 20 << 20) return toast('图片不能超过 20MB')
    editor.value.uploading = true
    try {
      const fd = new FormData()
      fd.append('file', file)
      fd.append('kind', 'image')
      const res: any = await api.post('/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
      if (res.data?.url) editor.value.referenceImage = res.data.url
      toast('上传成功')
    } catch (e: any) {
      toast(e.response?.data?.message || '上传失败')
    } finally {
      editor.value.uploading = false
    }
  }
  input.click()
}

const saveAsset = async () => {
  const e = editor.value
  if (!e.name.trim()) return toast('名称不能为空')
  e.saving = true
  try {
    if (e.mode === 'create') {
      await api.post(`/projects/${projectId}/assets/${e.kind}`, {
        name: e.name.trim(),
        imagePrompt: e.prompt,
        referenceImage: e.referenceImage,
        voiceTone: e.voiceTone
      })
      toast('已添加')
    } else {
      await api.put(`/assets/${e.kind}/${e.id}`, {
        name: e.name.trim(),
        imagePrompt: e.prompt,
        referenceImage: e.referenceImage,
        ...(e.kind === 'character' ? { voiceTone: e.voiceTone } : {})
      })
      toast('已保存')
    }
    e.open = false
    await loadAssets()
  } catch (err: any) {
    toast(err.response?.data?.message || '保存失败')
  } finally {
    e.saving = false
  }
}

const removeAsset = async (kind: string, a: any) => {
  const ok = await confirmDialog({
    title: `删除${kindLabel(kind)}「${a.name}」`,
    desc: '删除后不可恢复。引用它的分镜会在重新生成时更新。',
    confirmText: '删除',
    danger: true
  })
  if (!ok) return
  try {
    await api.delete(`/assets/${kind}/${a.id}`)
    await loadAssets()
    toast('已删除')
  } catch (e: any) {
    toast(e.response?.data?.message || '删除失败')
  }
}

// ---------- 总库 ----------
const openDrawer = async () => {
  drawerOpen.value = true
  await loadGlobal()
}

const importAsset = async (kind: string, a: any) => {
  if (a.added) return
  try {
    await api.post(`/projects/${projectId}/assets/${kind}/import`, { assetId: a.id })
    toast('已添加到项目')
    await Promise.all([loadAssets(), loadGlobal()])
  } catch (e: any) {
    toast(e.response?.data?.message || '添加失败')
  }
}

const drawerList = computed(() => globalLib.value[drawerTab.value] || [])
const drawerKind = computed(() =>
  drawerTab.value === 'characters' ? 'character' : drawerTab.value === 'scenes' ? 'scene' : 'item'
)

onMounted(load)
</script>

<template>
  <div class="manage">
    <!-- 顶部：返回 + 面包屑 -->
    <div class="top-bar">
      <button class="back" @click="router.push('/projects')">
        <svg viewBox="0 0 24 24" fill="none" width="14" height="14">
          <path d="M15 6l-6 6 6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        返回
      </button>
      <span class="tb-sep"></span>
      <nav class="crumbs">
        <router-link to="/" class="crumb">首页</router-link>
        <span class="crumb-sep">›</span>
        <router-link to="/projects" class="crumb">我的项目</router-link>
        <span class="crumb-sep">›</span>
        <span class="crumb on">{{ project?.name || '项目' }}</span>
      </nav>
    </div>

    <div v-if="loading" class="loading">加载中…</div>

    <template v-else>
      <!-- 功能 tab -->
      <div class="lib-tabs">
        <button
          v-for="t in TABS"
          :key="t.key"
          class="lib-tab"
          :class="{ on: tab === t.key }"
          @click="tab = t.key"
        >
          {{ t.label }}
        </button>
        <button class="lib-tab drawer" :class="{ on: drawerOpen }" @click="openDrawer">
          总角色场景库
          <svg viewBox="0 0 24 24" fill="none" width="12" height="12">
            <path d="M9 6l6 6-6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>

      <!-- 剧集管理 -->
      <section v-if="tab === 'episodes'" class="panel">
        <div class="panel-head">
          <h2>剧集管理 <span class="cnt">{{ episodes.length }}</span></h2>
          <p>每一集独立走「剧本 → 资产 → 分镜 → 成片」，点进去继续制作</p>
        </div>
        <div class="ep-list">
          <div v-for="e in episodes" :key="e.id" class="ep-row" @click="openEpisode(e)">
            <div class="ep-thumb">{{ e.episodeNumber }}</div>
            <div class="ep-main">
              <h4>{{ epTitle(e) }}</h4>
              <span class="ep-status">{{ statusText(e.status) }}</span>
            </div>
            <button class="btn btn-ghost btn-sm" @click.stop="openEpisode(e)">进入制作</button>
          </div>
          <button class="ep-add" @click="createEpisode">
            <svg viewBox="0 0 24 24" fill="none" width="16" height="16">
              <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
            新增剧集
          </button>
        </div>
      </section>

      <!-- 项目资产库 -->
      <section v-else class="panel">
        <div class="panel-head">
          <h2>
            {{ tab === 'characters' ? '项目角色库' : tab === 'scenes' ? '项目场景库' : '项目物品库' }}
            <span class="cnt">{{ currentList.length }}</span>
          </h2>
          <p>这里汇总本项目所有剧集用到的{{ kindLabel(currentKind) }}，改动会同步到分镜</p>
        </div>

        <div class="lib-grid">
          <div v-for="a in currentList" :key="a.id" class="lib-card">
            <div class="lib-thumb">
              <img v-if="a.referenceImage && a.referenceImage.startsWith('/static/')" :src="a.referenceImage" :alt="a.name" />
              <span v-else>{{ a.name?.[0] || '?' }}</span>
            </div>
            <div class="lib-body">
              <h4>{{ a.name }}</h4>
              <p v-if="a.voiceTone" class="lib-voice">音色：{{ a.voiceTone }}</p>
              <p class="lib-desc">{{ a.imagePrompt || '（还没写描述词）' }}</p>
            </div>
            <div class="lib-acts">
              <button class="btn btn-ghost btn-sm" @click="openEdit(currentKind, a)">编辑</button>
              <button class="btn btn-ghost btn-sm danger" @click="removeAsset(currentKind, a)">删除</button>
            </div>
          </div>

          <button class="lib-add" @click="openCreate">
            <svg viewBox="0 0 24 24" fill="none" width="18" height="18">
              <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
            </svg>
            <span>新增{{ kindLabel(currentKind) }}</span>
          </button>
        </div>
      </section>
    </template>

    <!-- 新增 / 编辑资产 -->
    <Teleport to="body">
      <div v-if="editor.open" class="ed-mask" @click.self="editor.open = false">
        <div class="ed-box">
          <header class="ed-head">
            <h3>{{ editor.mode === 'create' ? '新增' : '编辑' }}{{ kindLabel(editor.kind) }}</h3>
            <button class="ed-x" @click="editor.open = false">
              <svg viewBox="0 0 24 24" fill="none" width="16" height="16">
                <path d="M6 6l12 12M18 6 6 18" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
              </svg>
            </button>
          </header>
          <div class="ed-body">
            <div class="ed-ref">
              <div class="ed-thumb">
                <img v-if="editor.referenceImage" :src="editor.referenceImage" alt="参考图" />
                <span v-else>{{ editor.name?.[0] || '?' }}</span>
              </div>
              <div class="ed-ref-info">
                <button class="btn btn-ghost btn-sm" :disabled="editor.uploading" @click="pickImage">
                  {{ editor.uploading ? '上传中…' : editor.referenceImage ? '更换参考图' : '上传新图片' }}
                </button>
                <p class="ed-hint">点击上传/替换{{ kindLabel(editor.kind) }}参考图</p>
              </div>
            </div>

            <label class="ed-label">{{ kindLabel(editor.kind) }}名称 <span class="req">*</span></label>
            <input v-model="editor.name" class="ed-input" maxlength="30" :placeholder="`请输入${kindLabel(editor.kind)}名称`" />

            <label class="ed-label">{{ kindLabel(editor.kind) }}描述词 <span class="req">*</span></label>
            <textarea
              v-model="editor.prompt"
              class="ed-textarea"
              rows="5"
              placeholder="只写资产本身：外貌逐项 → 服装/陈设 → 材质细节"
            ></textarea>

            <template v-if="editor.kind === 'character'">
              <label class="ed-label">角色音色 <span class="opt">可选</span></label>
              <textarea v-model="editor.voiceTone" class="ed-textarea" rows="2" maxlength="120" placeholder="松弛年轻男音：中音域，清朗温厚，语速稍快。"></textarea>
            </template>
          </div>
          <footer class="ed-foot">
            <button class="btn btn-ghost btn-sm" @click="editor.open = false">取消</button>
            <button class="btn btn-primary btn-sm" :disabled="editor.saving" @click="saveAsset">
              {{ editor.saving ? '保存中…' : '保存' }}
            </button>
          </footer>
        </div>
      </div>
    </Teleport>

    <!-- 总角色场景库抽屉 -->
    <Teleport to="body">
      <div v-if="drawerOpen" class="dr-mask" @click.self="drawerOpen = false">
        <aside class="dr-box">
          <header class="dr-head">
            <h3>总角色场景库</h3>
            <button class="ed-x" @click="drawerOpen = false">
              <svg viewBox="0 0 24 24" fill="none" width="16" height="16">
                <path d="M6 6l12 12M18 6 6 18" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
              </svg>
            </button>
          </header>
          <div class="dr-tabs">
            <button class="dr-tab" :class="{ on: drawerTab === 'characters' }" @click="drawerTab = 'characters'">角色库</button>
            <button class="dr-tab" :class="{ on: drawerTab === 'scenes' }" @click="drawerTab = 'scenes'">场景库</button>
            <button class="dr-tab" :class="{ on: drawerTab === 'items' }" @click="drawerTab = 'items'">物品库</button>
          </div>
          <div class="dr-body">
            <p v-if="globalLoading" class="dr-empty">加载中…</p>
            <p v-else-if="!drawerList.length" class="dr-empty">还没有可用的{{ kindLabel(drawerKind) }}，先在任一项目里创建</p>
            <div v-for="a in drawerList" :key="a.id" class="dr-item">
              <div class="dr-thumb">
                <img v-if="a.referenceImage && a.referenceImage.startsWith('/static/')" :src="a.referenceImage" :alt="a.name" />
                <span v-else>{{ a.name?.[0] || '?' }}</span>
              </div>
              <div class="dr-main">
                <h4>{{ a.name }}</h4>
                <p>{{ a.imagePrompt }}</p>
              </div>
              <button class="dr-add" :class="{ done: a.added }" :disabled="a.added" @click="importAsset(drawerKind, a)">
                {{ a.added ? '已添加' : '添加' }}
              </button>
            </div>
          </div>
        </aside>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.manage {
  width: 100%;
  padding: 24px 0 60px;
}

/* 顶部条 */
.top-bar { display: flex; align-items: center; gap: 12px; margin-bottom: 18px; }
.back {
  display: inline-flex; align-items: center; gap: 4px;
  height: 28px; padding: 0 11px 0 8px; border-radius: 8px;
  font-size: 12.5px; color: var(--text-dim);
  background: rgba(255, 255, 255, 0.055);
  border: 1px solid rgba(255, 255, 255, 0.06);
  transition: all 0.22s var(--ease);
}
.back:hover { color: var(--text); background: rgba(255, 255, 255, 0.1); }
.tb-sep { width: 1px; height: 14px; background: rgba(255, 255, 255, 0.13); }
.crumbs { display: flex; align-items: center; gap: 7px; font-size: 12.5px; }
.crumb { color: var(--text-mute); transition: color 0.22s var(--ease); }
a.crumb:hover { color: var(--text); }
.crumb.on { color: var(--text); font-weight: 600; }
.crumb-sep { color: rgba(255, 255, 255, 0.22); font-size: 12px; }

/* 功能 tab */
.lib-tabs { display: flex; align-items: center; gap: 6px; margin-bottom: 22px; flex-wrap: wrap; }
.lib-tab {
  display: inline-flex; align-items: center; gap: 4px;
  height: 32px; padding: 0 14px; border-radius: 8px;
  font-size: 12.5px; color: var(--text-dim);
  background: rgba(255, 255, 255, 0.045);
  border: 1px solid rgba(255, 255, 255, 0.06);
  transition: all 0.2s var(--ease);
}
.lib-tab:hover { color: var(--text); background: rgba(255, 255, 255, 0.08); }
.lib-tab.on {
  color: #fff;
  background: linear-gradient(135deg, #ff8a3d, #ff6a2b);
  border-color: transparent;
  box-shadow: 0 6px 18px -6px rgba(255, 122, 45, 0.7);
}
.lib-tab.drawer { margin-left: auto; }

/* 面板 */
.panel {
  border-radius: 14px;
  border: 1px solid var(--border);
  background: var(--bg-soft);
  padding: 18px 20px 22px;
}
.panel-head h2 { font-size: 16px; font-weight: 650; display: flex; align-items: center; gap: 8px; }
.panel-head .cnt { font-size: 12px; font-weight: 500; color: var(--text-mute); }
.panel-head p { margin-top: 4px; font-size: 12px; color: var(--text-mute); }

/* 剧集列表 */
.ep-list { margin-top: 16px; display: flex; flex-direction: column; gap: 10px; }
.ep-row {
  display: flex; align-items: center; gap: 14px;
  padding: 12px 14px; border-radius: 11px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.025);
  cursor: pointer;
  transition: all 0.2s var(--ease);
}
.ep-row:hover { border-color: rgba(255, 145, 60, 0.4); background: rgba(255, 145, 60, 0.05); }
.ep-thumb {
  width: 34px; height: 34px; border-radius: 9px;
  display: grid; place-items: center;
  font-size: 13px; font-weight: 650; color: #fff;
  background: linear-gradient(135deg, #7c5cff, #5b3fd6);
}
.ep-main { flex: 1; min-width: 0; }
.ep-main h4 { font-size: 13.5px; font-weight: 600; }
.ep-status { font-size: 11px; color: var(--text-mute); }
.ep-add {
  display: flex; align-items: center; justify-content: center; gap: 8px;
  padding: 14px; border-radius: 11px;
  font-size: 12.5px; color: var(--text-mute);
  border: 1px dashed rgba(255, 255, 255, 0.16);
  background: transparent;
  transition: all 0.2s var(--ease);
}
.ep-add:hover { color: var(--text); border-color: rgba(255, 145, 60, 0.5); background: rgba(255, 145, 60, 0.05); }

/* 资产卡片 */
.lib-grid {
  margin-top: 16px;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}
.lib-card {
  display: flex; flex-direction: column;
  border-radius: 12px; overflow: hidden;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.025);
}
.lib-thumb {
  aspect-ratio: 4 / 3;
  display: grid; place-items: center;
  background: #0d0d14;
  font-size: 26px; font-weight: 600; color: var(--text-mute);
  overflow: hidden;
}
.lib-thumb img { width: 100%; height: 100%; object-fit: contain; display: block; }
.lib-body { padding: 10px 12px 6px; }
.lib-body h4 { font-size: 13px; font-weight: 620; }
.lib-voice { margin-top: 3px; font-size: 10.5px; color: #c4b5fd; }
.lib-desc {
  margin-top: 5px; font-size: 11px; line-height: 1.6; color: var(--text-mute);
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.lib-acts { display: flex; gap: 6px; padding: 8px 12px 12px; }
.lib-acts .danger:hover { color: #ff7a90; border-color: rgba(255, 122, 144, 0.4); }
.lib-add {
  min-height: 210px;
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px;
  border-radius: 12px;
  border: 1px dashed rgba(255, 255, 255, 0.16);
  color: var(--text-mute); font-size: 12px;
  transition: all 0.2s var(--ease);
}
.lib-add:hover { color: var(--text); border-color: rgba(255, 145, 60, 0.5); background: rgba(255, 145, 60, 0.05); }

.loading { padding: 80px; text-align: center; color: var(--text-mute); font-size: 14px; }

/* 编辑弹窗 */
.ed-mask {
  position: fixed; inset: 0; z-index: 9000;
  display: flex; align-items: center; justify-content: center;
  padding: 24px; background: rgba(4, 4, 9, 0.72); backdrop-filter: blur(6px);
}
.ed-box {
  width: 100%; max-width: 560px; max-height: 88vh;
  display: flex; flex-direction: column;
  border-radius: 16px; background: #15151e;
  border: 1px solid var(--border-strong);
  box-shadow: 0 32px 80px -28px rgba(0, 0, 0, 0.95);
}
.ed-head { display: flex; align-items: center; justify-content: space-between; padding: 16px 18px 12px; border-bottom: 1px solid var(--border); }
.ed-head h3 { font-size: 15px; font-weight: 630; }
.ed-x { width: 26px; height: 26px; display: grid; place-items: center; border-radius: 8px; color: var(--text-mute); }
.ed-x:hover { color: var(--text); background: rgba(255, 255, 255, 0.07); }
.ed-body { padding: 16px 18px; overflow-y: auto; }
.ed-ref { display: flex; align-items: center; gap: 14px; margin-bottom: 14px; }
.ed-thumb {
  width: 132px; aspect-ratio: 16 / 10; flex-shrink: 0;
  display: grid; place-items: center; border-radius: 10px; overflow: hidden;
  background: rgba(255, 255, 255, 0.03); border: 1px solid var(--border);
  font-size: 22px; font-weight: 600; color: var(--text-mute);
}
.ed-thumb img { width: 100%; height: 100%; object-fit: contain; background: #0d0d14; }
.ed-ref-info { display: flex; flex-direction: column; align-items: flex-start; gap: 7px; }
.ed-hint { font-size: 10.5px; color: var(--text-mute); }
.ed-label { display: flex; align-items: center; gap: 6px; font-size: 11.5px; font-weight: 550; color: var(--text-dim); margin: 12px 0 7px; }
.req { color: #ff7a90; font-size: 11px; }
.opt { font-size: 9.5px; padding: 1px 5px; border-radius: 4px; color: var(--text-mute); background: rgba(255, 255, 255, 0.06); }
.ed-input, .ed-textarea {
  width: 100%; padding: 9px 11px; border-radius: 10px;
  font-size: 12.5px; color: var(--text); font-family: inherit;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--border); outline: none;
}
.ed-input { height: 38px; }
.ed-textarea { line-height: 1.7; resize: vertical; }
.ed-input:focus, .ed-textarea:focus { border-color: rgba(124, 92, 255, 0.5); }
.ed-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 18px 16px; border-top: 1px solid var(--border); }

/* 总库抽屉 */
.dr-mask { position: fixed; inset: 0; z-index: 9100; background: rgba(4, 4, 9, 0.55); backdrop-filter: blur(3px); }
.dr-box {
  position: absolute; top: 0; right: 0; bottom: 0;
  width: 380px; max-width: 92vw;
  display: flex; flex-direction: column;
  background: #12121a;
  border-left: 1px solid var(--border-strong);
  box-shadow: -24px 0 60px rgba(0, 0, 0, 0.6);
}
.dr-head { display: flex; align-items: center; justify-content: space-between; padding: 16px 16px 12px; border-bottom: 1px solid var(--border); }
.dr-head h3 { font-size: 14px; font-weight: 630; }
.dr-tabs { display: flex; gap: 4px; padding: 10px 12px; border-bottom: 1px solid var(--border); }
.dr-tab {
  flex: 1; height: 30px; border-radius: 7px;
  font-size: 12px; color: var(--text-dim);
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid transparent;
}
.dr-tab.on { color: #fff; background: rgba(255, 145, 60, 0.16); border-color: rgba(255, 145, 60, 0.35); }
.dr-body { flex: 1; overflow-y: auto; padding: 12px; display: flex; flex-direction: column; gap: 10px; }
.dr-empty { padding: 40px 12px; text-align: center; font-size: 12px; color: var(--text-mute); }
.dr-item {
  display: flex; align-items: center; gap: 10px;
  padding: 8px; border-radius: 10px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.02);
}
.dr-thumb {
  width: 46px; height: 46px; flex-shrink: 0;
  border-radius: 8px; overflow: hidden;
  display: grid; place-items: center;
  background: #0d0d14; color: var(--text-mute);
  font-size: 16px; font-weight: 600;
}
.dr-thumb img { width: 100%; height: 100%; object-fit: cover; }
.dr-main { flex: 1; min-width: 0; }
.dr-main h4 { font-size: 12.5px; font-weight: 600; }
.dr-main p {
  margin-top: 2px; font-size: 10.5px; line-height: 1.5; color: var(--text-mute);
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.dr-add {
  flex-shrink: 0; height: 26px; padding: 0 10px; border-radius: 7px;
  font-size: 11px; color: #fff;
  background: linear-gradient(135deg, #ff8a3d, #ff6a2b);
}
.dr-add.done { color: var(--text-mute); background: rgba(255, 255, 255, 0.06); cursor: default; }
</style>
