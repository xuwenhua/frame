/**
 * 任务状态合并
 *
 * 背景（这是一个真实踩过的坑）：
 * `/api/tasks?episodeId=x` 返回该集最近 50 条任务，按 created_at 倒序。
 * 同一个资产通常会有多条历史（失败、重试、成功各一条）。
 *
 * 早期实现是逐条 `taskMap[assetId] = {...}`，后写入的覆盖先写入的 ——
 * 于是最旧那条的 `failed` 盖掉了最新那条的 `running`，
 * 卡片上的「生成中」遮罩凭空消失，看起来像「等待状态停了」。
 *
 * 这里按「每个资产只认最新一条」合并，并返回仍在进行中的数量。
 */

export interface TaskLike {
  assetId?: string
  storyboardId?: string
  status: string
  progress?: number
  resultUrl?: string
  errorMsg?: string
  /** 上游任务 ID：有它就说明真的提交出去了，只是还在跑，不能当前端"卡住"处理 */
  externalTaskId?: string
}

export interface TaskState {
  status: string
  progress: number
  url?: string
  errorMsg?: string
  externalTaskId?: string
}

export const isTaskRunning = (t?: TaskState) => !!t && (t.status === 'pending' || t.status === 'running')

/**
 * @param list  接口返回的任务列表（约定：按 created_at 倒序，最新在前）
 * @returns     合并后的 map 与仍在进行中的任务数
 */
export function mergeLatestTasks(list: TaskLike[]): {
  map: Record<string, TaskState>
  pending: number
} {
  const map: Record<string, TaskState> = {}
  const seen = new Set<string>()
  let pending = 0

  for (const t of list) {
    const key = t.assetId || t.storyboardId
    if (!key) continue
    // 已按时间倒序，第一条就是该资产的最新任务，后面的历史一律忽略
    if (seen.has(key)) continue
    seen.add(key)

    map[key] = {
      status: t.status,
      progress: t.progress ?? 0,
      url: t.resultUrl,
      errorMsg: t.errorMsg || '',
      externalTaskId: t.externalTaskId || ''
    }
    if (t.status === 'pending' || t.status === 'running') pending++
  }

  return { map, pending }
}
