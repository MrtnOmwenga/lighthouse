import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';
import { loadSession, session } from './session';

const routes: RouteRecordRaw[] = [
  { path: '/', name: 'welcome', component: () => import('./views/Welcome.vue'), meta: { public: true } },
  { path: '/monitors', name: 'monitors', component: () => import('./views/Monitors.vue') },
  { path: '/monitors/:id', name: 'monitor', component: () => import('./views/MonitorDetail.vue'), props: true },
  { path: '/incidents', name: 'incidents', component: () => import('./views/Incidents.vue') },
  { path: '/incidents/:id', name: 'incident', component: () => import('./views/IncidentDetail.vue'), props: true },
  { path: '/status', name: 'status', component: () => import('./views/StatusPreview.vue') },
  { path: '/readers', name: 'readers', component: () => import('./views/Readers.vue'), meta: { owner: true } },
  { path: '/security', name: 'security', component: () => import('./views/Security.vue'), meta: { owner: true } },
  { path: '/:rest(.*)*', redirect: '/' },
];

export const router = createRouter({ history: createWebHistory('/console/'), routes });

// Signed-out visitors land on the welcome page; readers is the owner's alone; a signed-in visitor
// skips the welcome page.
router.beforeEach(async (to) => {
  if (!session.loaded) await loadSession();
  if (!session.me && !to.meta.public) return { name: 'welcome' };
  if (to.meta.owner && session.me?.role !== 'owner') return { name: 'monitors' };
  if (to.name === 'welcome' && session.me) return { name: 'monitors' };
  return true;
});
