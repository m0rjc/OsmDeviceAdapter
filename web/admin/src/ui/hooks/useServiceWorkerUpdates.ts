import { useEffect } from 'react';
import { registerSW } from 'virtual:pwa-register';

/**
 * Hook to manage PWA service worker auto-update lifecycle.
 *
 * This hook:
 * 1. Registers the service worker using vite-plugin-pwa (autoUpdate mode)
 * 2. Listens for controller changes (new SW activated)
 * 3. Reloads the page when a new SW takes control
 *
 * With autoUpdate, the new service worker calls skipWaiting() automatically.
 * The controllerchange listener ensures the page reloads to use the new assets.
 * This is safe because pending score data is persisted in IndexedDB.
 *
 * Usage:
 * Call this hook once at app initialization (in App.tsx).
 */
export function useServiceWorkerUpdates() {
  useEffect(() => {
    if (!('serviceWorker' in navigator)) {
      console.log('Service Worker not supported in this browser');
      return;
    }

    // Register service worker with auto-update (skipWaiting called automatically)
    registerSW({
      onOfflineReady() {
        console.log('App is ready to work offline');
      },
    });

    // When a new SW activates and takes control, reload to use new assets.
    // This fires after skipWaiting() + clients.claim() in the new SW.
    const onControllerChange = () => {
      console.log('New service worker activated, reloading page');
      window.location.reload();
    };

    navigator.serviceWorker.addEventListener('controllerchange', onControllerChange);

    return () => {
      navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange);
    };
  }, []);
}
