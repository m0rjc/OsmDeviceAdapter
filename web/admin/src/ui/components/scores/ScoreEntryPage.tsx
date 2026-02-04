import {useEffect, useState} from 'react';
import {
    clearUserEntriesForSection,
    refreshCurrentSection,
    selectChangesForCurrentSection,
    selectSelectedPatrolKeys,
    selectSelectedSection,
    submitScoreChanges,
    syncNow,
    forceSync,
    useAppDispatch,
    useAppSelector
} from '../../state';
import {ConfirmDialog, Loading} from '../../../legacy/components';
import {useToast} from '../../hooks';
import {MessageCard} from '../MessageCard';
import {ScoreHeader} from './ScoreHeader';
import {PatrolList} from './PatrolList';
import {ScoreActions} from './ScoreActions';
import {SyncStatusBadge} from './SyncStatusBadge';

interface ScoreEntryPageProps {
    /** CSRF token for authenticated API requests (deprecated - worker handles auth) */
    csrfToken: string;
}

/**
 * Main score entry page component.
 *
 * Provides a complete score entry interface with:
 * - Automatic loading of patrol scores for the selected section
 * - Real-time user input tracking in Redux state
 * - Worker-based offline support with IndexedDB queue
 * - Service worker sync integration
 * - Confirmation dialog before submitting
 *
 * State Management:
 * - Section selection and auto-fetch handled by setSelectedSection thunk
 * - Patrol data and pending scores managed by worker (PatrolsChangeMessage broadcasts)
 * - User score entries tracked in UI slice (cleared after submit)
 * - Worker handles all API calls, offline queueing, and optimistic updates
 *
 * @example
 * ```tsx
 * <Provider store={store}>
 *   <ScoreEntryPage csrfToken={csrfToken} />
 * </Provider>
 * ```
 */
export function ScoreEntryPage({csrfToken: _csrfToken}: ScoreEntryPageProps) {
    const dispatch = useAppDispatch();
    const {showToast} = useToast();

    // Redux state
    const selectedSection = useAppSelector(selectSelectedSection);
    const patrolKeys = useAppSelector(selectSelectedPatrolKeys);
    const changes = useAppSelector(selectChangesForCurrentSection);

    // Local UI state
    const [showConfirm, setShowConfirm] = useState(false);

    // Auto-retry timer based on nextRetryTime
    useEffect(() => {
        if (!selectedSection?.nextRetryTime) {
            return;
        }

        const timeUntilRetry = selectedSection.nextRetryTime - Date.now();
        if (timeUntilRetry <= 0) {
            // Time has already passed, trigger sync immediately
            dispatch(syncNow());
            return;
        }

        // Set timer to trigger sync at nextRetryTime
        const timer = setTimeout(() => {
            dispatch(syncNow());
        }, timeUntilRetry);

        return () => clearTimeout(timer);
    }, [selectedSection?.nextRetryTime, dispatch]);

    // Event handlers
    const handleRefresh = () => {
        dispatch(refreshCurrentSection());
    };

    const handleClear = () => {
        if (!selectedSection) return;
        dispatch(clearUserEntriesForSection({sectionId: selectedSection.id}));
    };

    const handleAddScores = () => {
        if (changes.length === 0) {
            showToast('warning', 'No changes to submit');
            return;
        }
        setShowConfirm(true);
    };

    const handleConfirmSubmit = async () => {
        if (!selectedSection) return;
        if (changes.length === 0) return;

        try {
            // Submit to worker - it handles offline queueing and optimistic updates
            await dispatch(submitScoreChanges()).unwrap();

            // Clear user entries and close dialog
            dispatch(clearUserEntriesForSection({sectionId: selectedSection.id}));
            setShowConfirm(false);

            showToast('success', `Submitted ${changes.length} change(s)`);
        } catch (err) {
            // Worker handles retries, this is only for critical failures
            const message = err instanceof Error ? err.message : 'Failed to submit scores';
            showToast('error', message);
        }
    };

    const handleSyncNow = () => {
        dispatch(syncNow());
    };

    const handleForceSync = async () => {
        if (!selectedSection) return;

        // Browser confirmation dialog
        const confirmed = window.confirm(
            'Force Sync - Use with Caution\n\n' +
            'This will clear permanent errors and retry all pending scores. ' +
            'Be careful not to trigger rate limits.\n\n' +
            'Continue?'
        );

        if (!confirmed) {
            return;
        }

        try {
            await dispatch(forceSync()).unwrap();
            showToast('success', 'Force sync initiated');
        } catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to force sync';
            showToast('error', message);
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

    // Check loading state
    const isLoading = selectedSection.state === 'loading';
    const hasError = selectedSection.state === 'error';

    if (isLoading) {
        return <Loading message="Loading scores..."/>;
    }

    if (hasError && patrolKeys.length === 0) {
        return (
            <MessageCard
                title="Error Loading Scores"
                message={selectedSection.error || 'Failed to load patrol scores'}
                action={{
                    label: 'Retry',
                    onClick: handleRefresh,
                }}
            />
        );
    }

    const hasChanges = changes.length > 0;
    const isRefreshing = isLoading; // Could track separately if needed
    const hasPendingSync = (selectedSection.pendingCount ?? 0) > 0;

    return (
        <>
            <div className="scores-card">
                <ScoreHeader
                    sectionName={selectedSection.name}
                    isOnline={true} // Worker handles offline - always show as ready
                    hasPendingSync={hasPendingSync}
                    fetchedAt={null} // TODO: Track timestamp if needed
                />

                {hasPendingSync && (
                    <SyncStatusBadge
                        nextRetryTime={selectedSection.nextRetryTime}
                        pendingCount={selectedSection.pendingCount ?? 0}
                        readyCount={selectedSection.readyCount ?? 0}
                        syncInProgress={selectedSection.syncInProgress ?? false}
                    />
                )}

                <PatrolList
                    patrolKeys={patrolKeys}
                    sectionError={selectedSection.error}
                />

                <ScoreActions
                    hasChanges={hasChanges}
                    isRefreshing={isRefreshing}
                    isOnline={true} // Worker handles offline
                    readyCount={selectedSection.readyCount ?? 0}
                    pendingCount={selectedSection.pendingCount ?? 0}
                    onRefresh={handleRefresh}
                    onClear={handleClear}
                    onSubmit={handleAddScores}
                    onSyncNow={handleSyncNow}
                    onForceSync={handleForceSync}
                />
            </div>

            {showConfirm && (
                <ConfirmDialog
                    changes={changes.map(c => ({
                        patrolId: c.patrolId,
                        patrolName: c.name,
                        points: c.score,
                    }))}
                    isSubmitting={false}
                    onConfirm={handleConfirmSubmit}
                    onCancel={() => setShowConfirm(false)}
                />
            )}
        </>
    );
}
