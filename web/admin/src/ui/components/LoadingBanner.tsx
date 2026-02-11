import './LoadingBanner.css';

/**
 * Loading banner displayed while the user profile is being fetched.
 *
 * Shows a centered banner with a spinner and loading message.
 */
export function LoadingBanner() {
  return (
    <div className="loading-banner">
      <div className="loading-banner__content">
        <div className="loading-banner__spinner"></div>
        <p className="loading-banner__text">Loading...</p>
      </div>
    </div>
  );
}
