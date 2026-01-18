import { useServiceWorker } from '../hooks';

export function UpdatePrompt() {
  const { needRefresh, updateServiceWorker, dismissUpdate } = useServiceWorker();

  if (!needRefresh) {
    return null;
  }

  return (
    <div className="update-banner">
      <span className="update-message">A new version is available</span>
      <div className="update-actions">
        <button className="btn btn-primary btn-sm" onClick={updateServiceWorker}>
          Update now
        </button>
        <button className="btn btn-text btn-sm" onClick={dismissUpdate}>
          Later
        </button>
      </div>
    </div>
  );
}
