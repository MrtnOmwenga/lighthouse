// The console's view of Lighthouse's JSON API. Every call is same-origin with the session cookie;
// errors come back as RFC 9457 problem details and are thrown as ApiError.

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

export class ApiError extends Error {
  constructor(public status: number, public title: string, public detail?: string) {
    super(detail ? `${title}: ${detail}` : title);
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    let title = res.statusText || 'Request failed';
    let detail: string | undefined;
    try {
      const p = await res.json();
      title = p.title ?? title;
      detail = p.detail;
    } catch { /* not JSON */ }
    throw new ApiError(res.status, title, detail);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const api = {
  signInOptions: () => call<SignInOptions>('GET', '/api/sign-in-options'),
  session: () => call<{ signedIn: false } | ({ signedIn: true } & Me)>('GET', '/api/session'),
  startSandbox: () => call<{ role: Role; expiresInMinutes: number }>('POST', '/api/sandbox'),
  devLogin: () => call<{ role: Role }>('POST', '/auth/dev'),
  logout: () => call<void>('POST', '/auth/logout'),

  monitors: () => call<Monitor[]>('GET', '/api/monitors'),
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
