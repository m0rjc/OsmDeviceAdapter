import { useState, useEffect, useCallback, useRef } from 'react';
import { useAuth, useToast } from '../hooks';
import {
  fetchScores,
  updateScores,
  ApiError,
} from '../api';
import type { Patrol } from '../api';
import { OpenOfflineStore, OutboxEntry } from '../offlineStore';
import { Loading } from './Loading';
import { ConfirmDialog } from './ConfirmDialog';

// Helper to check if online
const isOnline = () => typeof navigator !== 'undefined' && navigator.onLine;

// Helper to listen for connectivity changes
const onConnectivityChange = (callback: (online: boolean) => void) => {
  const handleOnline = () => callback(true);
  const handleOffline = () => callback(false);
  window.addEventListener('online', handleOnline);
  window.addEventListener('offline', handleOffline);
  return () => {
    window.removeEventListener('online', handleOnline);
    window.removeEventListener('offline', handleOffline);
  };
};

// Helper to trigger manual sync
const manualSyncPendingScores = async () => {
  if (navigator.serviceWorker?.controller) {
    navigator.serviceWorker.controller.postMessage({ type: 'MANUAL_SYNC' });
  }
};

interface PatrolWithInput extends Patrol {
  pointsToAdd: number;
}

export function ScoreEntry() {
  const { selectedSectionId, csrfToken, sections } = useAuth();
  const { showToast } = useToast();

  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isSyncing, setIsSyncing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sectionName, setSectionName] = useState<string>('');
  const [patrols, setPatrols] = useState<PatrolWithInput[]>([]);
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null);
  const [showConfirm, setShowConfirm] = useState(false);
  const [pendingPoints, setPendingPoints] = useState<Map<string, number>>(new Map());
  const [failedEntries, setFailedEntries] = useState<OutboxEntry[]>([]);
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
      const store = await OpenOfflineStore();
      const allPending = await store.getAllPending();

      // Filter to current section and build map
      const pending = new Map<string, number>();
      for (const entry of allPending) {
        if (entry.sectionId === sectionId.toString() && entry.retryAfter !== -1) {
          // Only include entries that aren't permanently failed
          pending.set(entry.patrolId, entry.scoreDelta);
        }
      }

      setPendingPoints(pending);
    } catch {
      // Ignore errors loading pending points
    }
  }, []);

  // Load failed entries from IndexedDB
  const loadFailedEntries = useCallback(async (sectionId: number) => {
    try {
      const store = await OpenOfflineStore();
      const failed = await store.getFailedEntries();
      // Filter to current section
      const failedForSection = failed.filter(e => e.sectionId === sectionId.toString());
      setFailedEntries(failedForSection);
    } catch {
      // Ignore errors loading failed entries
    }
  }, []);

  useEffect(() => {
    if (selectedSectionId) {
      loadScores(selectedSectionId);
      loadPendingPoints(selectedSectionId);
      loadFailedEntries(selectedSectionId);
    }
  }, [selectedSectionId, loadScores, loadPendingPoints, loadFailedEntries]);

  // Listen for online/offline status changes
  useEffect(() => {
    return onConnectivityChange(async (isNowOnline) => {
      setOnline(isNowOnline);
      if (isNowOnline && selectedSectionId) {
        // Check if there are pending updates to sync
        const store = await OpenOfflineStore();
        const pending = await store.getPendingForSyncNow();
        const pendingForSection = pending.filter(e => e.sectionId === selectedSectionId.toString());

        if (pendingForSection.length === 0) {
          // No pending sync - refresh immediately
          loadScores(selectedSectionId, true);
        } else {
          // Trigger sync for pending updates
          await manualSyncPendingScores();
          // SYNC_SUCCESS handler will refresh pending points and failed entries
        }
        loadPendingPoints(selectedSectionId);
        loadFailedEntries(selectedSectionId);
      }
    });
  }, [selectedSectionId, loadScores, loadPendingPoints, loadFailedEntries]);

  // Periodic fallback sync for mobile browsers (where background sync may not work)
  useEffect(() => {
    if (!selectedSectionId) return;

    const SYNC_INTERVAL = 30000; // 30 seconds
    const intervalId = setInterval(async () => {
      // Only sync if online and not currently syncing
      if (online && !isSyncing) {
        const store = await OpenOfflineStore();
        const pending = await store.getPendingForSyncNow();
        const pendingForSection = pending.filter(e => e.sectionId === selectedSectionId.toString());

        if (pendingForSection.length > 0) {
          console.log(`[Periodic Sync] Found ${pendingForSection.length} pending patrol(s), triggering sync`);
          handleManualSync();
        }
      }
    }, SYNC_INTERVAL);

    return () => clearInterval(intervalId);
  }, [selectedSectionId, online, isSyncing]);

  // Detect removed patrols with pending updates
  useEffect(() => {
    if (patrols.length === 0 || pendingPoints.size === 0) return;

    const currentPatrolIds = new Set(patrols.map(p => p.id));
    const orphanedPending = Array.from(pendingPoints.entries())
      .filter(([id]) => !currentPatrolIds.has(id));

    if (orphanedPending.length > 0) {
      const patrolList = orphanedPending.map(([id, points]) => `  • Patrol ${id}: ${points} points`).join('\n');

      const shouldClear = confirm(
        `You have pending changes for ${orphanedPending.length} patrol(s) that no longer exist:\n\n` +
        `${patrolList}\n\n` +
        `These patrols may have been removed or merged. Clear these pending updates?`
      );

      if (shouldClear && selectedSectionId) {
        (async () => {
          const store = await OpenOfflineStore();
          for (const [id] of orphanedPending) {
            await store.clear(selectedSectionId.toString(), id);
          }
          await loadPendingPoints(selectedSectionId);
          showToast('info', `Cleared ${orphanedPending.length} orphaned update(s)`);
        })();
      }
    }
  }, [patrols, pendingPoints, selectedSectionId, loadPendingPoints, showToast]);

  // Listen for pendingCleared event from Header
  useEffect(() => {
    const handlePendingCleared = () => {
      if (selectedSectionId) {
        loadPendingPoints(selectedSectionId);
        loadFailedEntries(selectedSectionId);
      }
    };

    window.addEventListener('pendingCleared', handlePendingCleared);
    return () => window.removeEventListener('pendingCleared', handlePendingCleared);
  }, [selectedSectionId, loadPendingPoints, loadFailedEntries]);

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

          // Refresh pending points and failed entries
          if (selectedSectionId) {
            loadPendingPoints(selectedSectionId);
            loadFailedEntries(selectedSectionId);
          }
          syncRefreshTimeoutRef.current = null;
        }, 100);

        // Check if there are permanent errors
        if (event.data.hasPermanentErrors) {
          showToast('error', 'Some updates failed. See details above.');
        }
      } else if (event.data?.type === 'SYNC_ERROR') {
        showToast('error', event.data.error || 'Failed to sync pending scores');
        if (selectedSectionId) {
          loadPendingPoints(selectedSectionId);
          loadFailedEntries(selectedSectionId);
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
  }, [selectedSectionId, loadScores, loadPendingPoints, loadFailedEntries, showToast]);

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

  const handleManualSync = async () => {
    if (!online || hasPendingSync === false) return;

    setIsSyncing(true);
    try {
      await manualSyncPendingScores();
      // The sync result will be handled by the service worker message listener
      // which will show a toast and update the pending points
    } catch (err) {
      showToast('error', 'Failed to trigger sync');
    } finally {
      // Reset syncing state after a short delay to allow the service worker to process
      setTimeout(() => setIsSyncing(false), 2000);
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
      // Add entries to offline store first (for offline sync)
      const store = await OpenOfflineStore();
      for (const change of changes) {
        await store.addPoints(
          selectedSectionId.toString(),
          change.patrolId,
          change.points
        );
      }

      // Strip patrolName before sending to server (only used for confirmation dialog)
      const updatesForServer = changes.map(c => ({
        patrolId: c.patrolId,
        points: c.points,
      }));

      // Try to submit immediately
      const response = await updateScores(
        selectedSectionId,
        updatesForServer,
        csrfToken
      );

      if (response.ok) {
        // Parse response to get patrol results
        const result = await response.json();

        // Clear successful entries from store
        if (result.patrols && result.patrols.length > 0) {
          for (const patrol of result.patrols) {
            if (patrol.success) {
              await store.clear(selectedSectionId.toString(), patrol.id);
            }
          }

          // Update local state with scores from response
          setPatrols(prev =>
            prev.map(p => {
              const updated = result.patrols.find((r: any) => r.id === p.id);
              if (updated && updated.newScore !== undefined) {
                return { ...p, score: updated.newScore, pointsToAdd: 0 };
              }
              return p;
            })
          );
        }

        showToast('success', `Scores updated`);
      } else if (response.status === 202) {
        // Accepted - service worker will sync in background
        showToast('success', 'Scores queued for sync');
      } else {
        // Error - leave in store for service worker to retry
        showToast('error', 'Failed to update scores - will retry automatically');
      }

      // Clear input fields
      setPatrols(prev => prev.map(p => ({ ...p, pointsToAdd: 0 })));

      // Reload pending display
      await loadPendingPoints(selectedSectionId);
      await loadFailedEntries(selectedSectionId);

      setShowConfirm(false);
    } catch (err) {
      // Check if this is a network error (offline) or a different error
      const isNetworkError = err instanceof TypeError && err.message.includes('fetch');

      if (isNetworkError || !navigator.onLine) {
        // Network error - entries already in store, service worker will retry
        showToast('warning', 'Offline - scores will sync when online');
      } else {
        // Other error - show specific message if available
        const message = err instanceof ApiError ? err.message : 'Failed to update scores';
        showToast('error', message);
      }

      // Clear input fields
      setPatrols(prev => prev.map(p => ({ ...p, pointsToAdd: 0 })));

      // Reload pending display
      await loadPendingPoints(selectedSectionId);

      setShowConfirm(false);
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleRetryFailed = async () => {
    if (!selectedSectionId) return;

    const store = await OpenOfflineStore();
    for (const entry of failedEntries) {
      // Reset to retryAfter=0 for immediate retry
      await store.setRetryAfter(entry.sectionId, entry.patrolId, new Date(0));
    }
    await manualSyncPendingScores();
    showToast('info', 'Retrying failed updates...');
  };

  const handleClearFailed = async () => {
    if (!selectedSectionId) return;

    if (!confirm(`Clear ${failedEntries.length} failed update(s)? This cannot be undone.`)) {
      return;
    }

    const store = await OpenOfflineStore();
    for (const entry of failedEntries) {
      await store.clear(entry.sectionId, entry.patrolId);
    }

    setFailedEntries([]);
    await loadPendingPoints(selectedSectionId);
    showToast('success', 'Failed updates cleared');
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

        {failedEntries.length > 0 && (
          <div className="failed-updates-banner">
            <div className="banner-icon">⚠️</div>
            <div className="banner-content">
              <strong>Some updates failed to sync</strong>
              <p>{failedEntries.length} update(s) could not be saved. Details:</p>
              <ul>
                {failedEntries.map(entry => (
                  <li key={entry.key}>
                    Patrol {entry.patrolId}: {entry.scoreDelta > 0 ? '+' : ''}{entry.scoreDelta} points
                    <span className="error-detail"> - {entry.errorMessage}</span>
                  </li>
                ))}
              </ul>
            </div>
            <div className="banner-actions">
              <button className="btn btn-secondary" onClick={handleRetryFailed}>
                Retry All
              </button>
              <button className="btn btn-secondary" onClick={handleClearFailed}>
                Clear All
              </button>
            </div>
          </div>
        )}

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
