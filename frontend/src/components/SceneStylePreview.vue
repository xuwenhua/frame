<script setup lang="ts">
import { computed } from 'vue'

/**
 * 场景样式的效果预览示意（与角色样式预览同一套视觉语言）。
 * 三种样式的本质区别就是「画面里有几格、每格是什么视角」：
 *   单一场景视图 —— 一格平视主视角
 *   多角度视图 —— 主视角 + 俯视平面 + 局部细节
 *   广角全景 —— 一格超广角，强调纵深
 * 画幅跟随剧本：竖版剧本的单一场景是 9:16，其余固定 16:9（与后端 AspectFor 一致）。
 */

interface Panel {
  kind: 'view' | 'wide' | 'top' | 'detail'
  label: string
  x: number
  y: number
  w: number
  h: number
  wallY: number
  floorPath: string
  persp: string[]
  win: { x: number; y: number; w: number; h: number }
  table: { x: number; y: number; w: number; h: number }
  cab: { x: number; y: number; w: number; h: number }
  box: { x: number; y: number; w: number; h: number }
}

const props = defineProps<{
  style: string
  vertical?: boolean
  title?: string
  desc?: string
}>()

const H = 210
const PAD = 8
const GAP = 6
const LABEL_H = 15

const ratio = computed(() => {
  if (props.style === 'single') return props.vertical ? 9 / 16 : 4 / 3
  return 16 / 9
})
const W = computed(() => Math.round(H * ratio.value))

/** 房间示意图的几何（矩形构件 + 透视线，架构图用非等比坐标即可） */
function room(x: number, y: number, w: number, h: number, strong: boolean) {
  const wallY = y + h * (strong ? 0.38 : 0.46)
  const vpx = x + w * 0.5
  const floorPath = `M${x} ${wallY} L${x + w} ${wallY} L${x + w} ${y + h} L${x} ${y + h} Z`
  const persp = [
    `M${x} ${y + h} L${vpx} ${wallY}`,
    `M${x + w * 0.42} ${y + h} L${vpx} ${wallY}`,
    `M${x + w * 1.0} ${y + h} L${vpx} ${wallY}`
  ]
  return {
    wallY,
    floorPath,
    persp,
    win: { x: x + w * 0.6, y: y + h * 0.07, w: w * (strong ? 0.34 : 0.28), h: h * 0.24 },
    table: { x: x + w * 0.2, y: wallY + h * 0.12, w: w * (strong ? 0.44 : 0.34), h: h * 0.13 },
    cab: { x: x + w * 0.05, y: wallY - h * 0.2, w: w * 0.13, h: h * 0.22 },
    box: { x, y, w, h }
  }
}

const panels = computed<Panel[]>(() => {
  const innerW = W.value - PAD * 2
  const innerH = H - PAD * 2

  if (props.style === 'multi') {
    const mainW = (innerW - GAP) * 0.6
    const sideW = innerW - GAP - mainW
    const cellH = (innerH - GAP) / 2
    const mainH = innerH - LABEL_H
    return [
      { kind: 'view', label: '主视角', x: PAD, y: PAD, w: mainW, h: mainH, ...room(PAD, PAD, mainW, mainH, false) },
      {
        kind: 'top',
        label: '俯视',
        x: PAD + mainW + GAP,
        y: PAD,
        w: sideW,
        h: cellH - LABEL_H,
        ...room(PAD + mainW + GAP, PAD, sideW, cellH - LABEL_H, false)
      },
      {
        kind: 'detail',
        label: '局部细节',
        x: PAD + mainW + GAP,
        y: PAD + cellH + GAP,
        w: sideW,
        h: cellH - LABEL_H,
        ...room(PAD + mainW + GAP, PAD + cellH + GAP, sideW, cellH - LABEL_H, false)
      }
    ]
  }

  // 场景概念图：主视框缩小居中，四角留给陈设细节
  if (props.style === 'concept') {
    const cx = PAD + 46
    const cy = PAD + 22
    const cw = innerW - 92
    const ch = innerH - LABEL_H - 44
    return [{ kind: 'view', label: '', x: cx, y: cy, w: cw, h: ch, ...room(cx, cy, cw, ch, false) }]
  }

  const kind: Panel['kind'] = props.style === 'wide' ? 'wide' : 'view'
  const label = kind === 'wide' ? '超广角全景' : '一个平视主视角'
  const h = innerH - LABEL_H
  return [{ kind, label, x: PAD, y: PAD, w: innerW, h, ...room(PAD, PAD, innerW, h, kind === 'wide') }]
})
</script>

<template>
  <div class="ss">
    <div class="ss-canvas">
      <span class="ss-tag">示意</span>
      <svg :viewBox="`0 0 ${W} ${H}`" class="ss-svg" role="img" :aria-label="title">
        <g v-for="(p, i) in panels" :key="i">
          <rect
            :x="p.x"
            :y="p.y"
            :width="p.w"
            :height="p.h"
            rx="5"
            fill="rgba(255,255,255,0.035)"
            stroke="rgba(255,255,255,0.10)"
            stroke-width="0.6"
          />

          <!-- ===== 平视房间 / 超广角 ===== -->
          <template v-if="p.kind === 'view' || p.kind === 'wide'">
            <!-- 后墙 -->
            <rect :x="p.x" :y="p.y" :width="p.w" :height="p.wallY - p.y" fill="rgba(34,211,238,0.10)" />
            <!-- 地面 -->
            <path :d="p.floorPath" fill="rgba(34,211,238,0.20)" />
            <!-- 透视线 -->
            <path
              v-for="(d, k) in p.persp"
              :key="'p' + k"
              :d="d"
              stroke="rgba(34,211,238,0.38)"
              stroke-width="0.7"
              fill="none"
            />
            <!-- 窗 -->
            <rect
              :x="p.win.x"
              :y="p.win.y"
              :width="p.win.w"
              :height="p.win.h"
              rx="1.5"
              fill="rgba(34,211,238,0.22)"
              stroke="rgba(34,211,238,0.55)"
              stroke-width="0.8"
            />
            <!-- 柜 -->
            <rect
              :x="p.cab.x"
              :y="p.cab.y"
              :width="p.cab.w"
              :height="p.cab.h"
              rx="1.5"
              fill="rgba(34,211,238,0.28)"
            />
            <!-- 桌 -->
            <rect
              :x="p.table.x"
              :y="p.table.y"
              :width="p.table.w"
              :height="p.table.h"
              rx="2"
              fill="rgba(34,211,238,0.42)"
            />
            <line
              :x1="p.x"
              :y1="p.wallY"
              :x2="p.x + p.w"
              :y2="p.wallY"
              stroke="rgba(34,211,238,0.7)"
              stroke-width="0.9"
            />
            <!-- 超广角：额外的纵深层次 -->
            <template v-if="p.kind === 'wide'">
              <rect
                :x="p.x + p.w * 0.06"
                :y="p.wallY + (p.y + p.h - p.wallY) * 0.28"
                :width="p.w * 0.88"
                :height="(p.y + p.h - p.wallY) * 0.16"
                rx="1.5"
                fill="none"
                stroke="rgba(34,211,238,0.3)"
                stroke-width="0.7"
                stroke-dasharray="2.5 2"
              />
              <path
                :d="`M${p.x + p.w * 0.5} ${p.wallY} L${p.x + p.w * 0.5} ${p.y + p.h}`"
                stroke="rgba(34,211,238,0.28)"
                stroke-width="0.7"
                stroke-dasharray="3 2.5"
              />
            </template>
          </template>

          <!-- ===== 俯视平面图 ===== -->
          <template v-else-if="p.kind === 'top'">
            <rect
              :x="p.x + p.w * 0.12"
              :y="p.y + p.h * 0.14"
              :width="p.w * 0.76"
              :height="p.h * 0.72"
              rx="1.5"
              fill="rgba(34,211,238,0.10)"
              stroke="rgba(34,211,238,0.6)"
              stroke-width="0.8"
            />
            <rect
              :x="p.x + p.w * 0.26"
              :y="p.y + p.h * 0.36"
              :width="p.w * 0.34"
              :height="p.h * 0.24"
              rx="1"
              fill="rgba(34,211,238,0.4)"
            />
            <rect
              :x="p.x + p.w * 0.68"
              :y="p.y + p.h * 0.2"
              :width="p.w * 0.12"
              :height="p.h * 0.3"
              rx="1"
              fill="rgba(34,211,238,0.28)"
            />
          </template>

          <!-- ===== 局部细节放大 ===== -->
          <template v-else>
            <rect
              :x="p.x + p.w * 0.16"
              :y="p.y + p.h * 0.16"
              :width="p.w * 0.68"
              :height="p.h * 0.68"
              rx="2"
              fill="none"
              stroke="rgba(34,211,238,0.5)"
              stroke-width="0.8"
              stroke-dasharray="2.5 2"
            />
            <path
              :d="`M${p.x + p.w * 0.26} ${p.y + p.h * 0.66} q${p.w * 0.16} ${-p.h * 0.34} ${p.w * 0.24} 0 q${p.w * 0.12} ${p.h * 0.2} ${p.w * 0.24} ${-p.h * 0.06}`"
              fill="none"
              stroke="rgba(34,211,238,0.8)"
              stroke-width="1.1"
              stroke-linecap="round"
            />
            <circle :cx="p.x + p.w * 0.34" :cy="p.y + p.h * 0.34" r="3" fill="rgba(34,211,238,0.55)" />
          </template>

          <text
            :x="p.x + p.w / 2"
            :y="p.y + p.h + LABEL_H - 3"
            font-size="10.5"
            fill="rgba(255,255,255,0.66)"
            text-anchor="middle"
          >
            {{ p.label }}
          </text>
        </g>

        <!-- 场景概念图：四角补陈设/材质/光源的细节放大 -->
        <g v-if="style === 'concept'">
          <g
            v-for="(d, i) in [
              [26, 34],
              [W - 26, 34],
              [26, H - 46],
              [W - 26, H - 46]
            ]"
            :key="'d' + i"
          >
            <rect
              :x="d[0] - 21"
              :y="d[1] - 21"
              width="42"
              height="42"
              rx="5"
              fill="rgba(34,211,238,0.10)"
              stroke="rgba(34,211,238,0.42)"
              stroke-width="0.7"
            />
            <!-- 木纹 -->
            <g v-if="i === 0" stroke="rgba(34,211,238,0.85)" stroke-width="1.1" fill="none" stroke-linecap="round">
              <path :d="`M${d[0] - 10} ${d[1] - 8} q9 4 19 0`" />
              <path :d="`M${d[0] - 10} ${d[1]} q9 4 19 0`" />
              <path :d="`M${d[0] - 10} ${d[1] + 8} q9 4 19 0`" />
            </g>
            <!-- 织物 / 布褶 -->
            <g v-else-if="i === 1" stroke="rgba(34,211,238,0.85)" stroke-width="1.2" fill="none" stroke-linecap="round">
              <path :d="`M${d[0] - 9} ${d[1] - 9} q4 9 0 18`" />
              <path :d="`M${d[0] - 1} ${d[1] - 11} q5 11 0 22`" />
              <path :d="`M${d[0] + 7} ${d[1] - 9} q4 9 0 18`" />
            </g>
            <!-- 灯具 / 光源 -->
            <g v-else-if="i === 2" opacity="0.9">
              <path
                :d="`M${d[0] - 7} ${d[1] - 9} h14 l-3 6 h-8 Z`"
                fill="none"
                stroke="rgba(34,211,238,0.85)"
                stroke-width="1.1"
              />
              <path
                :d="`M${d[0] - 9} ${d[1] + 9} q9 -11 18 0`"
                fill="none"
                stroke="rgba(34,211,238,0.5)"
                stroke-width="1"
                stroke-dasharray="2 2"
              />
            </g>
            <!-- 陈设轮廓 -->
            <g v-else stroke="rgba(34,211,238,0.85)" stroke-width="1.1" fill="none">
              <rect :x="d[0] - 9" :y="d[1] - 8" width="18" height="12" rx="1.5" />
              <path :d="`M${d[0] - 9} ${d[1] + 4} v6 M${d[0] + 9} ${d[1] + 4} v6`" />
            </g>
          </g>
        </g>
      </svg>
    </div>

    <div class="ss-meta">
      <strong>{{ title }}</strong>
      <span>{{ desc }}</span>
    </div>
  </div>
</template>

<style scoped>
.ss {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ss-canvas {
  position: relative;
  width: 100%;
  height: 200px;
  border-radius: 9px;
  background: radial-gradient(120% 120% at 50% 0%, rgba(34, 211, 238, 0.1), rgba(0, 0, 0, 0) 70%),
    #0f0f17;
  border: 1px solid var(--border);
  padding: 4px;
  box-sizing: border-box;
  overflow: hidden;
}
.ss-tag {
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
.ss-svg {
  display: block;
  width: 100%;
  height: 100%;
}
.ss-meta {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 0 2px;
}
.ss-meta strong {
  font-size: 12px;
  font-weight: 600;
  color: var(--text);
}
.ss-meta span {
  font-size: 10.5px;
  line-height: 1.55;
  color: var(--text-mute);
}
</style>
