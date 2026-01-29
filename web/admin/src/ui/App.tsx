import { useWorkerBootstrap } from './hooks';
import {makeSelectPatrolById, selectUserScoreForPatrolKey, useAppSelector} from './state';
import { selectIsAuthenticated, selectUserName, selectGlobalError, selectSections, selectSelectedSection } from './state';
import { LoginPage, MessageCard, ErrorDialog } from './components';
import {type ReactElement, useMemo} from "react";

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
  const selectedSection = useAppSelector(selectSelectedSection);

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
            <p>Selected section ID: {selectedSection?.id ?? 'none'}</p>
            <p>Selected section Name: {selectedSection?.name ?? 'none'}</p>
            <h3>Sections:</h3>
            <ul>
              {sections.map(s => (
                <li key={s.id}>
                  {s.name} ({s.groupName}) ({s.state})
                </li>
              ))}
            </ul>
              <h3>Patrols:</h3>
              {selectedSection?.patrols.map((patrol:string) => <TmpPatrolDiagnostic key={patrol} patrolId={patrol}/>)}
          </div>
        </main>
      </div>
    </>
  );
}

function TmpPatrolDiagnostic(props: {patrolId: string}) : ReactElement {
    const selectPatrolById = useMemo(makeSelectPatrolById, []);
    const patrol = useAppSelector(state => selectPatrolById(state, props.patrolId));
    const userScore : number = useAppSelector(state => selectUserScoreForPatrolKey(state, props.patrolId));
    if(!patrol) return <span>WARN: Patrol id {props.patrolId} not found in patrol map</span>;
    return <>
        <h4>{patrol.name}</h4>
        <ul>
            <li><strong>Score:</strong>{patrol.committedScore}</li>
            <li><strong>Pending:</strong>{patrol.pendingScore}</li>
            <li><strong>User:</strong>{userScore}</li>
            {patrol.errorMessage && <li><strong>Error</strong>{patrol.errorMessage}</li>}
        </ul>
    </>
}