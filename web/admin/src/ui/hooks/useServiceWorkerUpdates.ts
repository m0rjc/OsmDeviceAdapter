import { useEffect, useRef } from 'react';
import { registerSW } from 'virtual:pwa-register';
import { useAppDispatch } from '../state';
import { setUpdateAvailable } from '../state/appSlice';

/**
 * Hook to manage PWA service worker update lifecycle.
 *
 * This hook:
 * 1. Registers the service worker using vite-plugin-pwa
 * 2. Listens for update availability
 * 3. Dispatches Redux actions to update global state
 * 4. Stores the updateSW function for later use
 *
 * Separation of concerns:
 * - This hook handles PWA lifecycle (framework concern)
 * - useWorkerBootstrap handles business logic worker (app concern)
 * - Both happen to use the same underlying service worker
 *
 * Usage:
 * Call this hook once at app initialization (in App.tsx).
 */
export function useServiceWorkerUpdates() {
  const dispatch = useAppDispatch();
  const updateSWRef = useRef<(() => Promise<void>) | null>(null);

  useEffect(() => {
    // Gracefully handle browsers without service worker support
    if (!('serviceWorker' in navigator)) {
      console.log('Service Worker not supported in this browser');
      return;
    }

    // Register service worker and set up update detection
    const updateSW = registerSW({
      onNeedRefresh() {
        console.log('New service worker version available');
        dispatch(setUpdateAvailable(true));
      },
      onOfflineReady() {
        console.log('App is ready to work offline');
        // Could dispatch an action to show a toast notification
      },
    });

    // Store the update function so components can trigger updates
    updateSWRef.current = updateSW;

    // Expose updateSW globally for UpdatePrompt component to use
    // This avoids prop drilling and keeps the hook self-contained
    if (typeof window !== 'undefined') {
      (window as any).__updateServiceWorker = updateSW;
    }
  }, [dispatch]);
}

/**
 * Helper function to trigger service worker update.
 * This is called by the UpdatePrompt component when user clicks "Update now".
 *
 * @returns Promise that resolves when update is triggered
 */
export async function applyServiceWorkerUpdate(): Promise<void> {
  if (typeof window !== 'undefined' && (window as any).__updateServiceWorker) {
    await (window as any).__updateServiceWorker();
    // Reload the page to activate the new service worker
    window.location.reload();
  }
}
