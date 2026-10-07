<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { api, type Incident, type Monitor, type Severity } from '../api';
import { ago, words } from '../format';
import { usePoll } from '../poll';

const router = useRouter();
const incidents = ref<Incident[]>([]);
const next = ref<string | undefined>();
const monitors = ref<Monitor[]>([]);
const error = ref('');
const loaded = ref(false);

// The first page is kept fresh; older pages are loaded on demand.
async function refresh() {
  try {
    const [page, list] = await Promise.all([api.incidents(), api.monitors()]);
    const older = incidents.value.slice(page.incidents.length);
    incidents.value = [...page.incidents, ...older.filter((o) => !page.incidents.some((i) => i.id === o.id))];
    if (!older.length) next.value = page.next;
    monitors.value = list;
    error.value = '';
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not load incidents.';
  } finally {
    loaded.value = true;
  }
}
usePoll(refresh, 5000);

async function more() {
  if (!next.value) return;
  try {
    const page = await api.incidents(next.value);
    incidents.value.push(...page.incidents);
    next.value = page.next;
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not load older incidents.';
  }
}

const monitorName = (id: string | null) => monitors.value.find((m) => m.id === id)?.name;

const opening = ref(false);
const form = ref<{ title: string; severity: Severity; public: boolean; message: string; monitorId: string }>(
  { title: '', severity: 'medium', public: true, message: '', monitorId: '' });
const formError = ref('');
async function open() {
  formError.value = '';
  try {
    const { monitorId, ...rest } = form.value;
    const created = await api.createIncident(monitorId ? { ...rest, monitorId } : rest);
    router.push(`/incidents/${created.id}`);
  } catch (e) {
    formError.value = e instanceof Error ? e.message : 'Could not open the incident.';
  }
}
</script>

<template>
  <section class="section stack">
    <div class="view-head">
      <div class="stack-sm">
        <span class="kicker">Incidents</span>
        <h1 class="page-title">{{ incidents.filter((i) => i.status !== 'resolved').length }} ongoing</h1>
      </div>
      <button type="button" class="button primary" @click="opening = !opening">{{ opening ? 'Cancel' : 'Open an incident' }}</button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <form v-if="opening" class="panel form-grid" @submit.prevent="open">
      <p class="label span-all">New incident</p>
      <label class="span-all">Title <input v-model="form.title" required maxlength="200" placeholder="Checkout is slow"></label>
      <label>Severity
        <select v-model="form.severity"><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select>
      </label>
      <label>Affects
        <select v-model="form.monitorId"><option value="">No particular monitor</option><option v-for="m in monitors" :key="m.id" :value="m.id">{{ m.name }}</option></select>
      </label>
      <label class="span-all">First update <textarea v-model="form.message" required maxlength="2000" rows="3" placeholder="What's happening, and what you're doing about it."></textarea></label>
      <label class="check span-all"><input v-model="form.public" type="checkbox"> Public: show it on the status page</label>
      <p v-if="formError" class="error span-all" role="alert">{{ formError }}</p>
      <div class="actions span-all"><button type="submit" class="button primary">Open</button></div>
    </form>

    <p v-if="loaded && !incidents.length" class="standfirst">No incidents yet. Break a simulated site on the monitors page and one will open by itself.</p>

    <article v-for="i in incidents" :key="i.id" class="report">
      <span class="pill" :class="i.status">{{ words(i.status) }} · {{ i.severity }} severity{{ i.automatic ? ' · opened automatically' : '' }}{{ i.public ? '' : ' · private' }}</span>
      <h3><RouterLink :to="`/incidents/${i.id}`">{{ i.title }}</RouterLink></h3>
      <p class="caption">Started {{ ago(i.startedAt) }}<template v-if="monitorName(i.monitorId)"> · {{ monitorName(i.monitorId) }}</template><template v-if="i.resolvedAt"> · resolved {{ ago(i.resolvedAt) }}</template></p>
    </article>
    <button v-if="next" type="button" class="button" @click="more">Older incidents</button>
  </section>
</template>
