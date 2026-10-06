import { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useLocation } from 'wouter';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, ResponsiveContainer, Legend as RLegend, BarChart, Bar } from 'recharts';
import * as api from '../api/client';
import { LoadingSkeleton, ErrorMessage, Pagination, Tooltip } from '../components/Shared';

export default function RepoDashboardPage({ repoId }: { repoId: number }) {
  const [, navigate] = useLocation();

  // Filter state from URL params
  const params = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '');
  const [authorIDs, setAuthorIDs] = useState<number[]>(
    params.get('authorIDs')?.split(',').map(Number).filter(Boolean) || []
  );
  const [pathFilter, setPathFilter] = useState(params.get('path') || '');
  const [typeFilter, setTypeFilter] = useState(params.get('type') || '');
  const [fromDate, setFromDate] = useState(params.get('fromDate') || '');
  const [toDate, setToDate] = useState(params.get('toDate') || '');
  const [objPage, setObjPage] = useState(1);
  const [sortCol, setSortCol] = useState('churn');
  const [sortDir, setSortDir] = useState('desc');

  // Sync filters to URL
  useEffect(() => {
    const p = new URLSearchParams();
    if (authorIDs.length) p.set('authorIDs', authorIDs.join(','));
    if (pathFilter) p.set('path', pathFilter);
    if (typeFilter) p.set('type', typeFilter);
    if (fromDate) p.set('fromDate', fromDate);
    if (toDate) p.set('toDate', toDate);
    const qs = p.toString();
    const newUrl = `/repos/${repoId}${qs ? '?' + qs : ''}`;
    window.history.replaceState(null, '', newUrl);
  }, [authorIDs, pathFilter, typeFilter, fromDate, toDate, repoId]);

  const fp: Parameters<typeof api.getMetricsSummary>[1] = {
    authorIDs: authorIDs.length ? authorIDs : undefined,
    path: pathFilter || undefined,
    type: typeFilter || undefined,
    from: fromDate ? Math.floor(new Date(fromDate).getTime() / 1000) : undefined,
    to: toDate ? Math.floor(new Date(toDate).getTime() / 1000) : undefined,
  };

  const repo = useQuery({ queryKey: ['repo', repoId], queryFn: () => api.getRepository(repoId) });
  const summary = useQuery({ queryKey: ['summary', repoId, fp], queryFn: () => api.getMetricsSummary(repoId, fp) });
  const timeseries = useQuery({ queryKey: ['timeseries', repoId, fp], queryFn: () => api.getMetricsTimeSeries(repoId, fp) });
  const objects = useQuery({ queryKey: ['objects-metrics', repoId, fp, sortCol, sortDir, objPage], queryFn: () => api.getMetricsObjects(repoId, fp, sortCol, sortDir, objPage, 20) });
  const authorMetrics = useQuery({ queryKey: ['author-metrics', repoId, fp], queryFn: () => api.getMetricsAuthors(repoId, fp) });
  const authors = useQuery({ queryKey: ['authors', repoId], queryFn: () => api.listAuthors(repoId) });

  function clearFilters() {
    setAuthorIDs([]);
    setPathFilter('');
    setTypeFilter('');
    setFromDate('');
    setToDate('');
    setObjPage(1);
  }

  function handleSort(col: string) {
    if (sortCol === col) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc');
    } else {
      setSortCol(col);
      setSortDir('desc');
    }
    setObjPage(1);
  }

  if (repo.isLoading) return <div className="app-shell"><LoadingSkeleton rows={5} /></div>;
  if (repo.isError) return <div className="app-shell"><ErrorMessage message="Repository not found." /></div>;

  return (
    <main className="app-shell">
      <div className="dash-header">
        <button className="btn-link" onClick={() => navigate('/')}>← All Repositories</button>
        <h1 className="dash-title">{repo.data?.name}</h1>
        <button className="btn-link" onClick={() => navigate(`/repos/${repoId}/authors`)}>Manage Authors</button>
      </div>

      {/* Filter bar */}
      <section className="filter-bar">
        <div className="filter-group">
          <label>Author</label>
          <select multiple value={authorIDs.map(String)} onChange={e => {
            const vals = Array.from(e.target.selectedOptions, o => Number(o.value));
            setAuthorIDs(vals);
            setObjPage(1);
          }}>
            {authors.data?.authors?.map((a: api.Author) => (
              <option key={a.id} value={a.id}>{a.canonicalName}</option>
            ))}
          </select>
        </div>
        <div className="filter-group">
          <label>Path</label>
          <input type="text" placeholder="e.g. src/components" value={pathFilter} onChange={e => { setPathFilter(e.target.value); setObjPage(1); }} />
        </div>
        <div className="filter-group">
          <label>Type</label>
          <select value={typeFilter} onChange={e => { setTypeFilter(e.target.value); setObjPage(1); }}>
            <option value="">All</option>
            <option value="file">Files</option>
            <option value="directory">Directories</option>
          </select>
        </div>
        <div className="filter-group">
          <label>From</label>
          <input type="date" value={fromDate} onChange={e => { setFromDate(e.target.value); setObjPage(1); }} />
        </div>
        <div className="filter-group">
          <label>To</label>
          <input type="date" value={toDate} onChange={e => { setToDate(e.target.value); setObjPage(1); }} />
        </div>
        <button className="btn-clear" onClick={clearFilters}>Clear Filters</button>
      </section>

      {/* KPI Cards */}
      <section className="kpi-grid">
        {summary.isLoading ? <LoadingSkeleton rows={1} /> : summary.data && (
          <>
            <KpiCard label="Added" value={fmt(summary.data.added)} tooltip="Total lines added across all matching commits." />
            <KpiCard label="Removed" value={fmt(summary.data.removed)} tooltip="Total lines removed across all matching commits." />
            <KpiCard label="Growth" value={fmt(summary.data.growth)} tooltip="Net line growth: added − removed." />
            <KpiCard label="Churn" value={fmt(summary.data.churn)} tooltip="Total churn: added + removed." />
            <KpiCard label="Modifications" value={fmt(summary.data.modifications)} tooltip="Count of commits where churn > 0 for the root." />
            <KpiCard label="Mod Frequency" value={summary.data.modificationFrequency.toFixed(2)} tooltip="modifications / |H| (0 if empty)." />
            <KpiCard label="Churn Rate" value={summary.data.churnRate.toFixed(2)} tooltip="churn / |H| (lines per commit, 0 if empty)." />
          </>
        )}
      </section>

      {/* Charts */}
      <section className="chart-grid">
        <div className="chart-panel">
          <h3>Lines Added vs Removed Over Time</h3>
          {timeseries.isLoading ? <LoadingSkeleton rows={4} /> : (
            <ResponsiveContainer width="100%" height={300}>
              <AreaChart data={timeseries.data?.series || []}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="date" fontSize={11} />
                <YAxis fontSize={11} />
                <RLegend />
                <Area type="monotone" dataKey="totalAdded" name="Added" stroke="#2f9e44" fill="#2f9e44" fillOpacity={0.3} />
                <Area type="monotone" dataKey="totalRemoved" name="Removed" stroke="#e03131" fill="#e03131" fillOpacity={0.3} />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
        <div className="chart-panel">
          <h3>Top Objects by Churn</h3>
          {objects.isLoading ? <LoadingSkeleton rows={4} /> : (
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={(objects.data?.metrics || []).slice(0, 10)} layout="vertical">
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis type="number" fontSize={11} />
                <YAxis dataKey="path" type="category" width={150} fontSize={10} />
                <RLegend />
                <Bar dataKey="churn" name="Churn" fill="#5c7cfa" />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>
      </section>

      {/* Object Metrics Table */}
      <section className="metrics-table-section">
        <h3>Object Metrics</h3>
        {objects.isLoading ? <LoadingSkeleton rows={5} /> : objects.isError ? <ErrorMessage message="Failed to load metrics." /> : (
          <>
            <div className="table-wrap">
              <table className="metrics-table">
                <thead>
                  <tr>
                    {[
                      { key: 'path', label: 'Path' }, { key: 'type', label: 'Type' },
                      { key: 'added', label: 'Added' }, { key: 'removed', label: 'Removed' },
                      { key: 'growth', label: 'Growth' }, { key: 'churn', label: 'Churn' },
                      { key: 'modifications', label: 'Mods' }, { key: 'modFrequency', label: 'Mod Freq' },
                      { key: 'churnRate', label: 'Churn Rate' },
                    ].map(col => (
                      <th key={col.key} onClick={() => handleSort(col.key)} className="sortable-th">
                        {col.label} {sortCol === col.key ? (sortDir === 'asc' ? '▲' : '▼') : ''}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {(objects.data?.metrics || []).map((m: api.ObjectMetric, i: number) => (
                    <tr key={m.path + m.type} className={i % 2 ? 'zebra' : ''}>
                      <td className="path-cell" onClick={() => { if (m.type === 'directory') { setPathFilter(m.path); setObjPage(1); } }} style={{ cursor: m.type === 'directory' ? 'pointer' : 'default' }}>
                        {m.type === 'directory' ? '📁 ' : '📄 '}{m.path}
                      </td>
                      <td>{m.type}</td>
                      <td>{fmt(m.added)}</td>
                      <td>{fmt(m.removed)}</td>
                      <td>{fmt(m.growth)}</td>
                      <td>{fmt(m.churn)}</td>
                      <td>{fmt(m.modifications)}</td>
                      <td>{m.modificationFrequency.toFixed(2)}</td>
                      <td>{m.churnRate.toFixed(2)}</td>
                    </tr>
                  ))}
                  {(objects.data?.metrics || []).length === 0 && (
                    <tr><td colSpan={9} className="empty-state">No objects match the current filters.</td></tr>
                  )}
                </tbody>
              </table>
            </div>
            <Pagination page={objPage} total={objects.data?.total || 0} limit={20} onChange={setObjPage} />
          </>
        )}
      </section>

      {/* Author Breakdown */}
      <section className="metrics-table-section">
        <h3>Author Breakdown</h3>
        {authorMetrics.isLoading ? <LoadingSkeleton rows={3} /> : (
          <div className="table-wrap">
            <table className="metrics-table">
              <thead>
                <tr>
                  <th>Author</th><th>Email</th><th>Modifications</th><th>Churn</th><th>Ownership %</th>
                </tr>
              </thead>
              <tbody>
                {(authorMetrics.data?.authors || []).map((a: api.AuthorMetric, i: number) => (
                  <tr key={a.authorId} className={i % 2 ? 'zebra' : ''}>
                    <td>{a.name}</td>
                    <td>{a.email}</td>
                    <td>{fmt(a.modifications)}</td>
                    <td>{fmt(a.churn)}</td>
                    <td>{(a.ownership * 100).toFixed(1)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </main>
  );
}

function KpiCard({ label, value, tooltip }: { label: string; value: string; tooltip: string }) {
  return (
    <div className="kpi-card">
      <Tooltip content={tooltip}><span className="kpi-label">{label}</span></Tooltip>
      <span className="kpi-value">{value}</span>
    </div>
  );
}

function fmt(n: number): string {
  return n.toLocaleString();
}
