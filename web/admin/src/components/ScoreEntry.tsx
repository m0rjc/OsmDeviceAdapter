import { useState, useEffect, useCallback, useRef } from 'react';
import { useAuth, useToast } from '../hooks';
import {
  fetchScores,
  updateScores,
  ApiError,
  OfflineQueuedError,
  getPendingPointsByPatrol,
  onConnectivityChange,
  isOnline,
} from '../api';
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
  const [pendingPoints, setPendingPoints] = useState<Map<string, number>>(new Map());
  const [online, setOnline] = useState(isOnline());

  // Refs for debouncing sync-triggered refreshes and consolidating toasts
  const syncRefreshTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const syncedSectionsRef = useRef<Set<number>>(new Set());

  const loadScores = useCallback(async (sectionId: number, isRefresh = false, silent = false) => {
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
      if (isRefresh && !silent) {
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

  // Load pending points from IndexedDB
  const loadPendingPoints = useCallback(async (sectionId: number) => {
    try {
      const pending = await getPendingPointsByPatrol(sectionId);
      setPendingPoints(pending);
    } catch {
      // Ignore errors loading pending points
    }
  }, []);

  useEffect(() => {
    if (selectedSectionId) {
      loadScores(selectedSectionId);
      loadPendingPoints(selectedSectionId);
    }
  }, [selectedSectionId, loadScores, loadPendingPoints]);

  // Listen for online/offline status changes
  useEffect(() => {
    return onConnectivityChange(async (isNowOnline) => {
      setOnline(isNowOnline);
      if (isNowOnline && selectedSectionId) {
        // Check if there are pending updates to sync
        const pending = await getPendingPointsByPatrol(selectedSectionId);
        if (pending.size === 0) {
          // No pending sync - refresh immediately
          loadScores(selectedSectionId, true);
        }
        // If there are pending updates, wait for SYNC_SUCCESS to refresh
        // This ensures we get the correct data after sync completes
        loadPendingPoints(selectedSectionId);
      }
    });
  }, [selectedSectionId, loadScores, loadPendingPoints]);

  // Listen for service worker sync messages
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      if (event.data?.type === 'SYNC_SUCCESS') {
        // Track which sections have been synced
        if (event.data.sectionId) {
          syncedSectionsRef.current.add(event.data.sectionId);
        }

        // If patrol data is provided, apply it immediately (optimistic update from server)
        // This avoids a race condition where we refresh from OSM before the background
        // worker has processed the outbox entries (which happens every 30 seconds)
        if (event.data.patrols && Array.isArray(event.data.patrols) && event.data.patrols.length > 0) {
          setPatrols(prev =>
            prev.map(p => {
              const updated = event.data.patrols.find((r: any) => r.id === p.id);
              if (updated) {
                return { ...p, score: updated.newScore, pointsToAdd: 0 };
              }
              return p;
            })
          );
        }

        // Debounce - multiple SYNC_SUCCESS messages may arrive in quick succession
        // (one per section). Wait for all to arrive before showing toast and refreshing.
        if (syncRefreshTimeoutRef.current) {
          clearTimeout(syncRefreshTimeoutRef.current);
        }
        syncRefreshTimeoutRef.current = setTimeout(() => {
          const count = syncedSectionsRef.current.size;
          if (count > 0) {
            const message = count === 1
              ? 'Pending scores synced'
              : `Pending scores synced (${count} sections)`;
            showToast('success', message);
          }
          syncedSectionsRef.current.clear();

          // Only refresh pending points (not scores) - scores were already updated above
          if (selectedSectionId) {
            loadPendingPoints(selectedSectionId);
          }
          syncRefreshTimeoutRef.current = null;
        }, 100);
      } else if (event.data?.type === 'SYNC_ERROR') {
        showToast('error', event.data.error || 'Failed to sync pending scores');
        if (selectedSectionId) {
          loadPendingPoints(selectedSectionId);
        }
      }
      // Note: SYNC_AUTH_REQUIRED is handled globally by SyncAuthHandler in App.tsx
    };

    navigator.serviceWorker?.addEventListener('message', handleMessage);
    return () => {
      navigator.serviceWorker?.removeEventListener('message', handleMessage);
      if (syncRefreshTimeoutRef.current) {
        clearTimeout(syncRefreshTimeoutRef.current);
      }
    };
  }, [selectedSectionId, loadScores, loadPendingPoints, showToast]);

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
        patrolName: p.name, // Used for confirmation dialog only, not sent to server
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
      // Strip patrolName before sending to server (only used for confirmation dialog)
      const updatesForServer = changes.map(c => ({
        patrolId: c.patrolId,
        points: c.points,
      }));

      const result = await updateScores(
        selectedSectionId,
        updatesForServer,
        csrfToken
      );

      if (result.success && result.patrols && result.patrols.length > 0) {
        // Update local state with scores from response
        // Both 200 OK and 202 Accepted return patrol data
        setPatrols(prev =>
          prev.map(p => {
            const updated = result.patrols!.find(r => r.id === p.id);
            if (updated) {
              return { ...p, score: updated.newScore, pointsToAdd: 0 };
            }
            return p;
          })
        );

        // Show appropriate toast based on response status
        if (result.status === 'accepted') {
          showToast('success', `Scores updated (syncing to OSM in background)`);
        } else {
          showToast('success', `Updated ${result.patrols.length} patrol(s)`);
        }

        // Reload pending points to reflect any queued changes
        loadPendingPoints(selectedSectionId);
      }

      setShowConfirm(false);
    } catch (err) {
      if (err instanceof OfflineQueuedError) {
        // Changes were queued for later sync
        showToast('warning', 'Offline - changes queued for sync');
        // Clear the input fields and update pending points display
        setPatrols(prev => prev.map(p => ({ ...p, pointsToAdd: 0 })));
        loadPendingPoints(selectedSectionId);
        setShowConfirm(false);
      } else {
        const message = err instanceof ApiError ? err.message : 'Failed to update scores';
        showToast('error', message);
      }
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

  const hasPendingSync = pendingPoints.size > 0;

  return (
    <>
      <div className="scores-card">
        <div className="scores-header">
          <h2>{sectionName}</h2>
          <div className="scores-meta">
            {!online && <span className="offline-badge">Offline</span>}
            {hasPendingSync && <span className="pending-badge">Pending sync</span>}
            {fetchedAt && <span>Updated {fetchedAt.toLocaleTimeString()}</span>}
          </div>
        </div>

        {patrols.length === 0 ? (
          <div className="empty-state">
            <h3>No Patrols Found</h3>
            <p>This section doesn't have any patrols configured.</p>
          </div>
        ) : (
          <div className="patrol-cards">
            {patrols.map(patrol => {
              const pendingForPatrol = pendingPoints.get(patrol.id) || 0;
              return (
                <div key={patrol.id} className={`patrol-card${pendingForPatrol ? ' has-pending' : ''}`}>
                  <div className="patrol-card-header">
                    <span className="patrol-name">
                      {patrol.name}
                      {pendingForPatrol !== 0 && (
                        <span className="patrol-pending-badge" title="Pending sync">
                          {pendingForPatrol > 0 ? '+' : ''}{pendingForPatrol}
                        </span>
                      )}
                    </span>
                    <span className="patrol-current-score">{patrol.score}</span>
                    {(patrol.pointsToAdd !== 0 || pendingForPatrol !== 0) && (
                      <span className="patrol-new-score">
                        {patrol.score + patrol.pointsToAdd + pendingForPatrol}
                      </span>
                    )}
                  </div>
                  <div className="patrol-card-body">
                    <div className="patrol-input">
                      <span className="patrol-input-label">Add points:</span>
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
                  </div>
                </div>
              );
            })}
          </div>
        )}

        <div className="action-bar">
          <button
            className="btn btn-secondary"
            onClick={handleRefresh}
            disabled={isRefreshing || !online}
            title={!online ? 'Refresh disabled while offline' : undefined}
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
