<script setup lang="ts">
const props = defineProps<{
  label: string
  modelValue: boolean
  hint?: string
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const toggle = () => {
  if (props.disabled) return
  emit('update:modelValue', !props.modelValue)
}
</script>

<template>
  <div class="pill-group" :title="hint">
    <span class="pill-label">{{ label }}</span>
    <button
      type="button"
      role="switch"
      :aria-checked="modelValue"
      :aria-label="label"
      class="switch"
      :class="{ on: modelValue }"
      :disabled="disabled"
      @click="toggle"
    >
      <span class="knob"></span>
    </button>
  </div>
</template>

<style scoped>
.pill-group {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.pill-label {
  font-size: 12.5px;
  color: var(--text-mute);
  white-space: nowrap;
}
.switch {
  position: relative;
  width: 36px;
  height: 20px;
  padding: 0;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: rgba(255, 255, 255, 0.06);
  cursor: pointer;
  transition: all 0.24s var(--ease);
}
.switch.on {
  background: var(--accent);
  border-color: transparent;
  box-shadow: 0 0 0 3px rgba(124, 92, 255, 0.16);
}
.switch:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.knob {
  position: absolute;
  top: 50%;
  left: 2px;
  width: 14px;
  height: 14px;
  margin-top: -7px;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.24s var(--ease);
}
.switch.on .knob {
  transform: translateX(16px);
}
</style>
