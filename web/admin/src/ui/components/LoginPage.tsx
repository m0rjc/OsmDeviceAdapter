/**
 * LoginPage - Prompts user to authenticate via OSM OAuth.
 *
 * This is a simplified version of the legacy login page without
 * pending sync message handling (that will be added later).
 */
export function LoginPage() {
  const handleLogin = () => {
    // Redirect to the OAuth login endpoint
    window.location.href = '/admin/login';
  };

  return (
    <div className="app">
      <main className="main">
        <div className="login-card">
          <h1 className="login-title">Patrol Scores Admin</h1>

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
      <footer className="footer">
        <div className="footer-build">
          Build: {__BUILD_TIME__}
        </div>
      </footer>
    </div>
  );
}
