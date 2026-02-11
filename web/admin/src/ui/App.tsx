import {useState} from 'react';
import {useServiceWorkerUpdates, useWorkerBootstrap} from './hooks';
import {selectGlobalError, selectIsAuthenticated, selectIsLoading, selectUserName, useAppSelector} from './state';
import {ErrorDialog, LoadingBanner, LoginPage, MessageCard, ScoreEntryPage, SettingsPage, SectionSelector, ToastProvider} from './components';

type Tab = 'scores' | 'settings';

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
                        className={`nav-tab ${activeTab === 'scores' ? 'active' : ''}`}
                        onClick={() => setActiveTab('scores')}
                    >
                        Scores
                    </button>
                    <button
                        className={`nav-tab ${activeTab === 'settings' ? 'active' : ''}`}
                        onClick={() => setActiveTab('settings')}
                    >
                        Settings
                    </button>
                </nav>
                <main className="main">
                    <SectionSelector />
                    {activeTab === 'scores' && <ScoreEntryPage csrfToken=""/>}
                    {activeTab === 'settings' && <SettingsPage />}
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