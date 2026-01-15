import { useState, useEffect, useCallback } from 'react';
import { useAuth, useToast } from '../hooks';
import { fetchScores, updateScores, ApiError } from '../api';
import type { Patrol } from '../api';
import { Loading } from './Loading';
import { ConfirmDialog } from './ConfirmDialog';

interface PatrolWithInput extends Patrol {
  pointsToAdd: number;
}

export function ScoreEntry() {
  const { selectedSectionId, csrfToken, sections } = useAuth();
  const { showToast } = useToast();

  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sectionName, setSectionName] = useState<string>('');
  const [patrols, setPatrols] = useState<PatrolWithInput[]>([]);
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null);
  const [showConfirm, setShowConfirm] = useState(false);

  const loadScores = useCallback(async (sectionId: number, isRefresh = false) => {
    if (isRefresh) {
      setIsRefreshing(true);
    } else {
      setIsLoading(true);
    }
    setError(null);

    try {
      const data = await fetchScores(sectionId);
      setSectionName(data.section.name);
      setPatrols(
        data.patrols.map(p => ({
          ...p,
          pointsToAdd: 0,
        }))
      );
      setFetchedAt(new Date(data.fetchedAt));
      if (isRefresh) {
        showToast('success', 'Scores refreshed');
      }
    } catch (err) {
      const message = err instanceof ApiError ? err.message : 'Failed to load scores';
      setError(message);
      showToast('error', message);
    } finally {
      setIsLoading(false);
      setIsRefreshing(false);
    }
  }, [showToast]);

  useEffect(() => {
    if (selectedSectionId) {
      loadScores(selectedSectionId);
    }
  }, [selectedSectionId, loadScores]);

  const handlePointsChange = (patrolId: string, value: string) => {
    const points = value === '' || value === '-' ? 0 : parseInt(value, 10);
    if (isNaN(points)) return;

    // Clamp to valid range
    const clampedPoints = Math.max(-1000, Math.min(1000, points));

    setPatrols(prev =>
      prev.map(p => (p.id === patrolId ? { ...p, pointsToAdd: clampedPoints } : p))
    );
  };

  const handleClear = () => {
    setPatrols(prev => prev.map(p => ({ ...p, pointsToAdd: 0 })));
  };

  const handleRefresh = () => {
    if (selectedSectionId) {
      loadScores(selectedSectionId, true);
    }
  };

  const getChanges = () => {
    return patrols
      .filter(p => p.pointsToAdd !== 0)
      .map(p => ({
        patrolId: p.id,
        patrolName: p.name,
        points: p.pointsToAdd,
      }));
  };

  const handleAddScores = () => {
    const changes = getChanges();
    if (changes.length === 0) {
      showToast('warning', 'No changes to submit');
      return;
    }
    setShowConfirm(true);
  };

  const handleConfirmSubmit = async () => {
    if (!selectedSectionId || !csrfToken) return;

    const changes = getChanges();
    if (changes.length === 0) return;

    setIsSubmitting(true);

    try {
      const result = await updateScores(
        selectedSectionId,
        {
          updates: changes.map(c => ({
            patrolId: c.patrolId,
            points: c.points,
          })),
        },
        csrfToken
      );

      if (result.success) {
        // Update local state with new scores
        setPatrols(prev =>
          prev.map(p => {
            const updated = result.patrols.find(r => r.id === p.id);
            if (updated) {
              return { ...p, score: updated.newScore, pointsToAdd: 0 };
            }
            return p;
          })
        );
        showToast('success', `Updated ${result.patrols.length} patrol(s)`);
      }

      setShowConfirm(false);
    } catch (err) {
      const message = err instanceof ApiError ? err.message : 'Failed to update scores';
      showToast('error', message);
    } finally {
      setIsSubmitting(false);
    }
  };

  const hasChanges = patrols.some(p => p.pointsToAdd !== 0);

  if (!selectedSectionId) {
    return (
      <div className="empty-state">
        <h3>No Section Selected</h3>
        <p>Please select a section to view and edit patrol scores.</p>
      </div>
    );
  }

  if (sections.length === 0) {
    return (
      <div className="empty-state">
        <h3>No Sections Available</h3>
        <p>You don't have access to any sections.</p>
      </div>
    );
  }

  if (isLoading) {
    return <Loading message="Loading scores..." />;
  }

  if (error && patrols.length === 0) {
    return (
      <div className="error-message">
        <p>{error}</p>
        <button className="btn btn-primary" onClick={handleRefresh} style={{ marginTop: '1rem' }}>
          Retry
        </button>
      </div>
    );
  }

  return (
    <>
      <div className="scores-card">
        <div className="scores-header">
          <h2>{sectionName}</h2>
          <span className="scores-meta">
            {fetchedAt && `Updated ${fetchedAt.toLocaleTimeString()}`}
          </span>
        </div>

        {patrols.length === 0 ? (
          <div className="empty-state">
            <h3>No Patrols Found</h3>
            <p>This section doesn't have any patrols configured.</p>
          </div>
        ) : (
          <ul className="patrol-list">
            {patrols.map(patrol => (
              <li key={patrol.id} className="patrol-item">
                <div className="patrol-info">
                  <span className="patrol-name">{patrol.name}</span>
                  <span className="patrol-score">
                    Current: {patrol.score} points
                    {patrol.pointsToAdd !== 0 && (
                      <span>
                        {' '}
                        → {patrol.score + patrol.pointsToAdd} points
                      </span>
                    )}
                  </span>
                </div>
                <div className="patrol-input">
                  <span className="patrol-input-label">Add:</span>
                  <input
                    type="number"
                    min={-1000}
                    max={1000}
                    value={patrol.pointsToAdd === 0 ? '' : patrol.pointsToAdd}
                    onChange={e => handlePointsChange(patrol.id, e.target.value)}
                    placeholder="0"
                    className={
                      patrol.pointsToAdd > 0
                        ? 'positive'
                        : patrol.pointsToAdd < 0
                        ? 'negative'
                        : ''
                    }
                  />
                </div>
              </li>
            ))}
          </ul>
        )}

        <div className="action-bar">
          <button
            className="btn btn-secondary"
            onClick={handleRefresh}
            disabled={isRefreshing}
          >
            {isRefreshing ? 'Refreshing...' : 'Refresh'}
          </button>
          <button
            className="btn btn-secondary"
            onClick={handleClear}
            disabled={!hasChanges}
          >
            Clear
          </button>
          <button
            className="btn btn-success"
            onClick={handleAddScores}
            disabled={!hasChanges}
          >
            Add Scores
          </button>
        </div>
      </div>

      {showConfirm && (
        <ConfirmDialog
          changes={getChanges()}
          isSubmitting={isSubmitting}
          onConfirm={handleConfirmSubmit}
          onCancel={() => setShowConfirm(false)}
        />
      )}
    </>
  );
}
