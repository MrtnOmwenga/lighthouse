<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';
import { expire, session, signOut } from './session';
import SandboxBanner from './components/SandboxBanner.vue';

const router = useRouter();
const owner = computed(() => session.me?.role === 'owner');

// Bumped when the sandbox is reset, so the screen in view starts again from fresh data.
const generation = ref(0);
function ended() {
  if (expire()) router.replace({ name: 'welcome' });
}

async function leave() {
  await signOut();
  router.push({ name: 'welcome' });
}
</script>

<template>
  <a class="sr-only" href="#main">Skip to content</a>
  <SandboxBanner v-if="session.me?.role === 'sandbox'" :expires-at="session.me.expiresAt" @reset="generation++" @ended="ended" />
  <header class="masthead">
    <div class="wrap">
      <a class="brand" href="/">
        <svg width="44" height="52" viewBox="0 0 44 52" aria-hidden="true">
          <path class="beam" d="M22 12 L44 4 L44 20 Z" fill="#93203A" fill-opacity="0.35" />
          <path d="M17 50 L19 16 H25 L27 50 Z" fill="#1E1B18" />
          <path d="M18.2 28 H25.8 M17.6 38 H26.4" stroke="#FAEEE3" stroke-width="3" />
          <rect x="18" y="8" width="8" height="8" fill="#93203A" />
          <path d="M17 8 L22 3 L27 8 Z" fill="#1E1B18" />
          <path d="M12 50 H32" stroke="#1E1B18" stroke-width="2" />
        </svg>
        <span><span class="title">The Lighthouse</span><span class="sub">Console{{ owner ? ` · signed in as ${session.me?.login}` : '' }}</span></span>
      </a>
      <nav v-if="session.me" class="nav" aria-label="Console">
        <RouterLink to="/monitors" active-class="" exact-active-class="" :aria-current="$route.path.startsWith('/monitors') ? 'page' : undefined">Monitors</RouterLink>
        <RouterLink to="/incidents" :aria-current="$route.path.startsWith('/incidents') ? 'page' : undefined">Incidents</RouterLink>
        <RouterLink to="/status" :aria-current="$route.path === '/status' ? 'page' : undefined">Status page</RouterLink>
        <RouterLink v-if="owner" to="/readers" :aria-current="$route.path === '/readers' ? 'page' : undefined">Readers</RouterLink>
        <RouterLink v-if="owner" to="/security" :aria-current="$route.path === '/security' ? 'page' : undefined">Security</RouterLink>
        <button type="button" class="button" @click="leave">{{ owner ? 'Sign out' : 'Leave sandbox' }}</button>
      </nav>
    </div>
  </header>
  <main id="main" class="wrap console-main">
    <RouterView :key="generation" />
  </main>
  <footer class="foot">
    <div class="wrap">
      <span>The Lighthouse console. <a href="/">Back to the front page</a>.</span>
      <span class="links"><a href="/privacy">Privacy</a><a href="https://github.com/MrtnOmwenga/lighthouse" rel="noopener">Source</a></span>
    </div>
  </footer>
</template>
