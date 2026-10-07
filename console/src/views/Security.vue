<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { api, type AuthEvent, type Security } from '../api';
import { when } from '../format';

// Who can act as the owner right now, and who has tried: live sessions, and the record of
// sign-ins. No network addresses are kept.
const data = ref<Security | null>(null);
const error = ref('');
const busy = ref(false);

async function load() {
  try {
    data.value = await api.security();
    error.value = '';
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not load.';
  }
}
onMounted(load);

const others = computed(() => data.value?.sessions.filter((s) => !s.current).length ?? 0);

async function act(fn: () => Promise<unknown>) {
  busy.value = true;
  try {
    await fn();
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not do that.';
  } finally {
    busy.value = false;
  }
}
const end = (id: string) => act(() => api.endSession(id));
function endOthers() {
  if (confirm('Sign out of every other session? This one stays signed in.')) act(() => api.endOtherSessions());
}

const said: Record<AuthEvent['kind'], string> = {
  signed_in: 'Signed in',
  refused: 'Refused',
  signed_out: 'Signed out',
  sessions_ended: 'Sessions ended',
};
</script>

<template>
  <section class="section stack">
    <div class="view-head">
      <div class="stack-sm">
        <span class="kicker">Security · private</span>
        <h1 class="page-title">Who is signed in, and who has tried</h1>
      </div>
      <button v-if="others" type="button" class="button primary" :disabled="busy" @click="endOthers">Sign out everywhere else</button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <template v-if="data">
      <h2 class="section-title rule-top">Sessions</h2>
      <div class="table-wrap">
        <table class="data console-table">
          <thead><tr><th scope="col">Device</th><th scope="col">Account</th><th scope="col">Signed in</th><th scope="col">Ends</th><th scope="col"><span class="sr-only">Actions</span></th></tr></thead>
          <tbody>
            <tr v-for="s in data.sessions" :key="s.id">
              <td class="name">{{ s.device || 'unknown' }}</td><td>{{ s.login ?? '' }}</td><td>{{ when(s.createdAt) }}</td><td>{{ when(s.expiresAt) }}</td>
              <td><span v-if="s.current" class="caption">This session</span>
                <button v-else type="button" class="link-button" :disabled="busy" @click="end(s.id)">End<span class="sr-only"> the {{ s.device }} session from {{ when(s.createdAt) }}</span></button></td>
            </tr>
          </tbody>
        </table>
      </div>

      <h2 class="section-title rule-top">Sign-ins</h2>
      <p class="caption">The last 50, kept for 90 days. A refusal is a GitHub account other than yours trying to sign in here.</p>
      <p v-if="!data.events.length" class="caption">Nothing recorded yet.</p>
      <div v-else class="table-wrap">
        <table class="data console-table">
          <thead><tr><th scope="col">When</th><th scope="col">What</th><th scope="col">GitHub account</th><th scope="col">Detail</th></tr></thead>
          <tbody>
            <tr v-for="e in data.events" :key="e.id">
              <td>{{ when(e.at) }}</td>
              <td><span class="status" :class="e.kind === 'refused' ? 'down' : 'up'">{{ said[e.kind] }}</span></td>
              <td>{{ e.login }}<span v-if="e.githubId" class="caption"> · id {{ e.githubId }}</span></td>
              <td>{{ e.detail }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>
