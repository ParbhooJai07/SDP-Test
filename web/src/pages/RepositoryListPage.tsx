import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocation } from 'wouter';
import * as api from '../api/client';
import { StatusBadge, ProgressBar, ErrorMessage, LoadingSkeleton } from '../components/Shared';

export default function RepositoryListPage() {
  const queryClient = useQueryClient();
  const [, navigate] = useLocation();

  const [cloneUrl, setCloneUrl] = useState('');
  const [cloneName, setCloneName] = useState('');
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploadName, setUploadName] = useState('');
  const [formError, setFormError] = useState('');

  const repos = useQuery({ queryKey: ['repositories'], queryFn: api.listRepositories, refetchInterval: 3000 });

  const cloneMut = useMutation({
    mutationFn: () => api.cloneRepository(cloneUrl, cloneName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['repositories'] });
      setCloneUrl('');
      setCloneName('');
      setFormError('');
    },
    onError: (e: Error) => setFormError(e.message),
  });

  const uploadMut = useMutation({
    mutationFn: () => api.uploadRepository(uploadFile!, uploadName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['repositories'] });
      setUploadFile(null);
      setUploadName('');
      setFormError('');
    },
    onError: (e: Error) => setFormError(e.message),
  });

  const deleteMut = useMutation({
    mutationFn: (id: number) => api.deleteRepository(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['repositories'] }),
  });

  const retryMut = useMutation({
    mutationFn: (id: number) => api.reanalyzeRepository(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['repositories'] }),
  });

  function handleClone(e: React.FormEvent) {
    e.preventDefault();
    setFormError('');
    try {
      new URL(cloneUrl);
    } catch {
      setFormError('Please enter a valid URL.');
      return;
    }
    cloneMut.mutate();
  }

  function handleUpload(e: React.FormEvent) {
    e.preventDefault();
    setFormError('');
    if (!uploadFile || !uploadFile.name.toLowerCase().endsWith('.zip')) {
      setFormError('Please select a .zip file.');
      return;
    }
    uploadMut.mutate();
  }

  return (
    <main className="app-shell">
      <section className="hero-card">
        <p className="eyebrow">Repo Analysis Tool</p>
        <h1>Analyze Git history across repositories.</h1>
        <p className="lede">
          Import a repository, resolve authors, and explore exact added, removed, growth, churn, and ownership metrics.
        </p>
        {formError && <ErrorMessage message={formError} />}
        <div className="action-grid">
          <form className="panel" onSubmit={handleClone}>
            <h2>Clone remote repository</h2>
            <label htmlFor="clone-url">Repository URL</label>
            <input id="clone-url" type="url" placeholder="https://github.com/example/project.git" value={cloneUrl} onChange={e => setCloneUrl(e.target.value)} required />
            <label htmlFor="clone-name">Name (optional)</label>
            <input id="clone-name" type="text" placeholder="Auto-detected from URL" value={cloneName} onChange={e => setCloneName(e.target.value)} />
            <button type="submit" disabled={cloneMut.isPending || !cloneUrl}>
              {cloneMut.isPending ? 'Cloning…' : 'Clone'}
            </button>
          </form>
          <form className="panel" onSubmit={handleUpload}>
            <h2>Upload ZIP archive</h2>
            <label htmlFor="repo-zip">Repository ZIP</label>
            <input id="repo-zip" type="file" accept=".zip" onChange={e => setUploadFile(e.target.files?.[0] ?? null)} />
            <label htmlFor="upload-name">Name (optional)</label>
            <input id="upload-name" type="text" placeholder="Auto-detected from filename" value={uploadName} onChange={e => setUploadName(e.target.value)} />
            <button type="submit" disabled={uploadMut.isPending || !uploadFile}>
              {uploadMut.isPending ? 'Uploading…' : 'Upload'}
            </button>
          </form>
        </div>
      </section>

      <section className="repository-list" aria-labelledby="repositories-heading">
        <div className="section-heading">
          <h2 id="repositories-heading">Repositories</h2>
          {repos.isFetching && <span className="subtle">Refreshing…</span>}
        </div>

        {repos.isLoading && <LoadingSkeleton rows={3} />}
        {repos.isError && <ErrorMessage message={(repos.error as Error).message} />}
        {repos.data?.repositories.length === 0 && (
          <p className="empty-state">No repositories have been imported yet. Use the forms above to add one.</p>
        )}

        <div className="cards">
          {repos.data?.repositories.map((repo: api.Repository) => (
            <RepoCard
              key={repo.id}
              repo={repo}
              onOpen={() => navigate(`/repos/${repo.id}`)}
              onDelete={() => { if (confirm(`Delete "${repo.name}"?`)) deleteMut.mutate(repo.id); }}
              onRetry={() => retryMut.mutate(repo.id)}
            />
          ))}
        </div>
      </section>
    </main>
  );
}

function RepoCard({ repo, onOpen, onDelete, onRetry }: {
  repo: api.Repository;
  onOpen: () => void;
  onDelete: () => void;
  onRetry: () => void;
}) {
  const jobs = useQuery({
    queryKey: ['jobs', repo.id],
    queryFn: () => api.listJobs(repo.id),
    enabled: repo.status === 'analyzing',
    refetchInterval: repo.status === 'analyzing' ? 2000 : false,
  });

  const latestJob = jobs.data?.jobs?.[0];

  return (
    <article className="repo-card">
      <div className="repo-card-header">
        <h3 className="repo-card-name" onClick={onOpen} style={{ cursor: repo.status === 'ready' ? 'pointer' : 'default' }}>
          {repo.name}
        </h3>
        <StatusBadge status={repo.status} />
      </div>
      <p className="repo-card-meta">{repo.sourceType} · {repo.headCommit ? repo.headCommit.slice(0, 8) : '—'}</p>
      {repo.status === 'analyzing' && latestJob && (
        <ProgressBar value={latestJob.doneCommits} max={latestJob.totalCommits} />
      )}
      {repo.status === 'failed' && repo.failure && (
        <p className="error" style={{ fontSize: '0.85rem' }}>{repo.failure}</p>
      )}
      <div className="repo-card-actions">
        {repo.status === 'ready' && <button onClick={onOpen}>Open Dashboard</button>}
        {repo.status === 'failed' && <button onClick={onRetry}>Retry</button>}
        <button className="btn-danger" onClick={onDelete}>Delete</button>
      </div>
    </article>
  );
}
