import { useState, useEffect, useCallback, useRef } from 'react';
import { useAppDispatch, useAppSelector } from '../../state/hooks';
import {
  selectSelectedSection,
  selectSelectedPatrolKeys,
  selectChangesForCurrentSection,
} from '../../state';
import { setCanonicalPatrols } from '../../state/patrolsSlice';
import { setPatrolScore } from '../../state/uiSlice';
import {
  fetchScores,
  updateScores,
  ApiError,
  OfflineQueuedError,
  getPendingPointsByPatrol,
  onConnectivityChange,
  isOnline,
} from '../../../legacy/api';
import { Loading } from '../../../legacy/components/Loading';
import { ConfirmDialog } from '../../../legacy/components/ConfirmDialog';
import { useToast } from '../../../legacy/hooks';
import { MessageCard } from '../MessageCard';
import { ScoreHeader } from './ScoreHeader';
import { PatrolList } from './PatrolList';
import { ScoreActions } from './ScoreActions';

interface ScoreEntryPageProps {
  /** CSRF token for authenticated API requests */
  csrfToken: string;
}

/**
 * Main score entry page component.
 *
 * Provides a complete score entry interface with:
 * - Automatic loading of patrol scores for the selected section
 * - Real-time user input tracking in Redux state
 * - Offline support with IndexedDB queue
 * - Service worker sync integration
 * - Online/offline status tracking
 * - Confirmation dialog before submitting
 *
 * State Management:
 * - Uses Redux for section/patrol data and user entries
 * - Preserves user edits during server updates via setCanonicalPatrols
 * - Tracks pending sync points from IndexedDB separately
 *
 * Lifecycle:
 * 1. Loads scores when section is selected (if not already loaded)
 * 2. User enters points (stored in Redux via setUserEntry)
 * 3. On submit, sends to API or queues for offline sync
 * 4. Listens for service worker sync messages to refresh after background sync
 *
 * @example
 * ```tsx
 * <Provider store={store}>
 *   <ScoreEntryPage csrfToken={csrfToken} />
 * </Provider>
 * ```
 */
export function ScoreEntryPage({ csrfToken }: ScoreEntryPageProps) {
  const dispatch = useAppDispatch();
  const { showToast } = useToast();

  const selectedSection = useAppSelector(selectSelectedSection);
  const patrols = useAppSelector(selectPatrolsForSelectedSection);
  const arePatrolsLoaded = useAppSelector(selectArePatrolsLoadedForSelectedSection);
  const hasUnsavedEdits = useAppSelector(selectHasUnsavedEdits);

  const [isLoading, setIsLoading] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
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

      // Update Redux state with canonical patrol list
      dispatch(setCanonicalPatrols({
        sectionId,
        patrols: data.patrols.map(p => ({
          id: p.id,
          name: p.name,
          committedScore: p.score,
          pendingScore: 0, // Server doesn't track pending scores separately
        })),
      }));

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
  }, [dispatch, showToast]);

  // Load pending points from IndexedDB
  const loadPendingPoints = useCallback(async (sectionId: number) => {
    try {
      const pending = await getPendingPointsByPatrol(sectionId);
      setPendingPoints(pending);
    } catch {
      // Ignore errors loading pending points
    }
  }, []);

  // Load scores when section changes
  useEffect(() => {
    if (selectedSection && !arePatrolsLoaded) {
      loadScores(selectedSection.id);
      loadPendingPoints(selectedSection.id);
    }
  }, [selectedSection, arePatrolsLoaded, loadScores, loadPendingPoints]);

  // Listen for online/offline status changes
  useEffect(() => {
    return onConnectivityChange(async (isNowOnline) => {
      setOnline(isNowOnline);
      if (isNowOnline && selectedSection) {
        // Check if there are pending updates to sync
        const pending = await getPendingPointsByPatrol(selectedSection.id);
        if (pending.size === 0) {
          // No pending sync - refresh immediately
          loadScores(selectedSection.id, true);
        }
        // If there are pending updates, wait for SYNC_SUCCESS to refresh
        loadPendingPoints(selectedSection.id);
      }
    });
  }, [selectedSection, loadScores, loadPendingPoints]);

  // Listen for service worker sync messages
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      if (event.data?.type === 'SYNC_SUCCESS') {
        // Track which sections have been synced
        if (event.data.sectionId) {
          syncedSectionsRef.current.add(event.data.sectionId);
        }

        // Debounce - multiple SYNC_SUCCESS messages may arrive in quick succession
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

          if (selectedSection) {
            loadScores(selectedSection.id, true, true); // silent=true to skip "refreshed" toast
            loadPendingPoints(selectedSection.id);
          }
          syncRefreshTimeoutRef.current = null;
        }, 100);
      } else if (event.data?.type === 'SYNC_ERROR') {
        showToast('error', event.data.error || 'Failed to sync pending scores');
        if (selectedSection) {
          loadPendingPoints(selectedSection.id);
        }
      }
    };

    navigator.serviceWorker?.addEventListener('message', handleMessage);
    return () => {
      navigator.serviceWorker?.removeEventListener('message', handleMessage);
      if (syncRefreshTimeoutRef.current) {
        clearTimeout(syncRefreshTimeoutRef.current);
      }
    };
  }, [selectedSection, loadScores, loadPendingPoints, showToast]);

  const handlePointsChange = (patrolId: string, value: string) => {
    if (!selectedSection) return;

    const points = value === '' || value === '-' ? 0 : parseInt(value, 10);
    if (isNaN(points)) return;

    // Clamp to valid range
    const clampedPoints = Math.max(-1000, Math.min(1000, points));

    dispatch(setUserEntry({
      sectionId: selectedSection.id,
      patrolId,
      points: clampedPoints,
    }));
  };

  const handleClear = () => {
    if (!selectedSection) return;
    dispatch(clearAllUserEntries({ sectionId: selectedSection.id }));
  };

  const handleRefresh = () => {
    if (selectedSection) {
      loadScores(selectedSection.id, true);
    }
  };

  const getChanges = () => {
    if (!patrols) return [];

    return patrols
      .filter(p => p.userEntry !== 0)
      .map(p => ({
        patrolId: p.id,
        patrolName: p.name,
        points: p.userEntry,
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
    if (!selectedSection) return;

    const changes = getChanges();
    if (changes.length === 0) return;

    setIsSubmitting(true);

    try {
      const result = await updateScores(
        selectedSection.id,
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
        dispatch(setCanonicalPatrols({
          sectionId: selectedSection.id,
          patrols: result.patrols.map(r => ({
            id: r.id,
            name: r.name,
            committedScore: r.newScore,
            pendingScore: 0,
          })),
        }));

        // Clear user entries
        dispatch(clearAllUserEntries({ sectionId: selectedSection.id }));

        showToast('success', `Updated ${result.patrols.length} patrol(s)`);
      }

      setShowConfirm(false);
    } catch (err) {
      if (err instanceof OfflineQueuedError) {
        // Changes were queued for later sync
        showToast('warning', 'Offline - changes queued for sync');
        // Clear the input fields and update pending points display
        dispatch(clearAllUserEntries({ sectionId: selectedSection.id }));
        loadPendingPoints(selectedSection.id);
        setShowConfirm(false);
      } else {
        const message = err instanceof ApiError ? err.message : 'Failed to update scores';
        showToast('error', message);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  // Render empty states
  if (!selectedSection) {
    return (
      <MessageCard
        title="No Section Selected"
        message="Please select a section to view and edit patrol scores."
      />
    );
  }

  if (isLoading) {
    return <Loading message="Loading scores..." />;
  }

  if (error && !patrols) {
    return (
      <MessageCard
        title="Error Loading Scores"
        message={error}
        action={{
          label: 'Retry',
          onClick: handleRefresh,
        }}
      />
    );
  }

  if (!patrols) {
    return <Loading message="Loading scores..." />;
  }

  const hasPendingSync = pendingPoints.size > 0;

  return (
    <>
      <div className="scores-card">
        <ScoreHeader
          sectionName={selectedSection.name}
          isOnline={online}
          hasPendingSync={hasPendingSync}
          fetchedAt={fetchedAt}
        />

        <PatrolList
          patrols={patrols}
          pendingPointsMap={pendingPoints}
          onPointsChange={handlePointsChange}
        />

        <ScoreActions
          hasChanges={hasUnsavedEdits}
          isRefreshing={isRefreshing}
          isOnline={online}
          onRefresh={handleRefresh}
          onClear={handleClear}
          onSubmit={handleAddScores}
        />
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
