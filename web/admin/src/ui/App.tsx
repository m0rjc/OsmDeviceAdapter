import {useServiceWorkerUpdates, useWorkerBootstrap} from './hooks';
import {selectGlobalError, selectIsAuthenticated, selectIsLoading, selectUserName, useAppSelector} from './state';
import {ErrorDialog, LoadingBanner, LoginPage, MessageCard, ScoreEntryPage, ToastProvider, UpdatePrompt} from './components';

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
                <UpdatePrompt/>
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
                <UpdatePrompt/>
                <LoadingBanner/>
            </ToastProvider>
        );
    }

    // Show login page if not authenticated
    if (!isAuthenticated) {
        return (
            <ToastProvider>
                <UpdatePrompt/>
                <ErrorDialog/>
                <LoginPage/>
            </ToastProvider>
        );
    }

    // Show authenticated content
    return (
        <ToastProvider>
            <UpdatePrompt/>
            <ErrorDialog/>
            <div className="app">
                <header className="header">
                    <h1>Patrol Scores Admin</h1>
                    <p>Logged in as: {userName}</p>
                </header>
                <main className="main">
                    <ScoreEntryPage csrfToken=""/>
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