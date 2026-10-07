// The console's view of Lighthouse's JSON API. Every call is same-origin with the session cookie;
// errors come back as RFC 9457 problem details and are thrown as ApiError, which says what to tell
// the person: what to fix (by field), to wait, to sign in again, or what to quote in a report.

export type Role = 'owner' | 'sandbox';
export type Health = 'up' | 'down' | 'unknown';
export type Mode = 'up' | 'slow' | 'flaky' | 'down';

export interface Me { role: Role; login: string; expiresAt: string }
export interface SignInOptions { github: boolean; dev: boolean; sandbox: boolean }

export interface Monitor {
  id: string;
  name: string;
  slug: string;
  kind: 'http' | 'simulated';
  url: string | null;
  simulatedMode: Mode;
  intervalSeconds: number;
  timeoutMs: number;
  expectedStatusMin: number;
  expectedStatusMax: number;
  expectedText: string | null;
  failureThreshold: number;
  recoveryThreshold: number;
  public: boolean;
  paused: boolean;
  allowPrivateNetwork: boolean;
  health: Health;
  consecutiveFailures: number;
  consecutiveSuccesses: number;
  openIncidentId: string | null;
  lastCheckedAt: string | null;
  nextCheckAt: string;
  createdAt: string;
}

export type MonitorInput = Partial<Pick<Monitor,
  'name' | 'slug' | 'kind' | 'url' | 'simulatedMode' | 'intervalSeconds' | 'timeoutMs' | 'expectedStatusMin' |
  'expectedStatusMax' | 'expectedText' | 'failureThreshold' | 'recoveryThreshold' | 'public' | 'paused' | 'allowPrivateNetwork'>>;

export interface Check {
  id: number;
  monitorId: string;
  at: string;
  ok: boolean;
  statusCode: number | null;
  latencyMs: number;
  failure: string | null;
  tlsExpiresAt: string | null;
  warmup: boolean; // woke a sleeping service: counts for uptime, not for response times
}

export type Status = 'open' | 'in_progress' | 'resolved';
export type Severity = 'low' | 'medium' | 'high';

export interface Incident {
  id: string;
  monitorId: string | null;
  title: string;
  severity: Severity;
  status: Status;
  automatic: boolean;
  public: boolean;
  startedAt: string;
  resolvedAt: string | null;
}

export interface IncidentEvent {
  id: number;
  incidentId: string;
  at: string;
  kind: 'opened' | 'resolved' | 'status' | 'severity' | 'comment';
  message: string;
  public: boolean;
  author: string;
}

export interface IncidentDetail extends Incident { events: IncidentEvent[] }
export interface IncidentPage { incidents: Incident[]; next?: string }

export interface StatusMonitor {
  name: string;
  slug: string;
  health: Health;
  uptime24h: number | null;
  uptime7d: number | null;
  uptime90d: number | null;
  p50Ms: number | null;
  p95Ms: number | null;
  days: { date: string; uptime: number | null }[];
}
export interface StatusPage {
  overall: 'operational' | 'degraded' | 'major_outage' | 'no_monitors';
  monitors: StatusMonitor[];
  activeIncidents: IncidentDetail[];
  recentIncidents: IncidentDetail[];
  updatedAt: string;
}

export interface Report {
  since: string;
  summary: { views: number; visitors: number; engagedVisitors: number };
  pages: { path: string; project: string | null; views: number; visitors: number; engagedVisitors: number; medianEngagedSeconds: number }[];
  projects: { project: string; views: number; visitors: number; engagedVisitors: number; medianEngagedSeconds: number; launches: number; opens: number }[];
  refs: { ref: string; visitors: number; firstSeen: string; lastSeen: string; views: number; engagedSeconds: number; pages: string[]; demosOpened: number }[];
  referrers: { label: string; visitors: number }[];
  devices: { label: string; visitors: number }[];
}

export interface FieldError { field: string; message: string }

export class ApiError extends Error {
  constructor(
    public status: number, // 0: the request never got an answer
    public title: string,
    public detail?: string,
    public fields: FieldError[] = [],
    public requestId?: string,
  ) {
    super(describe(status, title, detail, requestId));
  }

  // The problems by field name, for a form to show beside its inputs.
  byField(): Record<string, string> {
    return Object.fromEntries(this.fields.map((f) => [f.field, f.message]));
  }
}

function describe(status: number, title: string, detail?: string, requestId?: string): string {
  if (status === 0) return 'No answer from the server. Check your connection; this will retry by itself.';
  if (status === 401) return 'Your session has ended. Sign in or start a sandbox again.';
  if (status === 429) return 'Too many requests. Wait a few seconds and try again.';
  if (status >= 500) return `Something went wrong on the server.${requestId ? ` If you report it, quote ${requestId}.` : ''}`;
  return detail ? `${title}: ${detail}` : title;
}

// Called when the server says the session is gone, so the app can go back to the welcome page.
let unauthorized: () => void = () => {};
export function onUnauthorized(fn: () => void) { unauthorized = fn; }

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, 'Offline');
  }
  if (!res.ok) {
    let title = res.statusText || 'Request failed';
    let detail: string | undefined;
    let fields: FieldError[] = [];
    let requestId = res.headers.get('X-Request-Id') ?? undefined;
    try {
      const p = await res.json();
      title = p.title ?? title;
      detail = p.detail;
      fields = Array.isArray(p.errors) ? p.errors : [];
      requestId = p.requestId ?? requestId;
    } catch { /* not JSON */ }
    if (res.status === 401) unauthorized();
    throw new ApiError(res.status, title, detail, fields, requestId);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export interface SessionInfo { id: string; login: string | null; device: string; createdAt: string; expiresAt: string; current: boolean }
export interface AuthEvent { id: number; at: string; kind: 'signed_in' | 'refused' | 'signed_out' | 'sessions_ended'; login: string; githubId: number | null; detail: string }
export interface Security { sessions: SessionInfo[]; events: AuthEvent[] }
export interface TestResult { ok: boolean; latencyMs: number; statusCode?: number; failure?: string }
export interface Overview { monitors: Monitor[]; stats: StatusMonitor[] }

export const api = {
  signInOptions: () => call<SignInOptions>('GET', '/api/sign-in-options'),
  session: () => call<{ signedIn: false } | ({ signedIn: true } & Me)>('GET', '/api/session'),
  startSandbox: () => call<{ role: Role; expiresInMinutes: number; resumed?: boolean }>('POST', '/api/sandbox'),
  resetSandbox: () => call<void>('POST', '/api/sandbox/reset'),
  devLogin: () => call<{ role: Role }>('POST', '/auth/dev'),
  logout: () => call<void>('POST', '/auth/logout'),
  security: () => call<Security>('GET', '/api/security'),
  endSession: (id: string) => call<void>('DELETE', `/api/sessions/${id}`),
  endOtherSessions: () => call<{ ended: number }>('POST', '/api/sessions/end-others'),

  overview: () => call<Overview>('GET', '/api/overview'),
  monitors: () => call<Monitor[]>('GET', '/api/monitors'),
  checkNow: (id: string) => call<{ monitor: Monitor; check: Check }>('POST', `/api/monitors/${id}/check`),
  testMonitor: (m: MonitorInput) => call<TestResult>('POST', '/api/monitors/test', m),
  monitor: (id: string) => call<Monitor>('GET', `/api/monitors/${id}`),
  createMonitor: (m: MonitorInput) => call<Monitor>('POST', '/api/monitors', m),
  updateMonitor: (id: string, m: MonitorInput) => call<Monitor>('PUT', `/api/monitors/${id}`, m),
  deleteMonitor: (id: string) => call<void>('DELETE', `/api/monitors/${id}`),
  setMode: (id: string, mode: Mode) => call<Monitor>('PUT', `/api/monitors/${id}/mode`, { mode }),
  checks: (id: string, before = 0) => call<Check[]>('GET', `/api/monitors/${id}/checks${before ? `?before=${before}` : ''}`),

  incidents: (cursor = '') => call<IncidentPage>('GET', `/api/incidents${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''}`),
  incident: (id: string) => call<IncidentDetail>('GET', `/api/incidents/${id}`),
  createIncident: (i: { title: string; severity: Severity; public: boolean; message: string; monitorId?: string }) =>
    call<IncidentDetail>('POST', '/api/incidents', i),
  updateIncident: (id: string, change: { status?: Status; severity?: Severity }) => call<Incident>('PATCH', `/api/incidents/${id}`, change),
  comment: (id: string, message: string, isPublic: boolean) =>
    call<IncidentEvent>('POST', `/api/incidents/${id}/comments`, { message, public: isPublic }),

  myStatus: () => call<StatusPage>('GET', '/api/my-status'),
  report: (days = 30) => call<Report>('GET', `/api/analytics?days=${days}`),
  exclusion: () => call<{ excluded: boolean }>('GET', '/api/analytics/exclusion'),
  setExclusion: (excluded: boolean) => call<{ excluded: boolean }>('PUT', '/api/analytics/exclusion', { excluded }),
};
