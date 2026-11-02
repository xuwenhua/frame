import io

p = r'D:\工作\frame\frontend\src\views\ProjectDetail.vue'
s = io.open(p, encoding='utf-8').read()

# ---------- 1. 状态 + 函数（插在 regenAssetImage 之后） ----------
anchor = '''// ---------- 资产：编辑 / 删除 / 大图 ----------'''
assert anchor in s, '锚点1'

fn = '''// ---------- 场景全景：柱形全景图 → 360 视频 ----------
// 全景图的用途是让模型看清「房间其余角度长什么样」，
// 单张场景图只有正前方，过肩/反打镜头全靠模型编，环境和家具就会变。
const panoramaBusy = ref<Record<string, boolean>>({})
const panoramaVideoBusy = ref<Record<string, boolean>>({})

/** 图片编辑模型：优先选 qwen-image-2.1-pe（图生图），没有就退默认图片模型 */
const editModelId = () => {
  const m =
    imageModels.value.find((x: any) => String(x.modelId || '').includes('qwen-image-2.1-pe')) ||
    imageModels.value.find((x: any) => x.isDefault && x.hasKey !== false) ||
    imageModels.value.find((x: any) => x.hasKey !== false)
  return m?.id || ''
}

/** 全景视频工作流：模型配置里 model_id = panorama-to-360video 的那条 */
const panoramaVideoModelId = () =>
  videoModels.value.find((x: any) => x.modelId === 'panorama-to-360video')?.id || ''

const makePanorama = async (a: any) => {
  if (panoramaBusy.value[a.id]) return
  if (!a.referenceImage) return toast.warning('先生成场景图', '柱形全景图是在场景图基础上往外扩展的')
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

// ---------- 资产：编辑 / 删除 / 大图 ----------'''

s = s.replace(anchor, fn, 1)

# ---------- 2. 模板按钮（插在「重新生成」按钮之前，仅场景显示） ----------
btn_anchor = '''                    <button class="regen-main" @click="regenAssetImage(sec.type, a)">'''
assert btn_anchor in s, '锚点2'

btns = '''                    <template v-if="sec.type === 'scene'">
                      <button
                        class="regen-main"
                        :disabled="panoramaBusy[a.id]"
                        :title="a.panoramaImage ? '重做柱形全景图' : '把场景图扩展成 360 度柱形全景图'"
                        @click="makePanorama(a)"
                      >
                        {{ panoramaBusy[a.id] ? '生成中…' : a.panoramaImage ? '重做全景图' : '生成全景图' }}
                      </button>
                      <button
                        v-if="a.panoramaImage"
                        class="regen-main"
                        :disabled="panoramaVideoBusy[a.id]"
                        :title="a.panoramaVideo ? '重做 360 全景视频' : '把柱形全景图转成 360 全景视频'"
                        @click="makePanoramaVideo(a)"
                      >
                        {{ panoramaVideoBusy[a.id] ? '生成中…' : a.panoramaVideo ? '重做 360' : '生成 360 视频' }}
                      </button>
                    </template>
                    <button class="regen-main" @click="regenAssetImage(sec.type, a)">'''

s = s.replace(btn_anchor, btns, 1)

io.open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('前端已修改')
