export type Session = { username: string; csrf: string; expires_at: string };
export type Exit = {
  id: string;
  name: string;
  country: string;
  policy: string;
  port: number;
  listen: string;
  username: string;
  status: 'stopped' | 'blocked';
  reason: string;
  created_at: string;
};
export type ExitInput = { name: string; country: string; policy: string; port?: number };
export type Overview = {
  version: string;
  mode: string;
  exit_count: number;
  active_count: number;
  country_count: number;
  node_count: number;
  executor_available: boolean;
  protection_verified: boolean;
  message: string;
};
export type Node = {
  id: string;
  hostname: string;
  ip: string;
  country: string;
  country_name: string;
  advertised_ping: number;
  advertised_speed: number;
  sessions: number;
  type: string;
  status: string;
  source: string;
  fetched_at: string;
};
export type Event = {
  id: number;
  level: string;
  kind: string;
  message: string;
  exit_id?: string;
  created_at: string;
};
export type Diagnostics = {
  platform: string;
  sandbox_plan_available: boolean;
  sandbox_helper_supported: boolean;
  executor_available: boolean;
  checks: { id: string; name: string; status: string; detail: string }[];
};
let csrf = '';
export function setCSRF(value: string) {
  csrf = value;
}
export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}
export async function api<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const response = await fetch(`/api${path}`, {
    method,
    credentials: 'same-origin',
    signal: AbortSignal.timeout(35000),
    headers: {
      'Content-Type': 'application/json',
      ...(method !== 'GET' ? { 'X-CSRF-Token': csrf } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 204) return undefined as T;
  let data;
  try {
    data = await response.json();
  } catch {
    throw new APIError(
      response.status,
      'invalid_response',
      '服务返回了无效响应，请检查后端是否运行。',
    );
  }
  if (!response.ok) {
    if (response.status === 401 && path !== '/auth/login')
      window.dispatchEvent(new Event('miku:session-expired'));
    throw new APIError(response.status, data.code ?? 'request_failed', data.message ?? '请求失败');
  }
  return data as T;
}
export const countries: Record<string, string> = {
  JP: '日本',
  KR: '韩国',
  US: '美国',
  SG: '新加坡',
  HK: '中国香港',
  TW: '中国台湾',
  GB: '英国',
  DE: '德国',
  FR: '法国',
  CA: '加拿大',
  NL: '荷兰',
  AU: '澳大利亚',
};
export const policies: Record<string, string> = {
  any: '不限类型',
  residential_preferred: '住宅优先',
  residential_strict: '严格住宅',
};
export function timestamp(value: string) {
  return new Date(value).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}
export function filterExits(exits: Exit[], query: string, country: string) {
  const q = query.trim().toLowerCase();
  return exits.filter(
    (e) =>
      (!country || e.country === country) &&
      `${e.name} ${e.country} ${countries[e.country] ?? ''} ${e.port}`.toLowerCase().includes(q),
  );
}

export type NetworkPlan = {
  schema: number;
  instance_id: string;
  exit_id: string;
  stage: string;
  namespace: string;
  nft_table: string;
  owner: string;
  nft_ruleset: string;
  host_routes_changed: boolean;
  host_firewall_changed: boolean;
  tunnel_enabled: boolean;
  proxy_enabled: boolean;
  changes: { scope: string; action: string; description: string }[];
  warnings: string[];
};

export type ExecutorStatus = {
  configured: boolean;
  reachable: boolean;
  mode: string;
  vpn_available: boolean;
  proxy_available: boolean;
  message: string;
};
export type SandboxState = {
  plan: NetworkPlan;
  phase: string;
  quarantine_verified: boolean;
  tunnel_ready: boolean;
  proxy_listening: boolean;
  reason: string;
};
