import { currentLang, text, textError } from "./i18n";

export class ApiError extends Error {
  // code is the server's name for the error, when it gives one.
  code?: string;
  constructor(message: string, code?: string) {
    super(message);
    this.code = code;
  }
}

const authPaths = ["/api/setup", "/api/session", "/api/session/totp", "/api/password"];

// Last answer of each read, kept for the tab: a page opened again shows what
// it had at once, then refreshes, instead of an empty skeleton every time.
const answers = new Map<string, unknown>();

const returnKey = "myinfra.return";

// returnPath is where to go back after signing in again: the page the person
// was on when the session ran out.
export function returnPath() {
  try {
    const path = sessionStorage.getItem(returnKey);
    sessionStorage.removeItem(returnKey);
    if (path && path.startsWith("/") && !path.startsWith("//") && !path.startsWith("/entree")) return path;
  } catch {
    // Storage blocked: back to the start.
  }
  return "/";
}

export function peek<T>(path: string): T | null {
  return (answers.get(path) as T | undefined) ?? null;
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const read = !init?.method || init.method === "GET";
  if (!read && path === "/api/session") answers.clear();
  const headers = new Headers(init?.headers);
  headers.set("Accept-Language", currentLang());
  if (init?.body) {
    headers.set("content-type", "application/json");
  }
  let res: Response;
  try {
    res = await fetch(path, { ...init, headers });
  } catch {
    throw new ApiError(text("err.network"));
  }
  if (res.status === 204) {
    return undefined as T;
  }
  const raw = await res.text();
  let body: any = {};
  if (raw) {
    try {
      body = JSON.parse(raw);
    } catch {
      throw new ApiError(textError(undefined, undefined, ""));
    }
  }
  if (!res.ok) {
    if (res.status === 401) answers.clear();
    if (res.status === 401 && !authPaths.includes(path) && !location.pathname.startsWith("/entree")) {
      try {
        sessionStorage.setItem(returnKey, location.pathname + location.search);
      } catch {
        // Storage blocked: the person starts again from the home page.
      }
      location.assign("/entree");
    }
    throw new ApiError(textError(body.code, body.params, body.error || ""), body.code);
  }
  if (read) answers.set(path, body);
  return body as T;
}

export type LocalImage = {
  id: string;
  ref: string;
  name: string;
  tag: string;
  source: "upload" | "url";
  sourceUrl?: string;
  size: number;
  platform?: string;
  ports: number[];
  volumes: string[];
  usedBy: string[];
  createdAt: string;
};

export type Secret = {
  id: string;
  kind: string;
  name: string;
  provider?: string;
  fingerprint?: string;
  publicKey?: string;
  hint?: string;
  keyId?: string;
  hasPrivate: boolean;
  createdAt: string;
};

export type StorageOption = {
  id: string;
  label: string;
  minGb: number;
  maxGb: number;
  hourlyPerGb?: string;
  monthlyPerGb?: string;
  required?: boolean;
};

export type Offer = {
  id: string;
  monthlyId?: string;
  name: string;
  location: string;
  vcpu: number;
  memoryMb: number;
  diskGb: number;
  hourly?: string;
  monthly?: string;
  storage?: StorageOption[];
};

export type Project = { id: string; name: string };

export type Catalog = {
  provider: string;
  currency: string;
  location: string;
  locations: { id: string; label: string }[];
  periods: string[];
  note: string;
  offers: Offer[];
};

export type Plan = {
  offerId: string;
  name: string;
  location: string;
  period: string;
  hourly?: string;
  monthly?: string;
  vcpu: number;
  memoryMb: number;
  diskGb: number;
  storageId?: string;
  storageGb?: number;
  storageLabel?: string;
  projectId?: string;
  currency: string;
};

export type Server = {
  id: string;
  name: string;
  status: string;
  clusterId?: string;
  joinCommand: string;
  address?: string;
  providerId?: string;
  provider?: string;
  providerName?: string;
  clusterName?: string;
  controlPlane?: boolean;
  tofu?: boolean;
  plan?: Plan;
  deploy?: DeployStep[];
  cost?: Cost;
  createdAt?: string;
  // reach is "lost" when the server stopped answering, "gone" when its
  // provider no longer has it.
  reach?: "lost" | "gone";
  lostSince?: string;
  seenAt?: string;
  providerCheckedAt?: string;
  providerError?: string;
};

export type Cost = {
  amount: string;
  currency: string;
  theoretical: boolean;
};

export type InfraEvent = {
  id: string;
  at: string;
  kind: string;
  level: string;
  message: string;
  serverId?: string;
  serverName?: string;
  clusterId?: string;
  clusterName?: string;
};

export type DeployStep = {
  id: string;
  label: string;
  status: string;
  detail?: string;
};

export type Cluster = { id: string; name: string; origin?: string };

export type Gauge = { used: number; total: number };

export type ServerUsage = { id: string; cpu: Gauge; memory: Gauge; disk: Gauge; measured: boolean };

export type ClusterUsage = {
  measured: boolean;
  cpu: Gauge;
  memory: Gauge;
  disk: Gauge;
  servers: ServerUsage[];
};

export type PodRuntime = {
  name: string;
  server: string;
  phase: string;
  reason: string;
  ready: boolean;
  cpu: number;
  memory: number;
  disk: number;
};

export type AppRuntime = {
  measured: boolean;
  status?: string;
  cpu: Gauge;
  memory: Gauge;
  disk: Gauge;
  pods: PodRuntime[];
};

export type ServerPod = PodRuntime & { application: string; appId: string; service: string };

export type ServerRuntime = {
  measured: boolean;
  cpu: Gauge;
  memory: Gauge;
  disk: Gauge;
  pods: ServerPod[];
};

export type AppService = {
  name: string;
  image: string;
  published: boolean;
  disk: boolean;
  replicas?: number;
};

export type ArgoApp = { name: string; namespace: string; sync: string; health: string };

export type Domain = {
  hostname: string;
  kind: string;
  status: string;
  zone?: string;
  record?: string;
  detail?: string;
};

export type DnsZone = { name: string; accountId: string; account: string; provider: string };

export type Application = {
  id: string;
  name: string;
  sourceKind: string;
  source: string;
  destinationKind: string;
  serverId?: string;
  serverName?: string;
  clusterId?: string;
  clusterName?: string;
  hostname: string;
  runStatus?: string;
  replicas?: number;
  variables?: Record<string, string>;
  domains?: Domain[];
  services?: AppService[];
  rollout?: string;
  rolloutDetail?: string;
  runDetail?: string;
  failover?: boolean;
  guarded?: boolean;
  paused?: boolean;
  logo?: string;
};

export type OverviewServer = Server & { usage?: ServerUsage };

export type OverviewPod = {
  name: string;
  appId: string;
  serverId?: string;
  service?: string;
  phase: string;
  reason?: string;
  ready: boolean;
};

export type Overview = {
  servers: OverviewServer[];
  apps: Application[];
  pods: OverviewPod[];
};

export type AppSecretApp = { id: string; name: string };

export type AppSecret = {
  id: string;
  name: string;
  keyCount?: number;
  keys?: string[];
  apps: AppSecretApp[];
};

export type CatalogueInput = { key: string; label: string };

export type CatalogueService = {
  name: string;
  image: string;
  published: boolean;
  disk: boolean;
};

export type CatalogueCard = {
  id: string;
  name: string;
  summary: string;
  description: string;
  site: string;
  logo: string;
  category: string;
  keepsFiles: boolean;
  inputs: CatalogueInput[];
  services: CatalogueService[];
};

export type CatalogueGroup = { name: string; articles: CatalogueCard[] };

export type CatalogueVersions = {
  name: string;
  image: string;
  tags: string[];
  detail?: string;
};

export type ReleaseService = {
  name: string;
  image: string;
  update?: string;
  tags: string[];
  detail?: string;
};

export type TemplateServer = {
  name: string;
  providerId?: string;
  provider?: string;
  providerName?: string;
  location?: string;
  projectId?: string;
  offerId?: string;
  period?: string;
  storageId?: string;
  storageGb?: number;
  keyIds?: string[];
  keyNames?: string[];
  controlPlane?: boolean;
};

export type TemplateName = { subdomain: string; zone: string };

export type TemplateApp = {
  // catalog is the catalogue article the app comes from.
  catalog?: string;
  name: string;
  sourceKind: string;
  source: string;
  replicas?: number;
  variables?: Record<string, string>;
  services?: AppService[];
  names?: TemplateName[];
};

export type TemplateBody = {
  clusterName?: string;
  servers?: TemplateServer[];
  applications?: TemplateApp[];
};

export type Template = { id: string; name: string; body: TemplateBody; createdAt: string };

export type RunStep = {
  kind: string;
  name: string;
  status: string;
  detail?: string;
  resourceId?: string;
  deploy?: DeployStep[];
};

export type TemplateRun = {
  id: string;
  templateId: string;
  status: string;
  clusterId?: string;
  steps: RunStep[];
  createdAt: string;
};

export type DestructionStep = {
  label: string;
  status: string;
  detail?: string;
};

export type Deployment = {
  id: string;
  kind: string;
  name: string;
  status: string;
  step?: string;
  to: string;
};

export type Destruction = {
  id: string;
  kind: string;
  targetId?: string;
  name: string;
  status: string;
  steps?: DestructionStep[];
  createdAt: string;
};

export type UpdateKind = "app" | "server" | "myinfra";

export type UpdateLevel = "patch" | "minor" | "major" | "build" | "security" | "reboot" | "packages";

export type ServiceUpdate = { name: string; image: string; next: string; level: UpdateLevel };

export type ServerUpdateDetail = {
  packages: number;
  security: number;
  reboot: boolean;
  system?: string;
  kernel?: string;
  k3s?: string;
  k3sNext?: string;
};

export type MyInfraUpdateDetail = { url: string; notes: string; published: string; prerelease: boolean };

export type UpdateItem = {
  kind: UpdateKind;
  id: string;
  name: string;
  current: string;
  available: string;
  level: UpdateLevel;
  detail: { services?: ServiceUpdate[] } & Partial<ServerUpdateDetail> & Partial<MyInfraUpdateDetail>;
  checkedAt: string;
};

export type VersionView = {
  current: string;
  latest?: string;
  url?: string;
  method: "local" | "compose" | "pod" | "manual";
  canUpdate: boolean;
  reason?: string;
  updating?: string;
  image?: string;
};

export type UpdatesView = {
  updates: UpdateItem[];
  checking: boolean;
  checkedAt?: string;
  version: VersionView;
};

export type ImpactEffect = "none" | "short" | "down";

export type Impact = {
  effect: ImpactEffect;
  apps: { id: string; name: string; effect: ImpactEffect; reason: string }[];
  notes: string[];
};

export type UpdateJobStep = {
  action: "check" | "drain" | "packages" | "k3s" | "reboot" | "ready" | "uncordon" | "settle";
  serverId: string;
  name: string;
  k3s?: string;
  status: "pending" | "running" | "done" | "failed" | "skipped";
  detail?: string;
};

export type UpdateJob = {
  id: string;
  clusterId?: string;
  serverId?: string;
  name: string;
  status: "running" | "failed" | "done" | "cancelled";
  steps: UpdateJobStep[];
  createdAt: string;
  finishedAt?: string;
};
