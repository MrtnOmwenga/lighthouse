<script setup lang="ts">
import { computed, ref } from 'vue';
import { api, ApiError, type Check, type Mode, type Monitor, type MonitorInput } from '../api';
import { ago, when } from '../format';
import { usePoll } from '../poll';
import { session } from '../session';
import HealthDot from '../components/HealthDot.vue';
import ModeSwitch from '../components/ModeSwitch.vue';
import MonitorForm from '../components/MonitorForm.vue';
import Sparkline from '../components/Sparkline.vue';

const props = defineProps<{ id: string }>();
const monitor = ref<Monitor | null>(null);
const checks = ref<Check[]>([]);
const error = ref('');
const owner = computed(() => session.me?.role === 'owner');

async function refresh() {
  try {
    [monitor.value, checks.value] = await Promise.all([api.monitor(props.id), api.checks(props.id)]);
    error.value = '';
    return `${monitor.value.health}:${checks.value[0]?.id}:${monitor.value.paused}`;
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not load the monitor.';
    throw e;
  }
}
const { poke } = usePoll(refresh, 5000);

const passing = computed(() => checks.value.filter((c) => c.ok).length);

async function act(what: string, fn: () => Promise<unknown>) {
  try {
    await fn();
    await poke();
  } catch (e) {
    error.value = e instanceof Error ? e.message : `Could not ${what}.`;
  }
}
const setMode = (mode: Mode) => act('change the mode', async () => { monitor.value = await api.setMode(props.id, mode); });

const checking = ref(false);
async function checkNow() {
  checking.value = true;
  await act('check it', () => api.checkNow(props.id));
  checking.value = false;
}

// Editing: the form starts from the monitor as it is; saving checks it at once.
const editing = ref(false);
const saving = ref(false);
const formErrors = ref<Record<string, string>>({});
async function save(input: MonitorInput) {
  saving.value = true;
  formErrors.value = {};
  try {
    monitor.value = await api.updateMonitor(props.id, input);
    editing.value = false;
    if (!monitor.value.paused) { try { await api.checkNow(props.id); } catch { /* checked on schedule */ } }
    await poke();
  } catch (e) {
    if (e instanceof ApiError && e.fields.length) formErrors.value = e.byField();
    else error.value = e instanceof Error ? e.message : 'Could not save.';
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <section class="section stack">
    <RouterLink class="more" to="/monitors">← All monitors</RouterLink>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <template v-if="monitor">
      <div class="view-head">
        <div class="stack-sm">
          <span class="kicker">{{ monitor.kind === 'http' ? 'HTTP monitor' : 'Simulated site' }}{{ monitor.public ? '' : ' · private' }}</span>
          <h1 class="page-title">{{ monitor.name }}</h1>
          <p class="byline">
            <span v-if="monitor.paused" class="status unknown">❚❚ Paused</span><HealthDot v-else :health="monitor.health" /> · checked {{ ago(monitor.lastCheckedAt) }} · every {{ monitor.intervalSeconds }} s ·
            down after {{ monitor.failureThreshold }} failures, up after {{ monitor.recoveryThreshold }} successes
          </p>
          <p v-if="monitor.kind === 'http'" class="caption">{{ monitor.url }}</p>
        </div>
        <div class="actions">
          <ModeSwitch v-if="monitor.kind === 'simulated'" :mode="monitor.simulatedMode" :name="monitor.name" :group="monitor.id" @change="setMode" />
          <button type="button" class="button" :disabled="checking || monitor.paused" @click="checkNow">{{ checking ? 'Checking…' : 'Check now' }}</button>
          <button v-if="!editing" type="button" class="button" @click="editing = true">Edit</button>
        </div>
      </div>

      <MonitorForm v-if="editing" :key="monitor.id" :initial="monitor" :owner="owner" editing submit-label="Save and check" :errors="formErrors" :busy="saving"
                   @submit="save" @cancel="editing = false" />

      <div class="panel ink chart-panel">
        <p class="label">Last {{ checks.length }} checks · {{ passing }} passed</p>
        <Sparkline v-if="checks.length" :checks="checks" />
        <p v-else class="caption">No checks yet.</p>
      </div>

      <div class="table-wrap">
        <table class="data console-table">
          <thead><tr><th scope="col">When</th><th scope="col">Result</th><th scope="col" class="num">Status</th><th scope="col" class="num">Response</th><th scope="col">Failure</th></tr></thead>
          <tbody>
            <tr v-for="c in checks.slice(0, 30)" :key="c.id">
              <td>{{ when(c.at) }}</td>
              <td><span class="status" :class="c.ok ? 'up' : 'down'">{{ c.ok ? '● Passed' : '● Failed' }}</span><span v-if="c.warmup" class="caption block">woke it up</span></td>
              <td class="num">{{ c.statusCode ?? '–' }}</td>
              <td class="num">{{ c.latencyMs }} ms</td>
              <td>{{ c.failure ?? '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>
