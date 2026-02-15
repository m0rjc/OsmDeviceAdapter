import {useState} from 'react';
import {useServiceWorkerUpdates, useWorkerBootstrap} from './hooks';
import {selectGlobalError, selectIsAuthenticated, selectIsLoading, selectSelectedSectionId, selectUserName, useAppSelector} from './state';
import {ErrorDialog, LoadingBanner, LoginPage, MessageCard, ScoreEntryPage, SettingsPage, SectionSelector, TeamsPage, ScoreboardSettings, ToastProvider} from './components';

type Tab = 'scores' | 'settings' | 'teams';

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

    // Set up PWA lifecycle for service worker updates
    useServiceWorkerUpdates();

    const isLoading = useAppSelector(selectIsLoading);
    const isAuthenticated = useAppSelector(selectIsAuthenticated);
    const userName = useAppSelector(selectUserName);
    const globalError = useAppSelector(selectGlobalError);

    // Show global error if present (e.g., worker initialization failure, session mismatch)
    // This takes priority over loading state
    if (globalError) {
        return (
            <ToastProvider>
                <ErrorDialog/>
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
            </ToastProvider>
        );
    }

    // Show loading banner while fetching user profile
    if (isLoading) {
        return (
            <ToastProvider>
                <LoadingBanner/>
            </ToastProvider>
        );
    }

    // Show login page if not authenticated
    if (!isAuthenticated) {
        return (
            <ToastProvider>
                <ErrorDialog/>
                <LoginPage/>
            </ToastProvider>
        );
    }

    // Show authenticated content
    return <AuthenticatedApp userName={userName} />;
}

/**
 * Authenticated app content with tab navigation.
 */
function AuthenticatedApp({ userName }: { userName: string | null }) {
    const [activeTab, setActiveTab] = useState<Tab>('scores');
    const selectedSectionId = useAppSelector(selectSelectedSectionId);
    const isAdhocSection = selectedSectionId === 0;

    // Switch away from teams tab if user selects a non-adhoc section
    const effectiveTab = (activeTab === 'teams' && !isAdhocSection) ? 'scores' : activeTab;

    return (
        <ToastProvider>
            <ErrorDialog/>
            <div className="app">
                <header className="header">
                    <div className="header-content">
                        <h1 className="header-title">Patrol Scores Admin</h1>
                        <div className="header-user">
                            <span className="header-user-name">{userName}</span>
                        </div>
                    </div>
                </header>
                <nav className="nav-tabs">
                    <button
                        className={`nav-tab ${effectiveTab === 'scores' ? 'active' : ''}`}
                        onClick={() => setActiveTab('scores')}
                    >
                        Scores
                    </button>
                    {isAdhocSection && (
                        <button
                            className={`nav-tab ${effectiveTab === 'teams' ? 'active' : ''}`}
                            onClick={() => setActiveTab('teams')}
                        >
                            Teams
                        </button>
                    )}
                    <button
                        className={`nav-tab ${effectiveTab === 'settings' ? 'active' : ''}`}
                        onClick={() => setActiveTab('settings')}
                    >
                        Settings
                    </button>
                </nav>
                <main className="main">
                    <SectionSelector />
                    {effectiveTab === 'scores' && <ScoreEntryPage csrfToken=""/>}
                    {effectiveTab === 'teams' && <TeamsPage />}
                    {effectiveTab === 'settings' && (
                        <>
                            <SettingsPage />
                            <ScoreboardSettings />
                        </>
                    )}
                </main>
                <footer className="footer">
                    <div className="footer-build">
                        Build: {__BUILD_TIME__}
                    </div>
                </footer>
            </div>
        </ToastProvider>
    );
}