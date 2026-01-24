import { useState, useEffect } from 'react';
import { getPendingSyncMessage } from './SyncAuthHandler.tsx';

export function LoginPage() {
  const [pendingMessage, setPendingMessage] = useState<string | null>(null);

  useEffect(() => {
    // Check for pending sync message (e.g., from expired session during background sync)
    const message = getPendingSyncMessage();
    if (message) {
      setPendingMessage(message);
    }
  }, []);

  const handleLogin = () => {
    window.location.href = '/admin/login';
  };

  return (
    <div className="app">
      <main className="main">
        <div className="login-card">
          <h1 className="login-title">Patrol Scores Admin</h1>

          {pendingMessage && (
            <div className="login-warning">
              <p>{pendingMessage}</p>
              <p className="login-warning-detail">
                Your pending changes are saved locally and will sync after you log in.
              </p>
            </div>
          )}

          <p className="login-description">
            Sign in with your Online Scout Manager account to manage patrol scores for your section.
          </p>

          <div className="login-notice">
            <h2 className="login-notice-title">Permissions Required</h2>
            <p>
              This application requires access to member data in Online Scout Manager to retrieve patrol information and scores. You will be asked to grant this permission when you sign in.
            </p>
          </div>

          <button className="btn btn-primary login-button" onClick={handleLogin}>
            Login via OSM
          </button>
        </div>
      </main>
    </div>
  );
}
