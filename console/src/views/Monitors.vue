<script setup lang="ts">
import { computed, ref } from 'vue';
import { api, ApiError, type Mode, type Monitor, type MonitorInput, type StatusMonitor } from '../api';
import { ago, pct, ms } from '../format';
import { usePoll } from '../poll';
import { session } from '../session';
import HealthDot from '../components/HealthDot.vue';
import ModeSwitch from '../components/ModeSwitch.vue';
import MonitorForm from '../components/MonitorForm.vue';

const monitors = ref<Monitor[]>([]);
const stats = ref<Record<string, StatusMonitor>>({});
const error = ref('');
const loaded = ref(false);
const owner = computed(() => session.me?.role === 'owner');

// One request for the whole screen. What it returns is summarised so polling can ease off while
// nothing changes.
async function refresh() {
  try {
    const o = await api.overview();
    monitors.value = o.monitors;
    stats.value = Object.fromEntries(o.stats.map((m) => [m.slug, m]));
    error.value = '';
    return o.monitors.map((m) => `${m.id}:${m.health}:${m.lastCheckedAt}:${m.openIncidentId}:${m.simulatedMode}:${m.paused}`).join('|');
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not load monitors.';
    throw e;
  } finally {
    loaded.value = true;
  }
}
const { poke } = usePoll(refresh, 5000);

async function setMode(m: Monitor, mode: Mode) {
  const before = m.simulatedMode;
  m.simulatedMode = mode; // show the change at once; the next check runs straight away
  try {
    Object.assign(m, await api.setMode(m.id, mode));
    await poke();
  } catch (e) {
    m.simulatedMode = before;
    error.value = e instanceof Error ? e.message : 'Could not change the mode.';
  }
}

async function remove(m: Monitor) {
  if (!confirm(`Delete ${m.name}? Its checks go with it; its incidents are kept.`)) return;
  try {
    await api.deleteMonitor(m.id);
    await poke();
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not delete.';
  }
}

// Adding a monitor: saved, then checked at once so its first result is there to see.
const adding = ref(false);
const saving = ref(false);
const formErrors = ref<Record<string, string>>({});
const formError = ref('');
async function add(input: MonitorInput) {
  saving.value = true;
  formErrors.value = {};
  formError.value = '';
  try {
    const created = await api.createMonitor(input);
    adding.value = false;
    try { await api.checkNow(created.id); } catch { /* it will be checked on schedule */ }
    await poke();
  } catch (e) {
    if (e instanceof ApiError && e.fields.length) formErrors.value = e.byField();
    else formError.value = e instanceof Error ? e.message : 'Could not add the monitor.';
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <section class="section stack">
    <div class="view-head">
      <div class="stack-sm">
        <span class="kicker">Monitors</span>
        <h1 class="page-title">{{ monitors.length }} watched, {{ monitors.filter((m) => m.health === 'down').length }} down</h1>
      </div>
      <button v-if="!adding" type="button" class="button primary" @click="adding = true">Add a monitor</button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <MonitorForm v-if="adding" :owner="owner" submit-label="Add and check" :errors="formErrors" :busy="saving" @submit="add" @cancel="adding = false" />
    <p v-if="adding && formError" class="error" role="alert">{{ formError }}</p>

    <p v-if="loaded && !monitors.length && !error" class="standfirst">Nothing is monitored yet. Add a monitor to start.</p>

    <div class="table-wrap" v-if="monitors.length">
      <table class="data console-table">
        <thead>
          <tr><th scope="col">Monitor</th><th scope="col">Health</th><th scope="col" class="num">Uptime, 24 h</th><th scope="col" class="num">Median</th><th scope="col">Last check</th><th scope="col">Behaviour</th><th scope="col"><span class="sr-only">Actions</span></th></tr>
        </thead>
        <tbody>
          <tr v-for="m in monitors" :key="m.id">
            <td class="name"><RouterLink :to="`/monitors/${m.id}`">{{ m.name }}</RouterLink>
              <span class="caption block">{{ m.kind === 'http' ? m.url : 'simulated' }} · every {{ m.intervalSeconds }} s{{ m.public ? '' : ' · private' }}</span></td>
            <td><span v-if="m.paused" class="status unknown">❚❚ Paused</span><HealthDot v-else :health="m.health" /><RouterLink v-if="m.openIncidentId" class="caption block" :to="`/incidents/${m.openIncidentId}`">open incident</RouterLink></td>
            <td class="num">{{ pct(stats[m.slug]?.uptime24h) }}</td>
            <td class="num">{{ ms(stats[m.slug]?.p50Ms) }}</td>
            <td>{{ ago(m.lastCheckedAt) }}</td>
            <td><ModeSwitch v-if="m.kind === 'simulated'" :mode="m.simulatedMode" :name="m.name" :group="m.id" @change="(mode) => setMode(m, mode)" /><span v-else class="caption">live HTTP</span></td>
            <td><button type="button" class="link-button" @click="remove(m)">Delete<span class="sr-only"> {{ m.name }}</span></button></td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
