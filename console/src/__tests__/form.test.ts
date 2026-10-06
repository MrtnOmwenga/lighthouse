import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import MonitorForm from '../components/MonitorForm.vue';

describe('MonitorForm', () => {
  it('offers a sandbox simulated monitors only', () => {
    const w = mount(MonitorForm, { props: { owner: false, submitLabel: 'Add' } });
    expect(w.find('select').exists()).toBe(false);
    expect(w.find('input[type="url"]').exists()).toBe(false);
    expect(w.text()).toContain('never sends traffic to real sites');
  });

  it('submits an HTTP monitor with its settings, and a simulated one without an address', async () => {
    const w = mount(MonitorForm, { props: { owner: true, submitLabel: 'Add' } });
    await w.find('input').setValue('Payments API');
    await w.find('select').setValue('http');
    await w.find('input[type="url"]').setValue('https://example.com/health');
    await w.find('form').trigger('submit');
    expect(w.emitted('submit')![0]![0]).toMatchObject({ name: 'Payments API', kind: 'http', url: 'https://example.com/health', failureThreshold: 3, expectedText: null });

    await w.find('select').setValue('simulated');
    await w.find('form').trigger('submit');
    expect(w.emitted('submit')![1]![0]).toMatchObject({ kind: 'simulated', url: null, allowPrivateNetwork: false });
  });

  it('starts from the monitor being edited and keeps its address when renamed', async () => {
    const w = mount(MonitorForm, { props: { owner: true, editing: true, submitLabel: 'Save',
      initial: { name: 'Old', slug: 'old', kind: 'simulated', intervalSeconds: 45, failureThreshold: 5, paused: true } } });
    expect((w.find('input').element as HTMLInputElement).value).toBe('Old');
    await w.find('input').setValue('New name');
    await w.find('form').trigger('submit');
    expect(w.emitted('submit')![0]![0]).toMatchObject({ name: 'New name', slug: 'old', intervalSeconds: 45, failureThreshold: 5, paused: true });
  });

  it('shows the server\'s problems beside their fields, and opens the section that holds one', () => {
    const w = mount(MonitorForm, { props: { owner: true, submitLabel: 'Add', errors: { name: '1 to 100 characters.', failureThreshold: '1 to 20.' } } });
    expect(w.find('#err-name').text()).toBe('1 to 100 characters.');
    expect(w.find('input').attributes('aria-invalid')).toBe('true');
    expect(w.find('details').attributes('open')).toBeDefined();
    expect(w.find('#err-fail').text()).toBe('1 to 20.');
  });
});
