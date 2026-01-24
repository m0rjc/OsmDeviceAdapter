import { useAuth } from '../hooks';

export function Header() {
  const { user, logout } = useAuth();

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
          {user && <span className="header-user-name">{user.name}</span>}
          <button className="btn btn-text" onClick={logout}>
            Logout
          </button>
        </div>
      </div>
      <div className="header-build">Build: {buildLabel}</div>
    </header>
  );
}
