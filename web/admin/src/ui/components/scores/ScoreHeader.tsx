interface ScoreHeaderProps {
  /** Name of the current section being displayed */
  sectionName: string;
  /** Whether the app is currently online (navigator.onLine) */
  isOnline: boolean;
  /** Whether there are score updates queued for offline sync */
  hasPendingSync: boolean;
  /** Timestamp when scores were last fetched from the server */
  fetchedAt: Date | null;
}

/**
 * Header component for the score entry view.
 *
 * Displays:
 * - Section name as the main heading
 * - Status badges (offline, pending sync)
 * - Last updated timestamp
 *
 * Used at the top of the scores card to provide context and status information.
 */
export function ScoreHeader({ sectionName, isOnline, hasPendingSync, fetchedAt }: ScoreHeaderProps) {
  return (
    <div className="scores-header">
      <h2>{sectionName}</h2>
      <div className="scores-meta">
        {!isOnline && <span className="offline-badge">Offline</span>}
        {hasPendingSync && <span className="pending-badge">Pending sync</span>}
        {fetchedAt && <span>Updated {fetchedAt.toLocaleTimeString()}</span>}
      </div>
    </div>
  );
}
