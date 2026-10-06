import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useLocation } from 'wouter';
import * as api from '../api/client';
import type { Author, AuthorAlias } from '../api/client';
import { LoadingSkeleton, ErrorMessage, StatusBadge } from '../components/Shared';

export default function AuthorMergePage({ repoId }: { repoId: number }) {
  const [, navigate] = useLocation();
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [mergeTargetId, setMergeTargetId] = useState<number | null>(null);
  const [showMergeModal, setShowMergeModal] = useState(false);
  const [error, setError] = useState('');

  const authors = useQuery({ queryKey: ['authors', repoId], queryFn: () => api.listAuthors(repoId) });

  const mergeMut = useMutation({
    mutationFn: () => api.mergeAuthors(repoId, mergeTargetId!, Array.from(selected).filter(id => id !== mergeTargetId)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['authors', repoId] });
      setSelected(new Set());
      setShowMergeModal(false);
      setError('');
    },
    onError: (e: Error) => setError(e.message),
  });

  const unmergeMut = useMutation({
    mutationFn: ({ aliasIds }: { aliasIds: number[] }) => api.unmergeAuthors(repoId, 0, aliasIds),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['authors', repoId] }),
    onError: (e: Error) => setError(e.message),
  });

  function toggleSelect(id: number) {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function openMergeModal() {
    if (selected.size < 2) {
      setError('Select at least two authors to merge.');
      return;
    }
    setMergeTargetId(Array.from(selected)[0]);
    setShowMergeModal(true);
  }

  if (authors.isLoading) return <div className="app-shell"><LoadingSkeleton rows={5} /></div>;
  if (authors.isError) return <div className="app-shell"><ErrorMessage message="Failed to load authors." /></div>;

  const authorList = authors.data?.authors || [];

  return (
    <main className="app-shell">
      <div className="dash-header">
        <button className="btn-link" onClick={() => navigate(`/repos/${repoId}`)}>← Dashboard</button>
        <h1 className="dash-title">Author Management</h1>
      </div>

      {error && <ErrorMessage message={error} />}

      <section className="metrics-table-section">
        <div className="section-heading">
          <h2>Canonical Authors ({authorList.length})</h2>
          <button disabled={selected.size < 2} onClick={openMergeModal}>Merge Selected ({selected.size})</button>
        </div>

        <div className="table-wrap">
          <table className="metrics-table">
            <thead>
              <tr>
                <th style={{ width: 40 }}></th>
                <th>Name</th>
                <th>Email</th>
                <th>Source</th>
                <th>Aliases</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {authorList.map((author: Author, i: number) => (
                <AuthorRow
                  key={author.id}
                  author={author}
                  index={i}
                  isSelected={selected.has(author.id)}
                  onToggle={() => toggleSelect(author.id)}
                  onUnmerge={(aliasIds: number[]) => unmergeMut.mutate({ aliasIds })}
                />
              ))}
              {authorList.length === 0 && (
                <tr><td colSpan={6} className="empty-state">No authors found.</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* Merge Modal */}
      {showMergeModal && (
        <div className="modal-overlay" onClick={() => setShowMergeModal(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h2>Merge Authors</h2>
            <p>Select the target canonical identity:</p>
            <div className="merge-options">
              {authorList.filter((a: Author) => selected.has(a.id)).map((a: Author) => (
                <label key={a.id} className="merge-option">
                  <input type="radio" name="mergeTarget" checked={mergeTargetId === a.id} onChange={() => setMergeTargetId(a.id)} />
                  {a.canonicalName} &lt;{a.canonicalEmail}&gt;
                </label>
              ))}
            </div>
            <div className="modal-actions">
              <button onClick={() => setShowMergeModal(false)}>Cancel</button>
              <button disabled={mergeMut.isPending || !mergeTargetId} onClick={() => mergeMut.mutate()}>
                {mergeMut.isPending ? 'Merging…' : 'Confirm Merge'}
              </button>
            </div>
          </div>
        </div>
      )}
    </main>
  );
}

function AuthorRow({ author, index, isSelected, onToggle, onUnmerge }: {
  author: Author;
  index: number;
  isSelected: boolean;
  onToggle: () => void;
  onUnmerge: (aliasIds: number[]) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const aliases = author.aliases || [];
  const manualAliases = aliases.filter((a: AuthorAlias) => a.source === 'manual');

  return (
    <>
      <tr className={index % 2 ? 'zebra' : ''}>
        <td><input type="checkbox" checked={isSelected} onChange={onToggle} /></td>
        <td>{author.canonicalName}</td>
        <td>{author.canonicalEmail}</td>
        <td><StatusBadge status={author.source} /></td>
        <td>
          {aliases.length > 0 && (
            <button className="btn-link" onClick={() => setExpanded(!expanded)}>
              {expanded ? 'Hide' : 'Show'} {aliases.length} alias{aliases.length !== 1 ? 'es' : ''}
            </button>
          )}
        </td>
        <td>
          {manualAliases.length > 0 && (
            <button className="btn-danger btn-sm" onClick={() => onUnmerge(manualAliases.map((a: AuthorAlias) => a.id))}>
              Unmerge
            </button>
          )}
        </td>
      </tr>
      {expanded && aliases.map((alias: AuthorAlias) => (
        <tr key={alias.id} className="alias-row">
          <td></td>
          <td colSpan={2}>{alias.rawName} &lt;{alias.rawEmail}&gt;</td>
          <td><StatusBadge status={alias.source} /></td>
          <td colSpan={2}>→ {alias.resolvedName} &lt;{alias.resolvedEmail}&gt;</td>
        </tr>
      ))}
    </>
  );
}
