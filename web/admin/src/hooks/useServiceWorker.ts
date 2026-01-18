import { useState, useEffect, useCallback } from 'react';
import { registerSW } from 'virtual:pwa-register';

export type SyncMessageType = 'SYNC_SUCCESS' | 'SYNC_ERROR' | 'SYNC_AUTH_REQUIRED';

export interface SyncMessage {
  type: SyncMessageType;
  sectionId?: number;
  error?: string;
}

interface UseServiceWorkerReturn {
  needRefresh: boolean;
  updateServiceWorker: () => void;
  dismissUpdate: () => void;
}

export function useServiceWorker(): UseServiceWorkerReturn {
  const [needRefresh, setNeedRefresh] = useState(false);
  const [updateSW, setUpdateSW] = useState<(() => Promise<void>) | null>(null);

  useEffect(() => {
    const update = registerSW({
      onNeedRefresh() {
        setNeedRefresh(true);
      },
      onOfflineReady() {
        // App is ready to work offline, could show a toast if desired
      },
    });
    setUpdateSW(() => update);
  }, []);

  const updateServiceWorker = useCallback(() => {
    if (updateSW) {
      updateSW();
    }
  }, [updateSW]);

  const dismissUpdate = useCallback(() => {
    setNeedRefresh(false);
  }, []);

  return {
    needRefresh,
    updateServiceWorker,
    dismissUpdate,
  };
}

/**
 * Hook to listen for sync messages from the service worker.
 * Use this in components that need to react to background sync events.
 */
export function useSyncMessages(
  onSyncSuccess?: (sectionId?: number) => void,
  onSyncError?: (error: string, sectionId?: number) => void,
  onAuthRequired?: (error: string) => void
): void {
  useEffect(() => {
    if (!('serviceWorker' in navigator)) {
      return;
    }

    const handleMessage = (event: MessageEvent<SyncMessage>) => {
      const { type, sectionId, error } = event.data;

      switch (type) {
        case 'SYNC_SUCCESS':
          onSyncSuccess?.(sectionId);
          break;
        case 'SYNC_ERROR':
          onSyncError?.(error || 'Sync failed', sectionId);
          break;
        case 'SYNC_AUTH_REQUIRED':
          onAuthRequired?.(error || 'Session expired. Please log in again to sync your changes.');
          break;
      }
    };

    navigator.serviceWorker.addEventListener('message', handleMessage);
    return () => {
      navigator.serviceWorker.removeEventListener('message', handleMessage);
    };
  }, [onSyncSuccess, onSyncError, onAuthRequired]);
}
