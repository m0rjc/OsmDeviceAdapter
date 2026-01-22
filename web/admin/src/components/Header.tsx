import { useState, useEffect } from 'react';
import { useAuth } from '../hooks';
import { Menu } from './Menu';
import { OpenOfflineStore } from '../offlineStore';

// Helper to trigger manual sync
const manualSyncPendingScores = async () => {
  if (navigator.serviceWorker?.controller) {
    navigator.serviceWorker.controller.postMessage({ type: 'MANUAL_SYNC' });
  }
};

export function Header() {
  const { user, logout, pendingWrites } = useAuth();
  const [hasPendingEntries, setHasPendingEntries] = useState(false);
  const [hasFailedEntries, setHasFailedEntries] = useState(false);
  const [isSyncing, setIsSyncing] = useState(false);
  const [online, setOnline] = useState(() => typeof navigator !== 'undefined' && navigator.onLine);

  useEffect(() => {
    const checkPendingStatus = async () => {
      try {
        const store = await OpenOfflineStore();
        const pending = await store.getPendingForSyncNow();
        const failed = await store.getFailedEntries();

        setHasPendingEntries(pending.length > 0);
        setHasFailedEntries(failed.length > 0);
      } catch {
        // Ignore errors
      }
    };

    checkPendingStatus();

    // Re-check every 5 seconds
    const interval = setInterval(checkPendingStatus, 5000);
    return () => clearInterval(interval);
  }, []);

  // Listen for online/offline status changes
  useEffect(() => {
    const handleOnline = () => setOnline(true);
    const handleOffline = () => setOnline(false);
    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  const handleSyncNow = async () => {
    if (!online || !hasPendingEntries) return;

    setIsSyncing(true);
    try {
      await manualSyncPendingScores();
      // The sync result will be handled by service worker message listeners
    } finally {
      // Reset syncing state after a short delay to allow the service worker to process
      setTimeout(() => setIsSyncing(false), 2000);
    }
  };

  const handleForceRetryFailed = async () => {
    if (!confirm('Retry all failed updates? This will attempt to sync them again.')) {
      return;
    }

    const store = await OpenOfflineStore();
    const failed = await store.getFailedEntries();

    // Reset retryAfter to 0 for immediate retry
    for (const entry of failed) {
      await store.setRetryAfter(entry.sectionId, entry.patrolId, new Date(0));
    }

    // Trigger manual sync
    await manualSyncPendingScores();
    alert(`Retrying ${failed.length} failed update(s)...`);
  };

  const handleClearAllPending = async () => {
    const store = await OpenOfflineStore();
    const allPending = await store.getAllPending();

    if (allPending.length === 0) {
      alert('No pending updates to clear');
      return;
    }

    if (!confirm(
      `Clear all ${allPending.length} pending update(s)? This cannot be undone. ` +
      `Any score changes that haven't synced will be lost.`
    )) {
      return;
    }

    for (const entry of allPending) {
      await store.clear(entry.sectionId, entry.patrolId);
    }

    alert('All pending updates cleared');

    // Notify ScoreEntry to refresh
    window.dispatchEvent(new CustomEvent('pendingCleared'));
  };

  // Format build time for display (e.g., "Jan 16 14:32")
  const buildTime = new Date(__BUILD_TIME__);
  const buildLabel = buildTime.toLocaleDateString('en-GB', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });

  return (
    <header className="header">
      <div className="header-content">
        <h1 className="header-title">Score Entry</h1>
        <div className="header-user">
          {pendingWrites > 0 && (
            <span className="header-pending-badge" title={`${pendingWrites} pending write(s)`}>
              {pendingWrites} pending
            </span>
          )}
          {user && <span className="header-user-name">{user.name}</span>}
          <Menu
            options={[
              {
                label: 'Register Device',
                href: '/device',
              },
              ...(hasPendingEntries ? [{
                label: isSyncing ? 'Syncing...' : 'Sync Now',
                onClick: handleSyncNow,
                disabled: isSyncing || !online,
              }] : []),
              ...(hasFailedEntries ? [{
                label: 'Force Retry Failed Updates',
                onClick: handleForceRetryFailed,
              }] : []),
              ...(hasPendingEntries ? [{
                label: 'Clear All Pending',
                onClick: handleClearAllPending,
              }] : []),
              {
                label: 'Logout',
                onClick: logout,
              },
            ]}
          />
        </div>
      </div>
      <div className="header-build">Build: {buildLabel}</div>
    </header>
  );
}
