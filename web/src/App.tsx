import { useQuery } from '@tanstack/react-query';

type Repository = {
  id: number;
  name: string;
  sourceType: string;
  status: string;
  defaultRef: string;
  headCommit: string;
  createdAt: string;
  updatedAt: string;
};

type RepositoryResponse = {
  repositories: Repository[];
};

async function fetchRepositories(): Promise<RepositoryResponse> {
  const response = await fetch('/api/repositories');
  if (!response.ok) {
    throw new Error('Could not load repositories.');
  }
  return response.json();
}

export default function App() {
  const repositories = useQuery({ queryKey: ['repositories'], queryFn: fetchRepositories });

  return (
    <main className="app-shell">
      <section className="hero-card">
        <p className="eyebrow">Repo Analysis Tool</p>
        <h1>Analyze Git history across repositories.</h1>
        <p className="lede">
          Import a repository, resolve authors, and explore exact added, removed, growth, churn, and ownership metrics.
        </p>
        <div className="action-grid" aria-label="Repository import actions">
          <form className="panel">
            <h2>Clone remote repository</h2>
            <label htmlFor="clone-url">Repository URL</label>
            <input id="clone-url" name="url" type="url" placeholder="https://github.com/example/project.git" disabled />
            <button type="button" disabled>Clone support coming next</button>
          </form>
          <form className="panel">
            <h2>Upload ZIP archive</h2>
            <label htmlFor="repo-zip">Repository ZIP</label>
            <input id="repo-zip" name="zip" type="file" accept=".zip,application/zip" disabled />
            <button type="button" disabled>Upload support coming next</button>
          </form>
        </div>
      </section>

      <section className="repository-list" aria-labelledby="repositories-heading">
        <div className="section-heading">
          <h2 id="repositories-heading">Repositories</h2>
          {repositories.isFetching && <span>Refreshing…</span>}
        </div>
        {repositories.isError && <p className="error">{(repositories.error as Error).message}</p>}
        {repositories.isLoading && <p>Loading repositories…</p>}
        {repositories.data?.repositories.length === 0 && (
          <p className="empty-state">No repositories have been imported yet.</p>
        )}
        <div className="cards">
          {repositories.data?.repositories.map((repo) => (
            <article className="repo-card" key={repo.id}>
              <h3>{repo.name}</h3>
              <p>{repo.sourceType} · {repo.status}</p>
              <small>Default ref: {repo.defaultRef}</small>
            </article>
          ))}
        </div>
      </section>
    </main>
  );
}
