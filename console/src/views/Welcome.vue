<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { api, type SignInOptions } from '../api';
import { loadSession, session } from '../session';

const router = useRouter();
const options = ref<SignInOptions | null>(null);
const busy = ref(false);
const error = ref('');
// Why the visitor is back here, said once.
const notice = session.notice;
session.notice = '';

onMounted(async () => {
  try { options.value = await api.signInOptions(); } catch { options.value = { github: false, dev: false, sandbox: true }; }
});

async function enter(how: 'sandbox' | 'dev') {
  busy.value = true;
  error.value = '';
  try {
    if (how === 'sandbox') await api.startSandbox();
    else await api.devLogin();
    await loadSession();
    router.push({ name: 'monitors' });
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Something went wrong.';
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="section grid">
    <div class="span-8 stack">
      <span class="kicker">Console</span>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <h1 class="page-title">Run the monitor yourself</h1>
      <p class="standfirst">
        The sandbox gives you a private copy of Lighthouse with three simulated sites. Break one, watch the monitor
        notice, open an incident and resolve it when the site recovers. No account; it disappears after two hours.
      </p>
      <div class="actions">
        <button type="button" class="button primary" :disabled="busy" @click="enter('sandbox')">Start a sandbox →</button>
        <a v-if="options?.github" class="button" href="/auth/github">Owner sign-in with GitHub</a>
        <button v-if="options?.dev" type="button" class="button" :disabled="busy" @click="enter('dev')">Development sign-in</button>
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
    </div>
    <aside class="span-4 panel align-start">
      <p class="label">What you can try</p>
      <ol class="try-list">
        <li>Switch <b>Checkout API</b> to <b>Down</b> and wait: after two failed checks an incident opens by itself.</li>
        <li>Post an update on the incident, public or internal, and see what your status page shows.</li>
        <li>Switch it back to <b>Up</b>: two good checks later, the incident closes.</li>
      </ol>
    </aside>
  </section>
</template>
