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
  me: () => req<{ authenticated: boolean; username?: string }>("/api/v1/me"),
  login: (username: string, password: string) =>
    req<{ ok: boolean }>("/api/v1/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () =>
    req<{ ok: boolean }>("/api/v1/logout", { method: "POST", body: "{}" }),

  dashboard: (range: string) =>
    req<DashboardData>(`/api/v1/dashboard?range=${encodeURIComponent(range)}`),

  issues: (params: URLSearchParams) =>
    req<IssuesData>(`/api/v1/issues?${params.toString()}`),
  issue: (id: number | string) => req<IssueDetail>(`/api/v1/issues/${id}`),
  setIssueStatus: (id: number | string, status: string) =>
    req<{ ok: boolean }>(`/api/v1/issues/${id}/status`, {
      method: "POST",
      body: JSON.stringify({ status }),
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
  saveTelegram: (input: { botToken: string; chatId: string; enabled: boolean }) =>
    req<SettingsData>("/api/v1/settings/telegram", {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  testTelegram: () =>
    req<{ ok: boolean; message: string }>("/api/v1/settings/telegram/test", {
      method: "POST",
      body: "{}",
    }),
  saveWebhook: (channel: string, input: { url: string; enabled: boolean }) =>
    req<SettingsData>(`/api/v1/settings/webhooks/${channel}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  testWebhook: (channel: string) =>
    req<{ ok: boolean; message: string }>(
      `/api/v1/settings/webhooks/${channel}/test`,
      { method: "POST", body: "{}" },
    ),
  saveTracing: (urlTemplate: string) =>
    req<SettingsData>("/api/v1/settings/tracing", {
      method: "PUT",
      body: JSON.stringify({ urlTemplate }),
    }),

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

  rules: () => req<AlertRule[]>("/api/v1/rules"),
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

  saveSMTP: (input: {
    host: string;
    port: number;
    username: string;
    password: string;
    from: string;
    to: string;
    useTLS: boolean;
    enabled: boolean;
  }) =>
    req<SettingsData>("/api/v1/settings/smtp", {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  testSMTP: () =>
    req<{ ok: boolean; message: string }>("/api/v1/settings/smtp/test", {
      method: "POST",
      body: "{}",
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

export interface IssueDetail {
  issue: {
    id: number;
    title: string;
    exceptionType: string;
    exceptionValue: string;
    culprit: string;
    level: string;
    status: string;
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
}

export interface SourceMapMeta {
  id: number;
  release: string;
  bundleUrl: string;
  sizeBytes: number;
  createdAt: string;
}

export interface SettingsData {
  telegram: { botToken: string; chatId: string; enabled: boolean };
  botTokenMasked: string;
  webhooks: Record<string, { urlMasked: string; enabled: boolean }>;
  smtp: SMTPSettings;
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
  channels: string[];
}

export interface AlertRuleInput {
  name: string;
  projectId?: number | null;
  environment?: string;
  notifyNewIssue?: boolean;
  notifyThreshold?: number;
  notifyReopened?: boolean;
  cooldownSeconds?: number;
  channels?: string[];
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
