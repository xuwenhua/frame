<script setup lang="ts">
defineProps<{
  title: string
  desc: string
}>()
</script>

<template>
  <div class="ph">
    <div class="ph-art">
      <svg viewBox="0 0 24 24" fill="none" width="30" height="30">
        <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.6" />
        <path d="M12 8v5" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
        <circle cx="12" cy="16.2" r="1" fill="currentColor" />
      </svg>
    </div>
    <h1>{{ title }}</h1>
    <p>{{ desc }}</p>
    <span class="badge">开发中</span>
  </div>
</template>

<style scoped>
.ph {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 130px 20px;
  text-align: center;
}
.ph-art {
  width: 70px;
  height: 70px;
  border-radius: 21px;
  display: grid;
  place-items: center;
  color: var(--text-mute);
  background: var(--surface);
  border: 1px solid var(--border);
  margin-bottom: 22px;
}
.ph h1 {
  font-size: 26px;
  font-weight: 680;
  letter-spacing: -0.03em;
}
.ph p {
  font-size: 13.5px;
  color: var(--text-mute);
  margin: 7px 0 20px;
}
</style>
