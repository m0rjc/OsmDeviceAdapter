import { useWorkerBootstrap } from './hooks';
import { useAppSelector } from './state/hooks';
import { selectIsAuthenticated, selectUserName } from './state/userSlice';
import { selectGlobalError, selectSections, selectSelectedSectionId } from './state/appSlice';
import { LoginPage, MessageCard, ErrorDialog } from './components';

/**
 * Main application component using Redux and worker-based architecture.
 *
 * This component:
 * 1. Bootstraps the worker connection
 * 2. Shows login page if not authenticated
 * 3. Shows global error message if session mismatch occurs
 * 4. Shows authenticated content when logged in
 */
export function App() {
  // Bootstrap worker and set up message handlers
  useWorkerBootstrap();

  const isAuthenticated = useAppSelector(selectIsAuthenticated);
  const userName = useAppSelector(selectUserName);
  const globalError = useAppSelector(selectGlobalError);
  const sections = useAppSelector(selectSections);
  const selectedSectionId = useAppSelector(selectSelectedSectionId);

  // Show global error if present (e.g., session mismatch)
  if (globalError) {
    return (
      <>
        <ErrorDialog />
        <div className="app">
          <main className="main">
            <MessageCard
              title="Session Error"
              message={globalError}
              action={{
                label: 'Reload Page',
                onClick: () => window.location.reload(),
              }}
              className="error-state"
            />
          </main>
        </div>
      </>
    );
  }

  // Show login page if not authenticated
  if (!isAuthenticated) {
    return (
      <>
        <ErrorDialog />
        <LoginPage />
      </>
    );
  }

  // Show authenticated content (temporary bootstrap checkpoint)
  return (
    <>
      <ErrorDialog />
      <div className="app">
        <header className="header">
          <h1>Patrol Scores Admin</h1>
          <p>Logged in as: {userName}</p>
        </header>
        <main className="main">
          <div className="bootstrap-checkpoint">
            <h2>Bootstrap Checkpoint</h2>
            <p>Sections loaded: {sections.length}</p>
            <p>Selected section ID: {selectedSectionId ?? 'none'}</p>
            <h3>Sections:</h3>
            <ul>
              {sections.map(s => (
                <li key={s.id}>
                  {s.id === selectedSectionId ? '✓ ' : ''}
                  {s.name} ({s.groupName})
                  {s.patrols !== undefined && ` - ${s.patrols.length} patrols loaded`}
                </li>
              ))}
            </ul>
          </div>
        </main>
      </div>
    </>
  );
}
