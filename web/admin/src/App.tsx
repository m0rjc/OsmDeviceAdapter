import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider } from './context';
import { useAuth } from './hooks';
import { ToastProvider, Header, SectionSelector, ScoreEntry, Loading } from './components';
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

function App() {
  return (
    <BrowserRouter basename="/admin">
      <ToastProvider>
        <AuthProvider>
          <Routes>
            <Route path="/" element={<Navigate to="/scores" replace />} />
            <Route path="/scores" element={<ScoresPage />} />
          </Routes>
        </AuthProvider>
      </ToastProvider>
    </BrowserRouter>
  );
}

export default App;
