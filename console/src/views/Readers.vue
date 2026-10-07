<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api, type Report } from '../api';
import { duration, when, slugify } from '../format';

// The owner's private readership report, and a maker for ?ref= links: one tag per application,
// so an opened link shows up here with everything that visitor went on to read.
const days = ref(30);
const report = ref<Report | null>(null);
const error = ref('');

async function load() {
  try {
    report.value = await api.report(days.value);
    error.value = '';
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not load the report.';
  }
}
watch(days, load, { immediate: true });

// This browser can be marked as the owner's, so its visits aren't counted when he is signed out.
const excluded = ref(false);
onMounted(async () => {
  try { excluded.value = (await api.exclusion()).excluded; } catch { /* the report shows its own error */ }
});
async function setExcluded(value: boolean) {
  try {
    excluded.value = (await api.setExclusion(value)).excluded;
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not change the setting.';
  }
}

const company = ref('');
const target = ref('/');
const tag = computed(() => slugify(company.value));
const link = computed(() => (tag.value ? `${location.origin}${target.value}?ref=${tag.value}` : ''));
const copied = ref(false);
async function copy() {
  await navigator.clipboard.writeText(link.value);
  copied.value = true;
  setTimeout(() => { copied.value = false; }, 2000);
}
</script>

<template>
  <section class="section stack">
    <div class="view-head">
      <div class="stack-sm">
        <span class="kicker">Readers · private</span>
        <h1 class="page-title">Who read what</h1>
      </div>
      <label class="inline">Last
        <select v-model.number="days"><option :value="1">day</option><option :value="7">7 days</option><option :value="30">30 days</option><option :value="90">90 days</option></select>
      </label>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <form class="panel form-grid" @submit.prevent="copy">
      <p class="label span-all">Make a tracking link</p>
      <label>Company or role <input v-model="company" placeholder="Acme, backend role" maxlength="60"></label>
      <label>Page
        <select v-model="target"><option value="/">Front page</option><option value="/projects">Projects</option><option value="/about">About</option></select>
      </label>
      <p class="span-all link-out"><code>{{ link || 'Type a company to make a link' }}</code></p>
      <div class="actions span-all">
        <button type="submit" class="button primary" :disabled="!link">{{ copied ? 'Copied' : 'Copy link' }}</button>
      </div>
      <p class="caption span-all">The tag is removed from the visitor's address bar once read, so it doesn't spread if they share the link.</p>
    </form>

    <label class="check">
      <input type="checkbox" :checked="excluded" @change="setExcluded(($event.target as HTMLInputElement).checked)">
      Don't count visits from this browser, even when I'm signed out
    </label>
    <p class="caption">Set it once on each of your own browsers and devices. It lasts a year.</p>

    <template v-if="report">
      <h2 class="section-title rule-top">Everyone</h2>
      <div class="stats">
        <div class="stat claret"><span class="k">Engaged readers</span><p class="v">{{ report.summary.engagedVisitors }}</p></div>
        <div class="stat"><span class="k">All visitors</span><p class="v">{{ report.summary.visitors }}</p></div>
        <div class="stat"><span class="k">Page views</span><p class="v">{{ report.summary.views }}</p></div>
      </div>
      <p class="caption">Counted per day since {{ when(report.since) }}. Engaged: read a page for at least five seconds, or opened a demo. The rest glanced and left, or were scanners.</p>

      <h2 class="section-title rule-top">Tagged links</h2>
      <p v-if="!report.refs.length" class="caption">No tagged link has been opened yet.</p>
      <div v-else class="table-wrap">
        <table class="data console-table">
          <thead><tr><th scope="col">Tag</th><th scope="col">Last seen</th><th scope="col" class="num">Pages</th><th scope="col" class="num">Reading</th><th scope="col" class="num">Demos</th><th scope="col">Read</th></tr></thead>
          <tbody>
            <tr v-for="r in report.refs" :key="r.ref">
              <td class="name">{{ r.ref }}</td><td>{{ when(r.lastSeen) }}</td><td class="num">{{ r.views }}</td>
              <td class="num">{{ duration(r.engagedSeconds) }}</td><td class="num">{{ r.demosOpened }}</td>
              <td class="wrap-cell">{{ r.pages.join(', ') }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <h2 class="section-title rule-top">Projects</h2>
      <div class="table-wrap">
        <table class="data console-table">
          <thead><tr><th scope="col">Project</th><th scope="col" class="num">Engaged</th><th scope="col" class="num">Visitors</th><th scope="col" class="num">Views</th><th scope="col" class="num">Median reading</th><th scope="col" class="num">Launch pages</th><th scope="col" class="num">Demos opened</th></tr></thead>
          <tbody>
            <tr v-for="p in report.projects" :key="p.project">
              <td class="name">{{ p.project }}</td><td class="num">{{ p.engagedVisitors }}</td><td class="num">{{ p.visitors }}</td><td class="num">{{ p.views }}</td>
              <td class="num">{{ duration(p.medianEngagedSeconds) }}</td><td class="num">{{ p.launches }}</td><td class="num">{{ p.opens }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="grid">
        <div class="span-6">
          <h2 class="section-title rule-top">Came from</h2>
          <table class="data console-table"><tbody>
            <tr v-for="r in report.referrers" :key="r.label"><td>{{ r.label }}</td><td class="num">{{ r.visitors }}</td></tr>
          </tbody></table>
        </div>
        <div class="span-6">
          <h2 class="section-title rule-top">Devices</h2>
          <table class="data console-table"><tbody>
            <tr v-for="d in report.devices" :key="d.label"><td>{{ d.label }}</td><td class="num">{{ d.visitors }}</td></tr>
          </tbody></table>
        </div>
      </div>
    </template>
  </section>
</template>
