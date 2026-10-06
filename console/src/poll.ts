import { onMounted, onUnmounted } from 'vue';

// How many unchanged answers in a row before polling slows down, and by how much.
export const QUIET_AFTER = 6;
export const QUIET_FACTOR = 3;

// nextDelay is how long to wait before the next poll: the usual interval, or longer once the
// answer has stopped changing.
export function nextDelay(ms: number, unchanged: number): number {
  return unchanged >= QUIET_AFTER ? ms * QUIET_FACTOR : ms;
}

// usePoll runs fn now and then every `ms` while the page is visible, and stops with the view.
// If fn returns a string describing what it saw, polling slows down while that stays the same
// (nothing is happening) and speeds up again the moment it changes. poke() runs it at once and
// goes back to full speed: call it after the person does something.
export function usePoll(fn: () => Promise<unknown> | unknown, ms: number) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false;
  let last: string | undefined;
  let unchanged = 0;
  let running = false;

  const run = async () => {
    if (running) return;
    running = true;
    try {
      const seen = await fn();
      if (typeof seen === 'string') {
        unchanged = seen === last ? unchanged + 1 : 0;
        last = seen;
      }
    } catch { /* the view shows its own error state */ } finally {
      running = false;
    }
  };
  const tick = async () => {
    if (stopped) return;
    if (document.visibilityState === 'visible') await run();
    if (!stopped) timer = setTimeout(tick, nextDelay(ms, unchanged));
  };
  const poke = async () => {
    unchanged = 0;
    clearTimeout(timer);
    await run();
    if (!stopped) timer = setTimeout(tick, ms);
  };
  onMounted(tick);
  onUnmounted(() => { stopped = true; clearTimeout(timer); });
  return { poke };
}
