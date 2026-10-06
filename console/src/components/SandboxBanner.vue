<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { api } from '../api';

const props = defineProps<{ expiresAt: string }>();
const emit = defineEmits<{ reset: []; ended: [] }>();
const now = ref(Date.now());
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  timer = setInterval(() => {
    now.value = Date.now();
    if (ended.value) emit('ended');
  }, 15_000);
});
onUnmounted(() => clearInterval(timer));

const ended = computed(() => new Date(props.expiresAt).getTime() <= now.value);
const left = computed(() => {
  const m = Math.max(1, Math.round((new Date(props.expiresAt).getTime() - now.value) / 60_000));
  return m >= 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m} min`;
});

const busy = ref(false);
const error = ref('');
async function reset() {
  if (!confirm('Put the sandbox back to how it started? Your monitors and incidents here are replaced by the three samples.')) return;
  busy.value = true;
  error.value = '';
  try {
    await api.resetSandbox();
    emit('reset');
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not reset.';
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="sandbox-band" role="note">
    <div class="wrap">
      <span class="tag">SANDBOX</span>
      <span v-if="ended">This sandbox has ended.</span>
      <template v-else>
        <span>Everything here is simulated and yours alone. It disappears in {{ left }}.</span>
        <button type="button" class="band-button" :disabled="busy" @click="reset">Start over</button>
      </template>
      <span v-if="error" role="alert">{{ error }}</span>
    </div>
  </div>
</template>
