import {useEffect, useState} from 'react';

interface SyncStatusBadgeProps {
    /** Next retry time (milliseconds), undefined if no pending entries */
    nextRetryTime?: number;
    /** Total number of pending entries */
    pendingCount: number;
    /** Number of entries ready to sync now */
    readyCount: number;
    /** True if section sync lock is currently held */
    syncInProgress: boolean;
}

/**
 * Shows sync status and countdown for automatic retry.
 *
 * States:
 * - "Syncing..." - sync in progress
 * - "Retry in 2m 15s (3 pending)" - future retry scheduled
 * - "Ready to sync (3 pending)" - entries ready now
 * - null (hidden) - no pending entries
 *
 * Updates every second when future retry is scheduled.
 */
export function SyncStatusBadge({nextRetryTime, pendingCount, readyCount, syncInProgress}: SyncStatusBadgeProps) {
    const [now, setNow] = useState(Date.now());

    // Update clock every second when there's a future retry
    useEffect(() => {
        if (!nextRetryTime || nextRetryTime <= now) {
            return;
        }

        const timer = setInterval(() => {
            setNow(Date.now());
        }, 1000);

        return () => clearInterval(timer);
    }, [nextRetryTime, now]);

    // Don't show badge if no pending entries
    if (pendingCount === 0) {
        return null;
    }

    // Syncing state
    if (syncInProgress) {
        return (
            <div className="sync-status-badge syncing">
                <span className="spinner" aria-label="Syncing"></span>
                Syncing...
            </div>
        );
    }

    // Ready to sync now
    if (readyCount > 0) {
        return (
            <div className="sync-status-badge ready">
                Ready to sync ({pendingCount} pending)
            </div>
        );
    }

    // Future retry scheduled
    if (nextRetryTime && nextRetryTime > now) {
        const secondsUntilRetry = Math.ceil((nextRetryTime - now) / 1000);
        const timeText = formatDuration(secondsUntilRetry);

        return (
            <div className="sync-status-badge scheduled">
                Retry in {timeText} ({pendingCount} pending)
            </div>
        );
    }

    // Permanent errors or other blocked state
    return (
        <div className="sync-status-badge blocked">
            {pendingCount} pending (needs attention)
        </div>
    );
}

/**
 * Format seconds into human-readable duration.
 * Examples:
 * - 45 → "45s"
 * - 90 → "1m 30s"
 * - 3665 → "1h 1m"
 */
function formatDuration(seconds: number): string {
    if (seconds < 60) {
        return `${seconds}s`;
    }

    const minutes = Math.floor(seconds / 60);
    const remainingSeconds = seconds % 60;

    if (minutes < 60) {
        return remainingSeconds > 0 ? `${minutes}m ${remainingSeconds}s` : `${minutes}m`;
    }

    const hours = Math.floor(minutes / 60);
    const remainingMinutes = minutes % 60;

    return remainingMinutes > 0 ? `${hours}h ${remainingMinutes}m` : `${hours}h`;
}
