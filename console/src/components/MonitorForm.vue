<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { api, ApiError, type MonitorInput, type TestResult } from '../api';

// The settings of a monitor, for adding one and for editing one. The server decides what is
// valid; its answers are shown beside the fields they are about.
const props = defineProps<{
  initial?: MonitorInput;
  owner: boolean;
  editing?: boolean;
  submitLabel: string;
  errors?: Record<string, string>;
  busy?: boolean;
}>();
const emit = defineEmits<{ submit: [MonitorInput]; cancel: [] }>();

const form = reactive({
  name: props.initial?.name ?? '',
  kind: props.initial?.kind ?? 'simulated',
  url: props.initial?.url ?? '',
  simulatedMode: props.initial?.simulatedMode ?? 'up',
  intervalSeconds: props.initial?.intervalSeconds ?? (props.owner ? 60 : 30),
  timeoutMs: props.initial?.timeoutMs ?? 10000,
  expectedStatusMin: props.initial?.expectedStatusMin ?? 200,
  expectedStatusMax: props.initial?.expectedStatusMax ?? 399,
  expectedText: props.initial?.expectedText ?? '',
  failureThreshold: props.initial?.failureThreshold ?? 3,
  recoveryThreshold: props.initial?.recoveryThreshold ?? 2,
  public: props.initial?.public ?? true,
  paused: props.initial?.paused ?? false,
  allowPrivateNetwork: props.initial?.allowPrivateNetwork ?? false,
});
const http = computed(() => form.kind === 'http');

function body(): MonitorInput {
  const b: MonitorInput = { ...form, expectedText: form.expectedText || null };
  if (!http.value) { b.url = null; b.allowPrivateNetwork = false; }
  // Keep the address this monitor is known by when it is renamed.
  if (props.editing && props.initial?.slug) b.slug = props.initial.slug;
  return b;
}

// Problems reported while trying the settings, until the parent reports its own.
const tried = ref<Record<string, string>>({});
const problems = computed(() => ({ ...tried.value, ...(props.errors ?? {}) }));
const advancedOpen = computed(() =>
  ['timeoutMs', 'expectedStatusMin', 'expectedText', 'failureThreshold', 'recoveryThreshold'].some((f) => problems.value[f]));

const testing = ref(false);
const result = ref<TestResult | null>(null);
const testError = ref('');
async function test() {
  testing.value = true;
  result.value = null;
  testError.value = '';
  tried.value = {};
  try {
    result.value = await api.testMonitor(body());
  } catch (e) {
    if (e instanceof ApiError && e.fields.length) tried.value = e.byField();
    else testError.value = e instanceof Error ? e.message : 'Could not run the test.';
  } finally {
    testing.value = false;
  }
}

const failures: Record<string, string> = {
  timeout: 'it did not answer in time',
  connection: 'it could not be reached',
  status: 'it answered with an unexpected status',
  content: 'the expected text was not in the answer',
  tls: 'its certificate was not accepted',
  blocked: 'that address is private or reserved, and is not allowed',
};
const outcome = computed(() => {
  const r = result.value;
  if (!r) return '';
  if (r.ok) return `Passed${r.statusCode ? ` with status ${r.statusCode}` : ''} in ${r.latencyMs} ms.`;
  return `Failed: ${failures[r.failure ?? ''] ?? r.failure ?? 'unknown reason'}${r.statusCode ? ` (status ${r.statusCode})` : ''}.`;
});
</script>

<template>
  <form class="panel form-grid" novalidate @submit.prevent="emit('submit', body())">
    <p class="label span-all">{{ editing ? 'Edit monitor' : 'New monitor' }}</p>

    <label>Name
      <input v-model="form.name" required maxlength="100" placeholder="Payments API" :aria-invalid="!!problems.name" aria-describedby="err-name">
      <span v-if="problems.name || problems.slug" id="err-name" class="field-error">{{ problems.name || `That name's address is taken: ${problems.slug}` }}</span>
    </label>
    <label v-if="owner">Kind
      <select v-model="form.kind" :aria-invalid="!!problems.kind" aria-describedby="err-kind">
        <option value="simulated">Simulated</option><option value="http">HTTP</option>
      </select>
      <span v-if="problems.kind" id="err-kind" class="field-error">{{ problems.kind }}</span>
    </label>
    <label v-if="http" class="span-all">URL
      <input v-model="form.url" type="url" inputmode="url" placeholder="https://example.com/health" :aria-invalid="!!problems.url" aria-describedby="err-url">
      <span v-if="problems.url" id="err-url" class="field-error">{{ problems.url }}</span>
    </label>
    <label>Check every (seconds)
      <input v-model.number="form.intervalSeconds" type="number" inputmode="numeric" :min="owner ? 5 : 10" max="3600" :aria-invalid="!!problems.intervalSeconds" aria-describedby="err-interval">
      <span v-if="problems.intervalSeconds" id="err-interval" class="field-error">{{ problems.intervalSeconds }}</span>
    </label>
    <label class="check"><input v-model="form.public" type="checkbox"> Show on the status page</label>
    <label v-if="editing" class="check"><input v-model="form.paused" type="checkbox"> Paused: don't check it</label>

    <details class="span-all advanced" :open="advancedOpen || undefined">
      <summary>When it counts as down</summary>
      <div class="form-grid">
        <label>Failures in a row before it is down
          <input v-model.number="form.failureThreshold" type="number" inputmode="numeric" min="1" max="20" :aria-invalid="!!problems.failureThreshold" aria-describedby="err-fail">
          <span v-if="problems.failureThreshold" id="err-fail" class="field-error">{{ problems.failureThreshold }}</span>
        </label>
        <label>Successes in a row before it is up again
          <input v-model.number="form.recoveryThreshold" type="number" inputmode="numeric" min="1" max="20" :aria-invalid="!!problems.recoveryThreshold" aria-describedby="err-recover">
          <span v-if="problems.recoveryThreshold" id="err-recover" class="field-error">{{ problems.recoveryThreshold }}</span>
        </label>
        <template v-if="http">
          <label>Give up after (milliseconds)
            <input v-model.number="form.timeoutMs" type="number" inputmode="numeric" min="100" max="30000" :aria-invalid="!!problems.timeoutMs" aria-describedby="err-timeout">
            <span v-if="problems.timeoutMs" id="err-timeout" class="field-error">{{ problems.timeoutMs }}</span>
          </label>
          <label>Accepted status, from and to
            <span class="pair">
              <input v-model.number="form.expectedStatusMin" type="number" inputmode="numeric" min="100" max="599" aria-label="Lowest accepted status" :aria-invalid="!!problems.expectedStatusMin" aria-describedby="err-status">
              <input v-model.number="form.expectedStatusMax" type="number" inputmode="numeric" min="100" max="599" aria-label="Highest accepted status" :aria-invalid="!!problems.expectedStatusMin" aria-describedby="err-status">
            </span>
            <span v-if="problems.expectedStatusMin" id="err-status" class="field-error">{{ problems.expectedStatusMin }}</span>
          </label>
          <label class="span-all">The answer must contain (optional)
            <input v-model="form.expectedText" maxlength="200" placeholder="&quot;status&quot;:&quot;ok&quot;" :aria-invalid="!!problems.expectedText" aria-describedby="err-text">
            <span v-if="problems.expectedText" id="err-text" class="field-error">{{ problems.expectedText }}</span>
          </label>
          <label class="check span-all"><input v-model="form.allowPrivateNetwork" type="checkbox"> Allow a private or internal address</label>
        </template>
      </div>
    </details>

    <p v-if="!owner" class="caption span-all">Sandbox monitors are simulated: the sandbox never sends traffic to real sites.</p>
    <p v-if="outcome" class="span-all test-result" :class="result?.ok ? 'up' : 'down'" role="status">{{ outcome }}</p>
    <p v-if="testError" class="error span-all" role="alert">{{ testError }}</p>
    <div class="actions span-all">
      <button type="submit" class="button primary" :disabled="busy">{{ submitLabel }}</button>
      <button type="button" class="button" :disabled="testing" @click="test">{{ testing ? 'Testing…' : 'Test these settings' }}</button>
      <button type="button" class="button" @click="emit('cancel')">Cancel</button>
    </div>
  </form>
</template>
