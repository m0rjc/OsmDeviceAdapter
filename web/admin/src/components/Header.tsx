import { useAuth } from '../hooks';

export function Header() {
  const { user, logout } = useAuth();

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
    </header>
  );
}
