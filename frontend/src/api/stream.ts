/**
 * SSE 流式请求工具。
 *
 * 为什么不用 EventSource：它只能发 GET、也不能带 Authorization 头
 * （token 塞 query 里会被日志和 referer 带出去）。这里用 fetch 读流、
 * 自己按 `\n\n` 切事件 —— 和后端 `data: {...}\n\n` 的格式对应。
 */
export async function streamPOST<T = any>(
  path: string,
  body: unknown,
  onEvent: (ev: T) => void,
  signal?: AbortSignal
): Promise<void> {
  const token = localStorage.getItem('token')
  const res = await fetch(`/api${path}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {})
    },
    body: JSON.stringify(body),
    signal
  })

  if (!res.ok || !res.body) {
    let detail = ''
    try {
      detail = await res.text()
    } catch {
      /* ignore */
    }
    throw new Error(detail || `请求失败（HTTP ${res.status}）`)
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    // 事件以空行分隔；一次 read 可能拿到半个事件，所以要留着 buffer
    let idx = buffer.indexOf('\n\n')
    while (idx >= 0) {
      const chunk = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)
      for (const line of chunk.split('\n')) {
        if (!line.startsWith('data:')) continue
        const raw = line.slice(5).trim()
        if (!raw) continue
        try {
          onEvent(JSON.parse(raw) as T)
        } catch {
          /* 半截 JSON 直接跳过，不影响后续事件 */
        }
      }
      idx = buffer.indexOf('\n\n')
    }
  }
}
