<script setup lang="ts">
import { computed } from 'vue'

/**
 * 角色样式的效果预览示意。
 * 竞品在这里放的是真实示例图，我们没有素材，改用等比示意图表达「怎么裁切/分格」，
 * 这恰恰是四种样式最本质的区别（正面无头 / 背面全身 / 正面胸像 / 四格 / 面部分格）。
 */

type Kind =
  | 'nohead'
  | 'front'
  | 'back'
  | 'bust'
  | 'faceFront'
  | 'faceSide'
  | 'faceQuarter'

interface Panel {
  kind: Kind
  label: string
}

interface Preset {
  ratio: number
  cols: number
  rows: number
  panels: Panel[]
}

const props = defineProps<{ style: string; title?: string; desc?: string }>()

const PRESETS: Record<string, Preset> = {
  asset: {
    ratio: 16 / 9,
    cols: 3,
    rows: 1,
    panels: [
      { kind: 'nohead', label: '正面无头全身' },
      { kind: 'back', label: '背面全身' },
      { kind: 'bust', label: '正面胸像' }
    ]
  },
  quad: {
    ratio: 3 / 4,
    cols: 2,
    rows: 2,
    panels: [
      { kind: 'faceFront', label: '正面特写' },
      { kind: 'faceSide', label: '侧面特写' },
      { kind: 'front', label: '正面服装' },
      { kind: 'back', label: '背面全身' }
    ]
  },
  concept: {
    ratio: 16 / 9,
    cols: 1,
    rows: 1,
    panels: [{ kind: 'front', label: '整体设计 · 四周服饰细节' }]
  },
  closeup: {
    ratio: 16 / 9,
    cols: 3,
    rows: 1,
    panels: [
      { kind: 'faceFront', label: '正脸' },
      { kind: 'faceQuarter', label: '四分之三侧' },
      { kind: 'faceSide', label: '正侧面' }
    ]
  }
}

const preset = computed<Preset>(() => PRESETS[props.style] || PRESETS.asset)

const H = 210
const PAD = 8
const GAP = 7
const LABEL_H = 16

const W = computed(() => Math.round(H * preset.value.ratio))

const boxes = computed(() => {
  const p = preset.value
  const innerW = W.value - PAD * 2 - GAP * (p.cols - 1)
  const innerH = H - PAD * 2 - GAP * (p.rows - 1)
  const bw = innerW / p.cols
  // 每行的标签空间要在行内预留，否则多行时标签会压到下一行的面板上
  const cellH = innerH / p.rows
  const bh = cellH - LABEL_H
  return p.panels.map((panel, i) => {
    const col = i % p.cols
    const row = Math.floor(i / p.cols)
    const x = PAD + col * (bw + GAP)
    const y = PAD + row * (cellH + GAP)
    // 人物基础坐标：40 宽 × 100 高，脚在 y=100
    const scale = Math.min((bw * 0.84) / 40, (bh * 0.86) / 100)
    return {
      ...panel,
      x,
      y,
      w: bw,
      h: bh,
      scale,
      cx: x + bw / 2,
      fy: y + bh * 0.9 - 100 * scale
    }
  })
})
</script>

<template>
  <div class="cs">
    <div class="cs-canvas">
      <span class="cs-tag">示意</span>
      <svg
        :viewBox="`0 0 ${W} ${H}`"
        class="cs-svg"
        role="img"
        :aria-label="title"
      >
        <g v-for="(b, i) in boxes" :key="i">
          <!-- 每格底 -->
          <rect
            :x="b.x"
            :y="b.y"
            :width="b.w"
            :height="b.h"
            rx="5"
            fill="rgba(255,255,255,0.035)"
            stroke="rgba(255,255,255,0.10)"
            stroke-width="0.6"
          />

          <!-- ============ 正面全身（含头） ============ -->
          <g v-if="b.kind === 'front'" :transform="`translate(${b.cx},${b.fy}) scale(${b.scale})`">
            <circle cx="0" cy="12.5" r="8.4" fill="#9a8bff" />
            <path d="M-8.4 11 Q-8.4 3 0 3 Q8.4 3 8.4 11 L8.4 15 Q4 11 0 11 Q-4 11 -8.4 15 Z" fill="#6a4ce0" opacity="0.85" />
            <rect x="-3" y="19" width="6" height="5.5" rx="2" fill="#9a8bff" opacity="0.85" />
            <rect x="-16.6" y="25" width="5.8" height="27.5" rx="2.9" fill="#7c5cff" opacity="0.72" />
            <rect x="10.8" y="25" width="5.8" height="27.5" rx="2.9" fill="#7c5cff" opacity="0.72" />
            <path d="M-10 24 Q-10 22 -7 22 L7 22 Q10 22 10 24 L12 40 L10.5 58 L-10.5 58 L-12 40 Z" fill="#7c5cff" />
            <rect x="-9.7" y="57" width="7.4" height="39" rx="3.2" fill="#6a4ce0" opacity="0.92" />
            <rect x="2.3" y="57" width="7.4" height="39" rx="3.2" fill="#6a4ce0" opacity="0.92" />
            <rect x="-10.8" y="95" width="9" height="5" rx="2.2" fill="#5b40c4" />
            <rect x="1.8" y="95" width="9" height="5" rx="2.2" fill="#5b40c4" />
          </g>

          <!-- ============ 正面无头（头部裁切） ============ -->
          <g v-if="b.kind === 'nohead'" :transform="`translate(${b.cx},${b.fy}) scale(${b.scale})`">
            <rect x="-16.6" y="25" width="5.8" height="27.5" rx="2.9" fill="#7c5cff" opacity="0.72" />
            <rect x="10.8" y="25" width="5.8" height="27.5" rx="2.9" fill="#7c5cff" opacity="0.72" />
            <path d="M-10 24 Q-10 22 -7 22 L7 22 Q10 22 10 24 L12 40 L10.5 58 L-10.5 58 L-12 40 Z" fill="#7c5cff" />
            <rect x="-9.7" y="57" width="7.4" height="39" rx="3.2" fill="#6a4ce0" opacity="0.92" />
            <rect x="2.3" y="57" width="7.4" height="39" rx="3.2" fill="#6a4ce0" opacity="0.92" />
            <rect x="-10.8" y="95" width="9" height="5" rx="2.2" fill="#5b40c4" />
            <rect x="1.8" y="95" width="9" height="5" rx="2.2" fill="#5b40c4" />
            <!-- 裁切线：紧贴肩线上方 -->
            <line x1="-21" y1="21" x2="21" y2="21" stroke="#ff8fa3" stroke-width="1.5" stroke-dasharray="2.8 2" />
            <text x="0" y="14" font-size="9.5" fill="#ff8fa3" text-anchor="middle">裁切</text>
          </g>

          <!-- ============ 背面全身（可见后脑发型） ============ -->
          <g v-if="b.kind === 'back'" :transform="`translate(${b.cx},${b.fy}) scale(${b.scale})`">
            <!-- 后脑发型 + 垂到肩的发尾 -->
            <path d="M-8.8 12 Q-8.8 2.6 0 2.6 Q8.8 2.6 8.8 12 L9.6 27 Q6 24.5 0 24.5 Q-6 24.5 -9.6 27 Z" fill="#9a8bff" />
            <rect x="-3" y="19" width="6" height="5.5" rx="2" fill="#9a8bff" opacity="0.55" />
            <rect x="-16.6" y="25" width="5.8" height="27.5" rx="2.9" fill="#6f52f2" opacity="0.72" />
            <rect x="10.8" y="25" width="5.8" height="27.5" rx="2.9" fill="#6f52f2" opacity="0.72" />
            <path d="M-10 24 Q-10 22 -7 22 L7 22 Q10 22 10 24 L12 40 L10.5 58 L-10.5 58 L-12 40 Z" fill="#6f52f2" />
            <path d="M0 25 L0 57" stroke="rgba(255,255,255,0.28)" stroke-width="1.1" />
            <rect x="-9.7" y="57" width="7.4" height="39" rx="3.2" fill="#5b40c4" opacity="0.92" />
            <rect x="2.3" y="57" width="7.4" height="39" rx="3.2" fill="#5b40c4" opacity="0.92" />
            <rect x="-10.8" y="95" width="9" height="5" rx="2.2" fill="#4e34ae" />
            <rect x="1.8" y="95" width="9" height="5" rx="2.2" fill="#4e34ae" />
          </g>

          <!-- ============ 正面胸像（头肩至胸） ============ -->
          <g v-if="b.kind === 'bust'" :transform="`translate(${b.cx},${b.fy}) scale(${b.scale})`">
            <ellipse cx="0" cy="30" rx="14" ry="16.5" fill="#6a4ce0" opacity="0.75" />
            <ellipse cx="0" cy="32" rx="11.4" ry="13.4" fill="#a294ff" />
            <path d="M-11.4 24.5 Q0 16 11.4 24.5 L11.4 29 Q0 21.5 -11.4 29 Z" fill="#6a4ce0" opacity="0.9" />
            <rect x="-4.6" y="43" width="9.2" height="9" rx="2.4" fill="#a294ff" opacity="0.9" />
            <path d="M-25 100 L-21.5 68 Q0 55 21.5 68 L25 100 Z" fill="#7c5cff" />
            <path d="M-9.5 64 Q0 73 9.5 64" stroke="rgba(255,255,255,0.28)" stroke-width="1.2" fill="none" />
            <circle cx="-4.4" cy="32" r="1.5" fill="#241844" />
            <circle cx="4.4" cy="32" r="1.5" fill="#241844" />
            <path d="M-2.8 39 q2.8 2 5.6 0" stroke="#241844" stroke-width="1.1" fill="none" opacity="0.8" />
          </g>

          <!-- ============ 正面脸部特写 ============ -->
          <g v-if="b.kind === 'faceFront'" :transform="`translate(${b.cx},${b.fy}) scale(${b.scale})`">
            <ellipse cx="0" cy="52" rx="19" ry="22" fill="#6a4ce0" opacity="0.75" />
            <ellipse cx="0" cy="53.5" rx="14.4" ry="17.6" fill="#a294ff" />
            <path d="M-14.4 43 Q0 33.5 14.4 43 L14.4 47.5 Q0 39.5 -14.4 47.5 Z" fill="#6a4ce0" opacity="0.9" />
            <path d="M-9.4 52 q4.2 -2.6 7.4 0" stroke="#241844" stroke-width="1.4" fill="none" stroke-linecap="round" />
            <path d="M2 52 q4.2 -2.6 7.4 0" stroke="#241844" stroke-width="1.4" fill="none" stroke-linecap="round" />
            <circle cx="-5.6" cy="56.4" r="1.8" fill="#241844" />
            <circle cx="5.6" cy="56.4" r="1.8" fill="#241844" />
            <path d="M-0.8 58.4 v3" stroke="#241844" stroke-width="1" opacity="0.45" stroke-linecap="round" />
            <path d="M-3.8 66 q3.8 2.8 7.6 0" stroke="#241844" stroke-width="1.3" fill="none" stroke-linecap="round" />
            <path d="M-24 100 L-20.5 80 Q0 69 20.5 80 L24 100 Z" fill="#7c5cff" opacity="0.92" />
          </g>

          <!-- ============ 四分之三侧脸 ============ -->
          <g v-if="b.kind === 'faceQuarter'" :transform="`translate(${b.cx},${b.fy}) scale(${b.scale})`">
            <ellipse cx="-2" cy="52" rx="19" ry="22" fill="#6a4ce0" opacity="0.75" />
            <ellipse cx="2.2" cy="53.5" rx="13.4" ry="17.4" fill="#a294ff" />
            <path d="M-11.2 43 Q2 34 15.6 44 L15.6 48.5 Q2 40.5 -11.2 47.5 Z" fill="#6a4ce0" opacity="0.9" />
            <path d="M12.6 54 L19 59.5 L12.6 62 Z" fill="#a294ff" />
            <path d="M-5.6 52 q4 -2.4 7 0" stroke="#241844" stroke-width="1.4" fill="none" stroke-linecap="round" />
            <path d="M6.4 52.4 q3.6 -2.2 6.2 0" stroke="#241844" stroke-width="1.3" fill="none" stroke-linecap="round" />
            <circle cx="-1.6" cy="56.6" r="1.7" fill="#241844" />
            <circle cx="9.4" cy="56.6" r="1.5" fill="#241844" />
            <path d="M4.4 66.4 q3.8 2.6 7.4 -0.4" stroke="#241844" stroke-width="1.25" fill="none" stroke-linecap="round" />
            <path d="M-24 100 L-20.5 80 Q2 69 22.5 80 L26 100 Z" fill="#7c5cff" opacity="0.92" />
          </g>

          <!-- ============ 正侧面脸 ============ -->
          <g v-if="b.kind === 'faceSide'" :transform="`translate(${b.cx},${b.fy}) scale(${b.scale})`">
            <ellipse cx="-4" cy="52" rx="17" ry="21.5" fill="#6a4ce0" opacity="0.75" />
            <path d="M-13 40 Q0 33 10 40 Q15 50 14 58 Q10 71 0 73 Q-12 72 -14.5 58 Q-16 47 -13 40 Z" fill="#a294ff" />
            <path d="M13.6 52 L19.6 59 L13.6 62.4 Z" fill="#a294ff" />
            <path d="M-13 38 Q2 30 11 40 L11 46 Q2 38 -13 44 Z" fill="#6a4ce0" opacity="0.9" />
            <path d="M-10 34 Q4 27 12 36 L13 30 Q4 24 -9 28 Z" fill="#6a4ce0" opacity="0.6" />
            <circle cx="5.2" cy="54" r="1.6" fill="#241844" />
            <path d="M7 65 q4 0.6 6 -2" stroke="#241844" stroke-width="1.2" fill="none" stroke-linecap="round" />
            <path d="M-23 100 L-19 79 Q2 68 22 79 L25 100 Z" fill="#7c5cff" opacity="0.92" />
          </g>
        </g>

        <!-- 概念图：四周服饰细节放大（布褶 / 盘扣 / 刺绣 / 滚边） -->
        <g v-if="style === 'concept'">
          <g v-for="(d, i) in [[30, 46], [W - 30, 46], [30, 146], [W - 30, 146]]" :key="'d' + i">
            <rect
              :x="d[0] - 23"
              :y="d[1] - 23"
              width="46"
              height="46"
              rx="6"
              fill="rgba(124,92,255,0.10)"
              stroke="rgba(124,92,255,0.35)"
              stroke-width="0.7"
            />
            <!-- 布褶 -->
            <g v-if="i === 0" stroke="#9a8bff" stroke-width="1.3" fill="none" opacity="0.85" stroke-linecap="round">
              <path :d="`M${d[0] - 11} ${d[1] - 10} q5 10 0 20`" />
              <path :d="`M${d[0] - 3} ${d[1] - 12} q6 12 0 24`" />
              <path :d="`M${d[0] + 5} ${d[1] - 10} q5 10 0 20`" />
              <path :d="`M${d[0] + 12} ${d[1] - 7} q4 8 0 16`" />
            </g>
            <!-- 盘扣 -->
            <g v-else-if="i === 1" opacity="0.9">
              <circle :cx="d[0]" :cy="d[1] - 4" r="5" fill="none" stroke="#9a8bff" stroke-width="1.3" />
              <circle :cx="d[0]" :cy="d[1] - 4" r="2" fill="#9a8bff" />
              <path
                :d="`M${d[0]} ${d[1] + 1} q-7 5 0 10 q7 -5 0 -10`"
                fill="none"
                stroke="#9a8bff"
                stroke-width="1.2"
              />
            </g>
            <!-- 刺绣纹样 -->
            <g v-else-if="i === 2" opacity="0.85">
              <path
                :d="`M${d[0]} ${d[1] - 11} q9 11 0 22 q-9 -11 0 -22`"
                fill="none"
                stroke="#9a8bff"
                stroke-width="1.2"
              />
              <path
                :d="`M${d[0] - 8} ${d[1] - 5} q8 5 16 0 M${d[0] - 8} ${d[1] + 5} q8 -5 16 0`"
                fill="none"
                stroke="#9a8bff"
                stroke-width="1"
                opacity="0.8"
              />
            </g>
            <!-- 滚边 -->
            <g v-else opacity="0.85">
              <path
                :d="`M${d[0] - 12} ${d[1] - 7} q12 8 24 0 M${d[0] - 12} ${d[1] + 3} q12 8 24 0`"
                fill="none"
                stroke="#9a8bff"
                stroke-width="1.2"
              />
              <circle :cx="d[0] - 12" :cy="d[1] - 7" r="1.5" fill="#9a8bff" />
              <circle :cx="d[0] + 12" :cy="d[1] + 3" r="1.5" fill="#9a8bff" />
            </g>
          </g>
        </g>

        <!-- 每格标题 -->
        <text
          v-for="(b, i) in boxes"
          :key="'t' + i"
          :x="b.cx"
          :y="b.y + b.h + LABEL_H - 3"
          font-size="11"
          fill="rgba(255,255,255,0.66)"
          text-anchor="middle"
        >
          {{ b.label }}
        </text>
      </svg>
    </div>

    <div class="cs-meta">
      <strong>{{ title }}</strong>
      <span>{{ desc }}</span>
    </div>
  </div>
</template>

<style scoped>
.cs {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.cs-canvas {
  position: relative;
  /* 固定画布高度：四种样式的画幅不同，但卡片高度必须一致，否则切换时下拉会跳 */
  width: 100%;
  height: 200px;
  border-radius: 9px;
  background: radial-gradient(120% 120% at 50% 0%, rgba(124, 92, 255, 0.12), rgba(0, 0, 0, 0) 70%),
    #0f0f17;
  border: 1px solid var(--border);
  padding: 4px;
  box-sizing: border-box;
  overflow: hidden;
}
.cs-tag {
  position: absolute;
  top: 7px;
  right: 8px;
  z-index: 2;
  padding: 1px 6px;
  border-radius: 5px;
  font-size: 9px;
  color: rgba(255, 255, 255, 0.7);
  background: rgba(0, 0, 0, 0.45);
  border: 1px solid rgba(255, 255, 255, 0.14);
}
.cs-svg {
  display: block;
  width: 100%;
  height: 100%;
}
.cs-meta {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 0 2px;
}
.cs-meta strong {
  font-size: 12px;
  font-weight: 600;
  color: var(--text);
}
.cs-meta span {
  font-size: 10.5px;
  line-height: 1.55;
  color: var(--text-mute);
}
</style>
