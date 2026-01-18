/**
 * Offline queue for score updates using IndexedDB.
 * When offline, score changes are stored locally and synced when back online.
 */

import type { ScoreUpdate } from './types';

const DB_NAME = 'penguin-patrol-scores';
const DB_VERSION = 1;
const STORE_NAME = 'pending-updates';

export interface PendingUpdate {
  id?: number; // Auto-incremented by IndexedDB
  sectionId: number;
  updates: ScoreUpdate[];
  createdAt: number;
}

let dbPromise: Promise<IDBDatabase> | null = null;

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;

  dbPromise = new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION);

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);

    request.onupgradeneeded = (event) => {
      const db = (event.target as IDBOpenDBRequest).result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        const store = db.createObjectStore(STORE_NAME, {
          keyPath: 'id',
          autoIncrement: true,
        });
        store.createIndex('sectionId', 'sectionId', { unique: false });
      }
    };
  });

  return dbPromise;
}

/**
 * Queue a score update for later sync
 */
export async function queueUpdate(sectionId: number, updates: ScoreUpdate[]): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readwrite');
    const store = tx.objectStore(STORE_NAME);

    const pendingUpdate: PendingUpdate = {
      sectionId,
      updates,
      createdAt: Date.now(),
    };

    const request = store.add(pendingUpdate);
    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve();
  });
}

/**
 * Get all pending updates
 */
export async function getPendingUpdates(): Promise<PendingUpdate[]> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readonly');
    const store = tx.objectStore(STORE_NAME);
    const request = store.getAll();

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

/**
 * Get pending updates for a specific section
 */
export async function getPendingUpdatesForSection(sectionId: number): Promise<PendingUpdate[]> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readonly');
    const store = tx.objectStore(STORE_NAME);
    const index = store.index('sectionId');
    const request = index.getAll(sectionId);

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

/**
 * Remove a pending update after successful sync
 */
export async function removePendingUpdate(id: number): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readwrite');
    const store = tx.objectStore(STORE_NAME);
    const request = store.delete(id);

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve();
  });
}

/**
 * Clear all pending updates (e.g., after successful bulk sync)
 */
export async function clearAllPendingUpdates(): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, 'readwrite');
    const store = tx.objectStore(STORE_NAME);
    const request = store.clear();

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve();
  });
}

/**
 * Get total pending points per patrol for a section
 * Aggregates all queued updates
 */
export async function getPendingPointsByPatrol(sectionId: number): Promise<Map<string, number>> {
  const pending = await getPendingUpdatesForSection(sectionId);
  const pointsByPatrol = new Map<string, number>();

  for (const update of pending) {
    for (const scoreUpdate of update.updates) {
      const current = pointsByPatrol.get(scoreUpdate.patrolId) || 0;
      pointsByPatrol.set(scoreUpdate.patrolId, current + scoreUpdate.points);
    }
  }

  return pointsByPatrol;
}

/**
 * Check if we're currently online
 */
export function isOnline(): boolean {
  return navigator.onLine;
}

/**
 * Register for online/offline events
 */
export function onConnectivityChange(callback: (online: boolean) => void): () => void {
  const handleOnline = () => callback(true);
  const handleOffline = () => callback(false);

  window.addEventListener('online', handleOnline);
  window.addEventListener('offline', handleOffline);

  return () => {
    window.removeEventListener('online', handleOnline);
    window.removeEventListener('offline', handleOffline);
  };
}

/**
 * Request a background sync (if supported)
 */
export async function requestBackgroundSync(): Promise<boolean> {
  if ('serviceWorker' in navigator && 'sync' in ServiceWorkerRegistration.prototype) {
    try {
      const registration = await navigator.serviceWorker.ready;
      await (registration as ServiceWorkerRegistration & { sync: { register: (tag: string) => Promise<void> } }).sync.register('sync-scores');
      return true;
    } catch {
      console.warn('Background sync registration failed');
      return false;
    }
  }
  return false;
}
