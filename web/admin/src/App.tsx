import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider } from './context';
import { useAuth } from './hooks';
import { ToastProvider, Header, SectionSelector, ScoreEntry, Loading, LoginPage, UpdatePrompt, SyncAuthHandler } from './components';
import './styles.css';

function ScoresPage() {
  const { isLoading, error } = useAuth();

  if (isLoading) {
    return (
      <div className="app">
        <Loading message="Loading..." />
      </div>
    );
  }

  if (error) {
    return (
      <div className="app">
        <div className="main">
          <div className="error-message">
            <p>{error}</p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="app">
      <Header />
      <main className="main">
        <SectionSelector />
        <ScoreEntry />
      </main>
    </div>
  );
}

function AuthenticatedRoutes() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/" element={<Navigate to="/scores" replace />} />
        <Route path="/scores" element={<ScoresPage />} />
      </Routes>
    </AuthProvider>
  );
}

function App() {
  return (
    <BrowserRouter basename="/admin">
      <ToastProvider>
        <UpdatePrompt />
        <SyncAuthHandler />
        <Routes>
          <Route path="/signin" element={<LoginPage />} />
          <Route path="/*" element={<AuthenticatedRoutes />} />
        </Routes>
      </ToastProvider>
    </BrowserRouter>
  );
}

export default App;
