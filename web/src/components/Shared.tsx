export function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    ready: '#0b8a3e',
    analyzing: '#e67700',
    pending: '#5c7cfa',
    failed: '#c92a2a',
    queued: '#5c7cfa',
    running: '#e67700',
    succeeded: '#0b8a3e',
  };
  return (
    <span className="status-badge" style={{ background: colors[status] || '#868e96' }}>
      {status}
    </span>
  );
}

export function ProgressBar({ value, max }: { value: number; max: number }) {
  const pct = max > 0 ? Math.round((value / max) * 100) : 0;
  return (
    <div className="progress-bar-track">
      <div className="progress-bar-fill" style={{ width: `${pct}%` }} />
      <span className="progress-bar-label">{value}/{max} ({pct}%)</span>
    </div>
  );
}

export function LoadingSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="skeleton-container">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="skeleton-row" />
      ))}
    </div>
  );
}

export function ErrorMessage({ message }: { message: string }) {
  return <p className="error">{message}</p>;
}

export function Pagination({
  page,
  total,
  limit,
  onChange,
}: {
  page: number;
  total: number;
  limit: number;
  onChange: (p: number) => void;
}) {
  const totalPages = Math.max(1, Math.ceil(total / limit));
  return (
    <div className="pagination">
      <button disabled={page <= 1} onClick={() => onChange(page - 1)}>Previous</button>
      <span>Page {page} of {totalPages}</span>
      <button disabled={page >= totalPages} onClick={() => onChange(page + 1)}>Next</button>
    </div>
  );
}

export function Tooltip({ content, children }: { content: string; children: React.ReactNode }) {
  return (
    <span className="tooltip-wrapper" title={content}>
      {children}
      <span className="tooltip-icon">?</span>
    </span>
  );
}
