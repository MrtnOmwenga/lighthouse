import { createApp } from 'vue';
import App from './App.vue';
import { onUnauthorized } from './api';
import { router } from './router';
import { expire } from './session';
import './console.css';

// The server no longer knows this session: back to the welcome page, which says why.
onUnauthorized(() => {
  if (expire()) router.replace({ name: 'welcome' });
});

// Each screen is a separate file, fetched the first time it is opened. After a release the files
// this tab knows about are gone; reload once to pick up the new ones.
window.addEventListener('vite:preloadError', (event) => {
  const key = 'lh-reloaded-at';
  let last = 0;
  try { last = Number(sessionStorage.getItem(key)) || 0; } catch { /* storage unavailable */ }
  if (Date.now() - last < 60_000) return; // already tried: let the error show
  try { sessionStorage.setItem(key, String(Date.now())); } catch { /* storage unavailable */ }
  event.preventDefault();
  location.reload();
});

createApp(App).use(router).mount('#app');
