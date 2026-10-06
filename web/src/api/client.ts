export type Repository = {
  id: number;
  name: string;
  sourceType: string;
  remoteUrl?: string;
  defaultRef: string;
  headCommit: string;
  status: 'pending' | 'analyzing' | 'ready' | 'failed';
  failure?: string;
  createdAt: string;
  updatedAt: string;
  lastAnalyzed?: string;
};

export type AnalysisJob = {
  id: number;
  repositoryId: number;
  requestedRef: string;
  resolvedCommit: string;
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';
  totalCommits: number;
  doneCommits: number;
  failure?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
};

export type Author = {
  id: number;
  repositoryId: number;
  canonicalName: string;
  canonicalEmail: string;
  source: string;
  aliases?: AuthorAlias[];
};

export type AuthorAlias = {
  id: number;
  authorId: number;
  rawName: string;
  rawEmail: string;
  resolvedName: string;
  resolvedEmail: string;
  source: string;
};

export type Commit = {
  id: number;
  hash: string;
  parentHash: string;
  authorId: number;
  committerTimestamp: number;
  subject: string;
  authorName?: string;
  authorEmail?: string;
};

export type ObjectInfo = {
  id: number;
  path: string;
  type: 'file' | 'directory';
};

export type ObjectMetric = {
  path: string;
  type: string;
  added: number;
  removed: number;
  growth: number;
  churn: number;
  modifications: number;
  modificationFrequency: number;
  churnRate: number;
};

export type AuthorMetric = {
  authorId: number;
  name: string;
  email: string;
  modifications: number;
  churn: number;
  ownership: number;
};

export type TimePoint = {
  date: string;
  totalAdded: number;
  totalRemoved: number;
  totalChurn: number;
};

export type SummaryMetric = {
  added: number;
  removed: number;
  growth: number;
  churn: number;
  modifications: number;
  modificationFrequency: number;
  churnRate: number;
  commitCount: number;
};

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(url, options);
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `Request failed: ${res.status}`);
  }
  return res.json();
}

export async function listRepositories() {
  return request<{ repositories: Repository[] }>('/api/repositories');
}

export async function getRepository(id: number) {
  return request<Repository>(`/api/repositories/${id}`);
}

export async function cloneRepository(url: string, name: string) {
  return request<{ repository: Repository; jobID: number }>('/api/repositories/clone', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, name }),
  });
}

export async function uploadRepository(file: File, name: string) {
  const form = new FormData();
  form.append('file', file);
  form.append('name', name);
  return request<{ repository: Repository; jobID: number }>('/api/repositories/upload', {
    method: 'POST',
    body: form,
  });
}

export async function deleteRepository(id: number) {
  return request<{ status: string }>(`/api/repositories/${id}`, { method: 'DELETE' });
}

export async function reanalyzeRepository(id: number) {
  return request<{ jobID: number }>(`/api/repositories/${id}/reanalyze`, { method: 'POST' });
}

export async function listJobs(repoId: number) {
  return request<{ jobs: AnalysisJob[] }>(`/api/repositories/${repoId}/jobs`);
}

export async function getJob(repoId: number, jobId: number) {
  return request<AnalysisJob>(`/api/repositories/${repoId}/jobs/${jobId}`);
}

export async function listAuthors(repoId: number) {
  return request<{ authors: Author[] }>(`/api/repositories/${repoId}/authors`);
}

export async function mergeAuthors(repoId: number, targetID: number, sourceIDs: number[]) {
  return request<{ status: string }>(`/api/repositories/${repoId}/authors/merge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ targetID, sourceIDs }),
  });
}

export async function unmergeAuthors(repoId: number, authorID: number, aliasIDs: number[]) {
  return request<{ status: string }>(`/api/repositories/${repoId}/authors/unmerge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ authorID, aliasIDs }),
  });
}

export async function listObjects(repoId: number, path?: string) {
  const params = path ? `?path=${encodeURIComponent(path)}` : '';
  return request<{ objects: ObjectInfo[] }>(`/api/repositories/${repoId}/objects${params}`);
}

export async function listCommits(repoId: number, page = 1, limit = 50, from?: number, to?: number) {
  const params = new URLSearchParams({ page: String(page), limit: String(limit) });
  if (from) params.set('from', String(from));
  if (to) params.set('to', String(to));
  return request<{ commits: Commit[]; total: number; page: number; limit: number }>(
    `/api/repositories/${repoId}/commits?${params}`
  );
}

type MetricFilterParams = {
  authorIDs?: number[];
  path?: string;
  type?: string;
  from?: number;
  to?: number;
  commits?: string[];
};

function buildMetricParams(fp: MetricFilterParams): URLSearchParams {
  const params = new URLSearchParams();
  if (fp.authorIDs?.length) params.set('authorIDs', fp.authorIDs.join(','));
  if (fp.path) params.set('path', fp.path);
  if (fp.type) params.set('type', fp.type);
  if (fp.from) params.set('from', String(fp.from));
  if (fp.to) params.set('to', String(fp.to));
  if (fp.commits?.length) params.set('commits', fp.commits.join(','));
  return params;
}

export async function getMetricsSummary(repoId: number, fp: MetricFilterParams = {}) {
  const params = buildMetricParams(fp);
  return request<SummaryMetric>(`/api/repositories/${repoId}/metrics/summary?${params}`);
}

export async function getMetricsObjects(repoId: number, fp: MetricFilterParams = {}, sort = 'churn', dir = 'desc', page = 1, limit = 20) {
  const params = buildMetricParams(fp);
  params.set('sort', sort);
  params.set('dir', dir);
  params.set('page', String(page));
  params.set('limit', String(limit));
  return request<{ metrics: ObjectMetric[]; total: number; page: number; limit: number }>(
    `/api/repositories/${repoId}/metrics/objects?${params}`
  );
}

export async function getMetricsAuthors(repoId: number, fp: MetricFilterParams = {}) {
  const params = buildMetricParams(fp);
  return request<{ authors: AuthorMetric[] }>(`/api/repositories/${repoId}/metrics/authors?${params}`);
}

export async function getMetricsTimeSeries(repoId: number, fp: MetricFilterParams = {}) {
  const params = buildMetricParams(fp);
  return request<{ series: TimePoint[] }>(`/api/repositories/${repoId}/metrics/timeseries?${params}`);
}
