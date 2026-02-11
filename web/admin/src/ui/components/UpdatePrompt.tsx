import { useAppDispatch, useAppSelector } from '../state';
import { selectShouldShowUpdatePrompt, dismissUpdate } from '../state';
import { applyServiceWorkerUpdate } from '../hooks';

/**
 * UpdatePrompt - Banner notification for available service worker updates.
 *
 * Displays a non-intrusive banner when a new version of the app is available.
 * Users can choose to update immediately or dismiss the prompt and continue working.
 *
 * This component follows the separation of concerns principle:
 * - PWA lifecycle is managed separately from business logic
 * - Update state is stored in Redux (app slice)
 * - vite-plugin-pwa handles the underlying service worker update mechanism
 *
 * Usage:
 * Render once at the app root level (in App.tsx) outside main content.
 */
export function UpdatePrompt() {
  const dispatch = useAppDispatch();
  const shouldShow = useAppSelector(selectShouldShowUpdatePrompt);

  if (!shouldShow) {
    return null;
  }

  const handleUpdate = async () => {
    await applyServiceWorkerUpdate();
  };

  const handleDismiss = () => {
    dispatch(dismissUpdate());
  };

  return (
    <div className="update-banner">
      <span className="update-message">A new version is available</span>
      <div className="update-actions">
        <button className="btn btn-primary btn-sm" onClick={handleUpdate}>
          Update now
        </button>
        <button className="btn btn-text btn-sm" onClick={handleDismiss}>
          Later
        </button>
      </div>
    </div>
  );
}
