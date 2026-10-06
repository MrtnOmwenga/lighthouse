import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError, onUnauthorized } from '../api';
import { nextDelay, QUIET_AFTER } from '../poll';

function respond(status: number, body: unknown, headers: Record<string, string> = {}) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), { status, headers })));
}
async function failure(): Promise<ApiError> {
  try { await api.monitors(); } catch (e) { return e as ApiError; }
  throw new Error('expected the call to fail');
}

describe('api errors', () => {
  afterEach(() => { vi.unstubAllGlobals(); onUnauthorized(() => {}); });

  it('carries each invalid field, for the form to show beside its input', async () => {
    respond(422, { title: 'Invalid request', detail: 'name: 1 to 100 characters.', errors: [{ field: 'name', message: '1 to 100 characters.' }, { field: 'url', message: 'Required.' }] });
    const e = await failure();
    expect(e.byField()).toEqual({ name: '1 to 100 characters.', url: 'Required.' });
  });

  it('tells the app when the session is gone', async () => {
    const gone = vi.fn();
    onUnauthorized(gone);
    respond(401, { title: 'Sign in first' });
    expect((await failure()).message).toContain('session has ended');
    expect(gone).toHaveBeenCalledOnce();
  });

  it('gives a server failure a reference to quote, and nothing else', async () => {
    respond(500, { title: 'Something went wrong', requestId: 'abc123' });
    expect((await failure()).message).toBe('Something went wrong on the server. If you report it, quote abc123.');
  });

  it('says to wait when requests come too fast, and says so when there is no answer at all', async () => {
    respond(429, { title: 'Too many requests' });
    expect((await failure()).message).toContain('Wait a few seconds');
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('network'); }));
    const e = await failure();
    expect(e.status).toBe(0);
    expect(e.message).toContain('No answer from the server');
  });
});

describe('polling', () => {
  it('eases off once nothing has changed for a while', () => {
    expect(nextDelay(5000, 0)).toBe(5000);
    expect(nextDelay(5000, QUIET_AFTER - 1)).toBe(5000);
    expect(nextDelay(5000, QUIET_AFTER)).toBe(15000);
  });
});
