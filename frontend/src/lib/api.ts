// Typed client for the Go JSON API (see internal/httpserver/api.go).
// Same-origin, cookie session auth; every request sends credentials.
async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  if (res.status === 401) {
    throw new ApiError("unauthorized", 401);
  }
  let body: unknown = null;
  try {
    body = await res.json();
  } catch {
    body = null;
  }
  if (!res.ok) {
    const msg =
      typeof body === "object" && body !== null && "error" in body
        ? String((body as { error: unknown }).error)
        : `request failed (${res.status})`;
    throw new ApiError(msg, res.status);
  }
  return body as T;
}

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

export const api = {
  me: () => req<{ authenticated: boolean; username?: string; role?: string; totpEnabled?: boolean; totpRequired?: boolean }>("/api/v1/me"),
  login: (username: string, password: string) =>
    req<LoginResponse>("/api/v1/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  login2fa: (challenge: string, code: string) =>
    req<{ ok: boolean }>("/api/v1/login/2fa", {
      method: "POST",
      body: JSON.stringify({ challenge, code }),
    }),
  logout: () =>
    req<{ ok: boolean }>("/api/v1/logout", { method: "POST", body: "{}" }),

  twofaStatus: () => req<TwoFAStatus>("/api/v1/2fa/status"),
  twofaBegin: (challenge?: string) =>
    req<TwoFABegin>("/api/v1/2fa/setup/begin", {
      method: "POST",
      body: JSON.stringify(challenge ? { challenge } : {}),
    }),
  twofaConfirm: (code: string, challenge?: string) =>
    req<{ ok: boolean; recoveryCodes: string[] }>("/api/v1/2fa/setup/confirm", {
      method: "POST",
      body: JSON.stringify(challenge ? { code, challenge } : { code }),
    }),
  twofaDisable: (password: string) =>
    req<{ ok: boolean }>("/api/v1/2fa/disable", {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  twofaRegenCodes: (password: string) =>
    req<{ ok: boolean; recoveryCodes: string[] }>("/api/v1/2fa/recovery-codes/regenerate", {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  resetUser2fa: (id: number) =>
    req<{ ok: boolean }>(`/api/v1/users/${id}/2fa/reset`, {
      method: "POST",
      body: "{}",
    }),
  security: () => req<SecurityPolicy>("/api/v1/settings/security"),
  saveSecurity: (require2fa: boolean) =>
    req<SecurityPolicy>("/api/v1/settings/security", {
      method: "PUT",
      body: JSON.stringify({ require2fa }),
    }),
  ssoConfig: () => req<SSOConfig>("/api/v1/sso/config"),
  ssoSettings: () => req<SSOSettings>("/api/v1/settings/sso"),
  saveSSOSettings: (passwordLoginDisabled: boolean) =>
    req<SSOSettings>("/api/v1/settings/sso", {
      method: "PUT",
      body: JSON.stringify({ passwordLoginDisabled }),
    }),

  auditLog: (params?: { action?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.action) qs.set("action", params.action);
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return req<AuditLogData>(`/api/v1/audit-log${suffix}`);
  },

  dashboard: (range: string) =>
    req<DashboardData>(`/api/v1/dashboard?range=${encodeURIComponent(range)}`),

  issues: (params: URLSearchParams) =>
    req<IssuesData>(`/api/v1/issues?${params.toString()}`),
  issue: (id: number | string, eventId?: string) =>
    req<IssueDetail>(
      `/api/v1/issues/${id}${eventId ? `?event=${encodeURIComponent(eventId)}` : ""}`,
    ),
  issueNotes: (id: number | string) => req<IssueNote[]>(`/api/v1/issues/${id}/notes`),
  addIssueNote: (id: number | string, body: string) =>
    req<IssueNote>(`/api/v1/issues/${id}/notes`, {
      method: "POST",
      body: JSON.stringify({ body }),
    }),
  deleteIssueNote: (id: number | string, noteId: number) =>
    req<{ ok: boolean }>(`/api/v1/issues/${id}/notes/${noteId}`, {
      method: "DELETE",
    }),
  setIssueStatus: (id: number | string, status: string, mutedUntil?: string, resolveInRelease?: boolean) =>
    req<{ ok: boolean }>(`/api/v1/issues/${id}/status`, {
      method: "POST",
      body: JSON.stringify({ status, mutedUntil, resolveInRelease }),
    }),
  bulkIssueStatus: (ids: number[], status: string) =>
    req<{ ok: boolean }>("/api/v1/issues/bulk", {
      method: "POST",
      body: JSON.stringify({ ids, status }),
    }),

  projects: () => req<ProjectListItem[]>("/api/v1/projects"),
  createProject: (input: {
    name: string;
    slug: string;
    defaultEnvironment?: string;
    platform?: string;
    retentionDays?: number;
  }) =>
    req<ProjectDetail>("/api/v1/projects", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  project: (id: number | string) => req<ProjectDetail>(`/api/v1/projects/${id}`),
  testEvent: (id: number | string) =>
    req<{ issueId: number }>(`/api/v1/projects/${id}/test-event`, {
      method: "POST",
      body: "{}",
    }),
  updateProject: (
    id: number | string,
    input: {
      name?: string;
      defaultEnvironment?: string;
      platform?: string;
      retentionDays?: number;
      enabled?: boolean;
    },
  ) =>
    req<ProjectDetail>(`/api/v1/projects/${id}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    }),
  rotateProjectKey: (id: number | string) =>
    req<{ publicKey: string; dsn: string }>(`/api/v1/projects/${id}/rotate`, {
      method: "POST",
      body: "{}",
    }),
  deleteProject: (id: number | string) =>
    req<{ ok: boolean }>(`/api/v1/projects/${id}`, { method: "DELETE" }),

  platforms: () => req<Platform[]>("/api/v1/platforms"),

  projectOverview: (id: number | string, period: string) =>
    req<ProjectOverview>(`/api/v1/projects/${id}/overview?period=${encodeURIComponent(period)}`),

  projectReleases: (id: number | string) =>
    req<ReleaseHealth[]>(`/api/v1/projects/${id}/releases`),

  projectRelease: (id: number | string, release: string) =>
    req<ReleaseDetail>(`/api/v1/projects/${id}/releases/${encodeURIComponent(release)}`),

  projectLogs: (id: number | string, params?: { level?: string; q?: string; trace?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.level) qs.set("level", params.level);
    if (params?.q) qs.set("q", params.q);
    if (params?.trace) qs.set("trace", params.trace);
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return req<LogEntry[]>(`/api/v1/projects/${id}/logs${suffix}`);
  },

  projectTraces: (id: number | string, limit?: number) =>
    req<TraceTransaction[]>(`/api/v1/projects/${id}/traces${limit ? `?limit=${limit}` : ""}`),

  projectTrace: (id: number | string, traceId: string) =>
    req<TraceDetail>(`/api/v1/projects/${id}/traces/${encodeURIComponent(traceId)}`),

  projectRepo: (id: number | string) =>
    req<RepoConfig | null>(`/api/v1/projects/${id}/repo`),
  saveProjectRepo: (id: number | string, input: { provider: string; baseUrl?: string; owner: string; repo: string; token?: string }) =>
    req<RepoConfig>(`/api/v1/projects/${id}/repo`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteProjectRepo: (id: number | string) =>
    req<{ ok: boolean }>(`/api/v1/projects/${id}/repo`, { method: "DELETE" }),
  testProjectRepo: (id: number | string) =>
    req<{ ok: boolean; message: string }>(`/api/v1/projects/${id}/repo/test`, {
      method: "POST",
      body: "{}",
    }),

  issueLinks: (id: number | string) => req<IssueLink[]>(`/api/v1/issues/${id}/links`),
  createIssueLink: (id: number | string, input: { title?: string; body?: string }) =>
    req<IssueLink>(`/api/v1/issues/${id}/links`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteIssueLink: (id: number | string, linkId: number) =>
    req<{ ok: boolean }>(`/api/v1/issues/${id}/links/${linkId}`, {
      method: "DELETE",
    }),

  projectSessionSummary: (id: number | string, period: string) =>
    req<SessionSummary>(`/api/v1/projects/${id}/sessions/summary?period=${encodeURIComponent(period)}`),

  sourceMaps: (projectId: number | string) =>
    req<SourceMapMeta[]>(`/api/v1/projects/${projectId}/sourcemaps`),
  uploadSourceMap: (projectId: number | string, input: { release: string; bundleUrl: string; content: string }) =>
    req<SourceMapMeta>(`/api/v1/projects/${projectId}/sourcemaps`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteSourceMap: (projectId: number | string, mapId: number) =>
    req<{ ok: boolean }>(`/api/v1/projects/${projectId}/sourcemaps/${mapId}`, {
      method: "DELETE",
    }),

  settings: () => req<SettingsData>("/api/v1/settings"),
  saveTracing: (urlTemplate: string) =>
    req<SettingsData>("/api/v1/settings/tracing", {
      method: "PUT",
      body: JSON.stringify({ urlTemplate }),
    }),

  integrations: () => req<Integration[]>("/api/v1/integrations"),
  createIntegration: (input: IntegrationInput) =>
    req<Integration>("/api/v1/integrations", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateIntegration: (id: number, input: IntegrationInput) =>
    req<Integration>(`/api/v1/integrations/${id}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteIntegration: (id: number) =>
    req<{ ok: boolean }>(`/api/v1/integrations/${id}`, {
      method: "DELETE",
    }),
  testIntegration: (id: number, routingOverride?: RoutingOverride) =>
    req<{ ok: boolean; message: string }>(
      `/api/v1/integrations/${id}/test`,
      { method: "POST", body: JSON.stringify({ routingOverride: routingOverride ?? {} }) },
    ),

  storage: () => req<StorageUsage>("/api/v1/storage"),
  storageSweep: () =>
    req<{ deleted: number; byProject: Record<string, number> }>(
      "/api/v1/storage/sweep",
      { method: "POST", body: "{}" },
    ),
  storageVacuum: () =>
    req<{ ok: boolean }>("/api/v1/storage/vacuum", {
      method: "POST",
      body: "{}",
    }),
  storageSummary: () => req<StorageSummary>("/api/v1/storage/summary"),
  storageProjects: () => req<ProjectStorage[]>("/api/v1/storage/projects"),
  storageAge: () => req<StorageAge>("/api/v1/storage/age"),
  storageRetention: () => req<RetentionResponse>("/api/v1/storage/retention"),
  saveRetention: (input: RetentionSettings) =>
    req<RetentionResponse>("/api/v1/storage/retention", {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  previewRetention: (settings: RetentionSettings, projectId?: number) =>
    req<Record<string, number>>("/api/v1/storage/retention/preview", {
      method: "POST",
      body: JSON.stringify(projectId ? { settings, projectId } : { settings }),
    }),
  runRetention: () =>
    req<CleanupStatus>("/api/v1/storage/retention/run", {
      method: "POST",
      body: "{}",
    }),
  cleanupStatus: () => req<CleanupStatus>("/api/v1/storage/cleanup/status"),
  compaction: () => req<CompactionInfo>("/api/v1/storage/compaction"),
  compact: () =>
    req<CleanupStatus>("/api/v1/storage/compaction", {
      method: "POST",
      body: "{}",
    }),

  rules: (projectId?: number | null) => {
    const qs = projectId === undefined ? "" : `?project_id=${projectId === null ? "null" : projectId}`;
    return req<AlertRule[]>(`/api/v1/rules${qs}`);
  },
  createRule: (input: AlertRuleInput) =>
    req<AlertRule>("/api/v1/rules", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateRule: (id: number, input: AlertRuleInput) =>
    req<AlertRule>(`/api/v1/rules/${id}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteRule: (id: number) =>
    req<{ ok: boolean }>(`/api/v1/rules/${id}`, { method: "DELETE" }),

  apiKeys: () => req<APIKey[]>("/api/v1/api-keys"),
  createAPIKey: (input: { name: string; projectId?: number | null; scopes?: string[]; expiresAt?: string }) =>
    req<APIKey>("/api/v1/api-keys", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  rotateAPIKey: (id: number) =>
    req<APIKey>(`/api/v1/api-keys/${id}/rotate`, {
      method: "POST",
      body: "{}",
    }),
  revokeAPIKey: (id: number) =>
    req<{ ok: boolean }>(`/api/v1/api-keys/${id}/revoke`, {
      method: "POST",
      body: "{}",
    }),
  deleteAPIKey: (id: number) =>
    req<{ ok: boolean }>(`/api/v1/api-keys/${id}`, { method: "DELETE" }),

  users: () => req<User[]>("/api/v1/users"),
  createUser: (input: { username: string; password: string; role?: string; email?: string }) =>
    req<User>("/api/v1/users", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteUser: (id: number) =>
    req<{ ok: boolean }>(`/api/v1/users/${id}`, { method: "DELETE" }),
  setUserRole: (id: number, role: string) =>
    req<{ ok: boolean }>(`/api/v1/users/${id}/role`, {
      method: "PUT",
      body: JSON.stringify({ role }),
    }),
  setUserPassword: (id: number, password: string) =>
    req<{ ok: boolean }>(`/api/v1/users/${id}/password`, {
      method: "PUT",
      body: JSON.stringify({ password }),
    }),

  teams: () => req<Team[]>("/api/v1/teams"),
  createTeam: (input: { name: string; slug?: string }) =>
    req<Team>("/api/v1/teams", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteTeam: (id: number) =>
    req<{ ok: boolean }>(`/api/v1/teams/${id}`, { method: "DELETE" }),
  addTeamMember: (teamId: number, userId: number) =>
    req<{ ok: boolean }>(`/api/v1/teams/${teamId}/members`, {
      method: "POST",
      body: JSON.stringify({ userId }),
    }),
  removeTeamMember: (teamId: number, userId: number) =>
    req<{ ok: boolean }>(`/api/v1/teams/${teamId}/members/${userId}`, {
      method: "DELETE",
    }),
  grantTeamProject: (teamId: number, projectId: number) =>
    req<{ ok: boolean }>(`/api/v1/teams/${teamId}/projects`, {
      method: "POST",
      body: JSON.stringify({ projectId }),
    }),
  revokeTeamProject: (teamId: number, projectId: number) =>
    req<{ ok: boolean }>(`/api/v1/teams/${teamId}/projects/${projectId}`, {
      method: "DELETE",
    }),

  projectAccess: (id: number | string) =>
    req<ProjectAccess>(`/api/v1/projects/${id}/access`),
  projectQuota: (id: number | string) =>
    req<ProjectQuota>(`/api/v1/projects/${id}/quota`),
  saveProjectQuota: (id: number | string, input: { hourly?: number | null; daily?: number | null }) =>
    req<ProjectQuota>(`/api/v1/projects/${id}/quota`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  quotaDefaults: () => req<QuotaDefaults>("/api/v1/settings/quotas"),
  saveQuotaDefaults: (input: { defaultHourly: number; defaultDaily: number }) =>
    req<QuotaDefaults>("/api/v1/settings/quotas", {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  grantProjectMember: (id: number | string, userId: number) =>
    req<{ ok: boolean }>(`/api/v1/projects/${id}/access/members`, {
      method: "POST",
      body: JSON.stringify({ userId }),
    }),
  revokeProjectMember: (id: number | string, userId: number) =>
    req<{ ok: boolean }>(`/api/v1/projects/${id}/access/members/${userId}`, {
      method: "DELETE",
    }),

  invitations: () => req<Invitation[]>("/api/v1/invitations"),
  createInvitation: (input: { email: string; username: string; role?: string }) =>
    req<Invitation>("/api/v1/invitations", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  revokeInvitation: (token: string) =>
    req<{ ok: boolean }>(`/api/v1/invitations/${token}`, { method: "DELETE" }),
  getInvite: (token: string) => req<InviteInfo>(`/api/v1/invite/${token}`),
  acceptInvite: (input: { token: string; username?: string; password: string }) =>
    req<{ ok: boolean; username: string }>("/api/v1/invite/accept", {
      method: "POST",
      body: JSON.stringify(input),
    }),
};

export interface Point {
  label: string;
  value: number;
}

export interface DashboardData {
  range: string;
  stats: {
    totalEvents: number;
    unresolvedIssues: number;
    newIssues: number;
    affectedProjects: number;
    eventsToday: number;
    uniqueIssues: number;
    eventsDeltaPct: number;
    eventsDeltaLabel: string;
  };
  charts: {
    eventsOverTime: Point[];
    byProject: Point[];
    topExceptions: Point[];
    byEnvironment: Point[];
    newVsRecurring: { new: number; recurring: number };
    byStatus: Point[];
  };
  recentIssues: {
    id: number;
    title: string;
    level: string;
    projectName: string;
    environment: string;
    eventCount: number;
    lastSeen: string;
    lastSeenRelative: string;
  }[];
  hasData: boolean;
  hasProjects: boolean;
}

export interface IssuesData {
  issues: {
    id: number;
    title: string;
    culprit: string;
    level: string;
    status: string;
    projectName: string;
    environment: string;
    eventCount: number;
    userCount: number;
    lastSeen: string;
    lastSeenRelative: string;
    sparkline: number[];
  }[];
  total: number;
  page: number;
  totalPages: number;
  projects: { id: number; name: string }[];
  environments: string[];
}

export interface KeyValue {
  key: string;
  value: string;
}

export interface IssueNote {
  id: number;
  issueId: number;
  body: string;
  createdAt: string;
}

export interface IssueDetail {
  issue: {
    id: number;
    title: string;
    exceptionType: string;
    exceptionValue: string;
    culprit: string;
    level: string;
    status: string;
    mutedUntil: string | null;
    resolvedInRelease: string;
    eventCount: number;
    userCount: number;
    firstSeen: string;
    lastSeen: string;
    environment: string;
    release: string;
  };
  project: { id: number; name: string };
  tags: KeyValue[];
  user: { username: string; extra: KeyValue[] } | null;
  sdk: { name: string; integrations: string[] } | null;
  request: { url: string; queryParams: KeyValue[] } | null;
  contexts: { name: string; fields: KeyValue[] }[];
  breadcrumbs: {
    timestamp: string;
    type: string;
    level: string;
    message: string;
    data: Record<string, string>;
  }[];
  stacktrace: {
    file: string;
    line: number;
    col: number;
    function: string;
    module: string;
    contextLine: string;
    preContext: string[];
    postContext: string[];
    vars: KeyValue[];
    inApp: boolean;
    original: {
      source: string;
      line: number;
      column: number;
      function?: string;
      context?: string[];
    } | null;
  }[];
  events: { eventId: string; timestamp: string; message: string }[];
  latestEventId: string | null;
  currentEventId: string | null;
  prevEventId: string | null;
  nextEventId: string | null;
  rawEvent: Record<string, unknown> | null;
  notes: IssueNote[];
  links: IssueLink[];
  traceId: string;
  traceURL: string;
}

export interface ProjectListItem {
  id: number;
  name: string;
  slug: string;
  defaultEnvironment: string;
  enabled: boolean;
  retentionDays: number;
  platformLabel: string;
  events7d: number;
  unresolved: number;
}

export interface ProjectDetail {
  project: {
    id: number;
    name: string;
    slug: string;
    defaultEnvironment: string;
    enabled: boolean;
    retentionDays: number;
    platform: string;
    createdAt: string;
  };
  dsn: string;
  stats: { events7d: number; unresolved: number };
  environments: string[];
  platformLabel: string;
  platformInstall: string;
  platformSnippet: string;
  platforms: Platform[];
}

export interface Platform {
  id: string;
  label: string;
  language: string;
}

export interface ProjectOverview {
  period: string;
  events: number;
  newIssues: number;
  openIssues: number;
  volumeBySeverity: { bucket: string; counts: Record<string, number> }[];
  topIssues: {
    id: number;
    title: string;
    level: string;
    status: string;
    eventCount: number;
    lastSeen: string;
    lastSeenRelative: string;
  }[];
}

export interface ReleaseHealth {
  release: string;
  events: number;
  issues: number;
  sessions: number;
  users: number;
  transactions: number;
  crashFreeSessions: number;
  crashFreeUsers: number;
  firstSeen: string;
  lastSeen: string;
}

export interface ReleaseDetail {
  release: string;
  summary: ReleaseHealth;
  windowStart: string;
  windowEnd: string | null;
  environments: { environment: string; events: number; sessions: number }[];
  newIssues: {
    id: number;
    title: string;
    level: string;
    status: string;
    eventCount: number;
    firstSeen: string;
    firstSeenRelative: string;
    lastSeenRelative: string;
    environment: string;
  }[];
}

export interface SessionSummary {
  period: string;
  summary: {
    totalSessions: number;
    users: number;
    crashFreeSessions: number;
    crashFreeUsers: number;
  };
  timeseries: { bucket: string; healthy: number; crashed: number }[];
}

export interface SourceMapMeta {
  id: number;
  release: string;
  bundleUrl: string;
  sizeBytes: number;
  createdAt: string;
}

export interface SettingsData {
  traceURLTemplate: string;
}

export interface StorageUsage {
  driver: string;
  sizeBytes: number;
  tables: { name: string; rows: number }[];
}

export interface AlertRule {
  id: number;
  name: string;
  projectId: number | null;
  environment: string;
  notifyNewIssue: boolean;
  notifyThreshold: number;
  notifyReopened: boolean;
  cooldownSeconds: number;
  integrations: RuleIntegration[];
  channels?: string[];
}

export interface RuleIntegration {
  integrationId: number;
  provider: string;
  integrationName: string;
  isEnabled: boolean;
  routingOverride: RoutingOverride;
}

export interface RoutingOverride {
  chat_id?: string;
  url?: string;
  to?: string[];
  extra_headers?: Record<string, string>;
}

export interface AlertRuleInput {
  name: string;
  projectId?: number | null;
  environment?: string;
  notifyNewIssue?: boolean;
  notifyThreshold?: number;
  notifyReopened?: boolean;
  cooldownSeconds?: number;
  integrations?: { integrationId: number; routingOverride?: RoutingOverride }[];
  channels?: string[];
}

export interface Integration {
  id: number;
  name: string;
  provider: string;
  enabled: boolean;
  credentials: Record<string, unknown>;
  failureCount: number;
  lastFailure: string;
}

export interface IntegrationInput {
  name: string;
  provider: string;
  enabled: boolean;
  credentials: Record<string, unknown>;
}

export interface SMTPSettings {
  host: string;
  port: number;
  username: string;
  passwordMasked: string;
  from: string;
  to: string;
  useTLS: boolean;
  enabled: boolean;
}

export interface GenericWebhook {
  id: number;
  name: string;
  url: string;
  hasSecret: boolean;
  headers: Record<string, string>;
  enabled: boolean;
}

export interface APIKey {
  id: number;
  name: string;
  prefix: string;
  projectId: number | null;
  scopes: string[];
  expiresAt: string;
  revoked: boolean;
  lastUsedAt: string;
  createdAt: string;
  secret?: string;
}

export interface User {
  id: number;
  username: string;
  role: string;
  createdAt: string;
  totpEnabled: boolean;
  email: string;
  sso: boolean;
  lastLogin: string;
  isPrimary: boolean;
}

export interface Invitation {
  token: string;
  email: string;
  username: string;
  role: string;
  status: string;
  expiresAt: string;
  createdAt: string;
  acceptedAt?: string;
  invitedBy?: number;
}

export interface InviteInfo {
  email: string;
  username: string;
  role: string;
  expiresAt: string;
}

export interface RetentionWindows {
  events_days: number | null;
  transactions_days: number | null;
  logs_days: number | null;
}

export interface RetentionSettings {
  defaults: RetentionWindows;
  overrides: { project_id: number; events_days: number | null; transactions_days: number | null; logs_days: number | null }[];
}

export interface RetentionRun {
  started_at: string;
  finished_at?: string;
  status: string;
  removed?: Record<string, number>;
  error?: string;
}

export interface RetentionResponse {
  settings: RetentionSettings;
  next_run_at?: string;
  last_run?: RetentionRun;
}

export interface StorageSummary {
  bytes: number;
  events_rows: number;
  transactions_rows: number;
  logs_rows: number;
  source_maps: { file_count: number; total_bytes: number };
  frees_estimate: number;
}

export interface ProjectStorage {
  id: number;
  name: string;
  bytes: number;
  events_bytes: number;
  transactions_bytes: number;
  logs_bytes: number;
  events_count: number;
  transactions_count: number;
  logs_count: number;
  share: number;
  custom: boolean;
}

export interface StorageAge {
  bounds: number[];
  buckets: { events: number; transactions: number; logs: number }[];
}

export interface CleanupStatus {
  state: string;
  kind: string;
  started_at?: string;
  removed?: Record<string, number>;
  error?: string;
}

export interface CompactionInfo {
  needed: boolean;
  freeBytes?: number;
  sizeBytes?: number;
  reason?: string;
}

export interface LoginResponse {
  ok?: boolean;
  twoFactorRequired?: boolean;
  enrollRequired?: boolean;
  challenge?: string;
}

export interface TwoFAStatus {
  enabled: boolean;
  required: boolean;
  recoveryCodesRemaining: number;
}

export interface TwoFABegin {
  otpauthUrl: string;
  qrDataUri: string;
  secret: string;
}

export interface SecurityPolicy {
  require2fa: boolean;
}

export interface SSOConfig {
  enabled: boolean;
  name: string;
  issuer: string;
  passwordLoginDisabled: boolean;
}

export interface SSOSettings {
  enabled: boolean;
  name: string;
  issuer: string;
  passwordLoginDisabled: boolean;
}

export interface AuditLogEntry {
  id: number;
  createdAt: string;
  actorUserId: number;
  actorUsername: string;
  action: string;
  entityType: string;
  entityId: string;
  detail: string;
  ip: string;
}

export interface AuditLogData {
  entries: AuditLogEntry[];
  total: number;
}

export interface Team {
  id: number;
  name: string;
  slug: string;
  createdAt: string;
  memberIds: number[];
  projectIds: number[];
}

export interface ProjectAccess {
  memberIds: number[];
  teams: { id: number; name: string; slug: string }[];
}

export interface ProjectQuota {
  projectId: number;
  hourly: number;
  daily: number;
  hourlyOverride: number | null;
  dailyOverride: number | null;
  usedHour: number;
  usedDay: number;
  breachedHour: boolean;
  breachedDay: boolean;
}

export interface QuotaDefaults {
  defaultHourly: number;
  defaultDaily: number;
}

export interface LogEntry {
  id: number;
  timestamp: string;
  level: string;
  message: string;
  traceId: string;
  spanId: string;
  release: string;
  environment: string;
  attributes: Record<string, unknown>;
}

export interface TraceTransaction {
  id: number;
  eventId: string;
  name: string;
  traceId: string;
  spanId: string;
  status: string;
  release: string;
  environment: string;
  start: string;
  end: string;
  durationMs: number;
  spanCount: number;
}

export interface TraceSpan {
  id: number;
  traceId: string;
  spanId: string;
  parentSpanId: string;
  op: string;
  description: string;
  status: string;
  start: string;
  end: string;
  durationMs: number;
}

export interface TraceDetail {
  traceId: string;
  transactions: TraceTransaction[];
  spans: TraceSpan[];
  logs: LogEntry[];
}

export interface RepoConfig {
  provider: string;
  baseUrl: string;
  effectiveBaseUrl: string;
  owner: string;
  repo: string;
  slug: string;
  hasToken: boolean;
  updatedAt: string;
}

export interface IssueLink {
  id: number;
  issueId: number;
  provider: string;
  repo: string;
  number: number;
  url: string;
  title: string;
  createdAt: string;
}
