import { useEffect } from 'react';

const PENDING_SYNC_MESSAGE_KEY = 'pendingSyncAuthMessage';

/**
 * Global handler for SYNC_AUTH_REQUIRED messages from the service worker.
 * This component should be mounted at a high level (inside ToastProvider, outside routes)
 * so it's always active regardless of the current route.
 *
 * When a sync fails due to authentication issues, this stores a message in
 * sessionStorage and redirects to the sign-in page. The LoginPage component
 * will display the message.
 */
export function SyncAuthHandler() {
  useEffect(() => {
    if (!('serviceWorker' in navigator)) {
      return;
    }

    const handleMessage = (event: MessageEvent) => {
      if (event.data?.type === 'SYNC_AUTH_REQUIRED') {
        // Store message for display on login page
        const message = event.data.error || 'Session expired. Please log in again to sync your changes.';
        sessionStorage.setItem(PENDING_SYNC_MESSAGE_KEY, message);

        // Redirect to sign-in - pending updates remain in IndexedDB
        window.location.href = '/admin/signin';
      }
    };

    navigator.serviceWorker.addEventListener('message', handleMessage);
    return () => {
      navigator.serviceWorker.removeEventListener('message', handleMessage);
    };
  }, []);

  return null;
}

/**
 * Get and clear the pending sync auth message from sessionStorage.
 * Call this on the login page to retrieve and display the message.
 */
export function getPendingSyncMessage(): string | null {
  const message = sessionStorage.getItem(PENDING_SYNC_MESSAGE_KEY);
  if (message) {
    sessionStorage.removeItem(PENDING_SYNC_MESSAGE_KEY);
  }
  return message;
}
