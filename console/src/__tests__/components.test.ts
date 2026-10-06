import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import ModeSwitch from '../components/ModeSwitch.vue';
import Sparkline from '../components/Sparkline.vue';
import type { Check } from '../api';

describe('ModeSwitch', () => {
  it('is a labelled radio group that reports the chosen mode', async () => {
    const w = mount(ModeSwitch, { props: { mode: 'up', name: 'Checkout API', group: 'a1' } });
    expect(w.find('legend').text()).toContain('Checkout API');
    const radios = w.findAll('input[type="radio"]');
    expect(radios).toHaveLength(4);
    expect((radios[0]!.element as HTMLInputElement).checked).toBe(true);
    await radios[3]!.trigger('change');
    expect(w.emitted('change')).toEqual([['down']]);
  });

  it('groups its radio buttons by monitor, not by name', () => {
    const a = mount(ModeSwitch, { props: { mode: 'up', name: 'API', group: 'one' } });
    const b = mount(ModeSwitch, { props: { mode: 'up', name: 'API', group: 'two' } });
    expect(a.find('input').attributes('name')).not.toBe(b.find('input').attributes('name'));
  });
});

describe('Sparkline', () => {
  const check = (id: number, ok: boolean, latencyMs: number): Check =>
    ({ id, monitorId: 'm', at: '', ok, statusCode: ok ? 200 : 503, latencyMs, failure: ok ? null : 'status', tlsExpiresAt: null });

  it('draws passing checks as a line and failures as marks, oldest first', () => {
    // The API returns checks newest first.
    const w = mount(Sparkline, { props: { checks: [check(3, true, 100), check(2, false, 20), check(1, true, 50)] } });
    const points = w.find('polyline').attributes('points')!.split(' ');
    expect(points).toHaveLength(2);
    const [first, last] = points.map((p) => p.split(',').map(Number));
    expect(first![0]).toBeLessThan(last![0]!); // oldest on the left
    expect(first![1]).toBeGreaterThan(last![1]!); // 50 ms sits lower than the 100 ms peak
    expect(w.findAll('rect.fail')).toHaveLength(1);
    expect(w.attributes('aria-label')).toContain('3 checks');
  });
});
