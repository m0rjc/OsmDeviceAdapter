/**
 * Client-side outbox pattern for score updates using IndexedDB.
 *
 * Two-tier architecture:
 * 1. working-scores: Mutable store for user's current edits (running total per patrol)
 * 2. client-outbox: Immutable store for pending submissions with stable idempotency keys
 *
 * Flow:
 * - User changes score → update working-scores
 * - User submits → snapshot working-scores to client-outbox with idempotency keys
 * - Clear working-scores for submitted patrols
 * - On 200/202: delete from client-outbox (server owns it)
 * - On network error: keep in client-outbox, retry with same keys
 */

const DB_NAME = 'penguin-patrol-scores';
const DB_VERSION = 3; // Incremented for unique constraint fix on idempotencyKey
const WORKING_SCORES_STORE = 'working-scores';
const CLIENT_OUTBOX_STORE = 'client-outbox';

export interface WorkingScore {
  sectionId: number;
  patrolId: string;
  points: number; // Running total delta from user's edits
  updatedAt: number; // Timestamp of last change
}

export interface ClientOutboxEntry {
  id?: number; // Auto-incremented by IndexedDB
  idempotencyKey: string; // UUID generated when entry created
  sectionId: number;
  patrolId: string;
  points: number; // Delta to apply (frozen at submission time)
  status: 'pending' | 'syncing' | 'server-pending'; // server-pending means server accepted (202)
  createdAt: number; // When entry was created
  lastAttemptAt?: number; // Last sync attempt timestamp
  error?: string; // Last error message if sync failed
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
      const oldVersion = event.oldVersion;

      // Create working-scores store
      if (!db.objectStoreNames.contains(WORKING_SCORES_STORE)) {
        const workingStore = db.createObjectStore(WORKING_SCORES_STORE, {
          keyPath: ['sectionId', 'patrolId'],
        });
        workingStore.createIndex('sectionId', 'sectionId', { unique: false });
      }

      // Create client-outbox store
      if (!db.objectStoreNames.contains(CLIENT_OUTBOX_STORE)) {
        const outboxStore = db.createObjectStore(CLIENT_OUTBOX_STORE, {
          keyPath: 'id',
          autoIncrement: true,
        });
        outboxStore.createIndex('idempotencyKey', 'idempotencyKey', { unique: false });
        outboxStore.createIndex('sectionId', 'sectionId', { unique: false });
        outboxStore.createIndex('status', 'status', { unique: false });
      }

      // Migrate old pending-updates store if upgrading from v1
      if (oldVersion < 2 && db.objectStoreNames.contains('pending-updates')) {
        // Delete old store - users will need to re-enter any pending changes
        // This is acceptable for the upgrade as it's a cleaner migration path
        db.deleteObjectStore('pending-updates');
      }

      // Fix unique constraint on idempotencyKey (v2 -> v3)
      if (oldVersion < 3 && db.objectStoreNames.contains(CLIENT_OUTBOX_STORE)) {
        const tx = (event.target as IDBOpenDBRequest).transaction;
        if (tx) {
          const outboxStore = tx.objectStore(CLIENT_OUTBOX_STORE);
          // Delete and recreate the index without unique constraint
          if (outboxStore.indexNames.contains('idempotencyKey')) {
            outboxStore.deleteIndex('idempotencyKey');
          }
          outboxStore.createIndex('idempotencyKey', 'idempotencyKey', { unique: false });
        }
      }
    };
  });

  return dbPromise;
}

// ==================== Working Scores API ====================

/**
 * Update a patrol's working score (mutable running total)
 */
export async function updateWorkingScore(
  sectionId: number,
  patrolId: string,
  pointsDelta: number
): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(WORKING_SCORES_STORE, 'readwrite');
    const store = tx.objectStore(WORKING_SCORES_STORE);

    // Get current value
    const getRequest = store.get([sectionId, patrolId]);

    getRequest.onsuccess = () => {
      const current = getRequest.result as WorkingScore | undefined;
      const newPoints = (current?.points || 0) + pointsDelta;

      if (newPoints === 0) {
        // Zero out - remove entry
        store.delete([sectionId, patrolId]);
      } else {
        // Update or create entry
        const workingScore: WorkingScore = {
          sectionId,
          patrolId,
          points: newPoints,
          updatedAt: Date.now(),
        };
        store.put(workingScore);
      }
    };

    getRequest.onerror = () => reject(getRequest.error);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * Get all working scores for a section
 */
export async function getWorkingScores(sectionId: number): Promise<Map<string, number>> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(WORKING_SCORES_STORE, 'readonly');
    const store = tx.objectStore(WORKING_SCORES_STORE);
    const index = store.index('sectionId');
    const request = index.getAll(sectionId);

    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const scores = request.result as WorkingScore[];
      const map = new Map<string, number>();
      for (const score of scores) {
        map.set(score.patrolId, score.points);
      }
      resolve(map);
    };
  });
}

/**
 * Clear working scores for specific patrols (after submission)
 */
export async function clearWorkingScores(sectionId: number, patrolIds: string[]): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(WORKING_SCORES_STORE, 'readwrite');
    const store = tx.objectStore(WORKING_SCORES_STORE);

    for (const patrolId of patrolIds) {
      store.delete([sectionId, patrolId]);
    }

    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * Clear all working scores for a section
 */
export async function clearAllWorkingScores(sectionId: number): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(WORKING_SCORES_STORE, 'readwrite');
    const store = tx.objectStore(WORKING_SCORES_STORE);
    const index = store.index('sectionId');
    const request = index.openCursor(sectionId);

    request.onsuccess = () => {
      const cursor = request.result;
      if (cursor) {
        cursor.delete();
        cursor.continue();
      }
    };

    request.onerror = () => reject(request.error);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

// ==================== Client Outbox API ====================

/**
 * Generate a UUID v4 idempotency key
 */
function generateIdempotencyKey(): string {
  return crypto.randomUUID();
}

/**
 * Snapshot working scores into client outbox for submission
 * Returns the idempotency key and outbox entries created
 * Note: patrolName is NOT stored - server will fetch authoritative name from OSM
 */
export async function snapshotToOutbox(
  sectionId: number,
  patrolScores: Array<{ patrolId: string; points: number }>
): Promise<{ baseKey: string; entries: ClientOutboxEntry[] }> {
  const db = await openDB();
  const baseKey = generateIdempotencyKey();
  const entries: ClientOutboxEntry[] = [];

  return new Promise((resolve, reject) => {
    const tx = db.transaction([CLIENT_OUTBOX_STORE, WORKING_SCORES_STORE], 'readwrite');
    const outboxStore = tx.objectStore(CLIENT_OUTBOX_STORE);
    const workingStore = tx.objectStore(WORKING_SCORES_STORE);

    // Create outbox entries
    for (const patrol of patrolScores) {
      const entry: ClientOutboxEntry = {
        idempotencyKey: baseKey,
        sectionId,
        patrolId: patrol.patrolId,
        points: patrol.points,
        status: 'pending',
        createdAt: Date.now(),
      };
      outboxStore.add(entry);
      entries.push(entry);

      // Clear working score for this patrol
      workingStore.delete([sectionId, patrol.patrolId]);
    }

    tx.oncomplete = () => resolve({ baseKey, entries });
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * Get all client outbox entries
 */
export async function getOutboxEntries(): Promise<ClientOutboxEntry[]> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readonly');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const request = store.getAll();

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

/**
 * Get outbox entries by idempotency key (all entries in a batch)
 */
export async function getOutboxEntriesByKey(idempotencyKey: string): Promise<ClientOutboxEntry[]> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readonly');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const index = store.index('idempotencyKey');
    const request = index.getAll(idempotencyKey);

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

/**
 * Get pending outbox entries (status = 'pending')
 */
export async function getPendingOutboxEntries(): Promise<ClientOutboxEntry[]> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readonly');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const index = store.index('status');
    const request = index.getAll('pending');

    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

/**
 * Mark outbox entry as syncing
 */
export async function markOutboxEntrySyncing(id: number): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readwrite');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const getRequest = store.get(id);

    getRequest.onsuccess = () => {
      const entry = getRequest.result as ClientOutboxEntry;
      if (entry) {
        entry.status = 'syncing';
        entry.lastAttemptAt = Date.now();
        store.put(entry);
      }
    };

    getRequest.onerror = () => reject(getRequest.error);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * Mark outbox entries by idempotency key as server-pending or delete them
 * Call this on 202 Accepted or 200 OK response
 */
export async function handleServerResponse(
  idempotencyKey: string,
  status: 'accepted' | 'completed'
): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readwrite');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const index = store.index('idempotencyKey');
    const request = index.getAll(idempotencyKey);

    request.onsuccess = () => {
      const entries = request.result as ClientOutboxEntry[];
      for (const entry of entries) {
        if (status === 'accepted') {
          // Server accepted (202) - mark as server-pending and delete from client
          // We'll rely on server-side pending count from session endpoint
          if (entry.id !== undefined) {
            store.delete(entry.id);
          }
        } else {
          // Server completed (200) - delete from client
          if (entry.id !== undefined) {
            store.delete(entry.id);
          }
        }
      }
    };

    request.onerror = () => reject(request.error);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * Mark outbox entries by idempotency key as failed with error
 */
export async function markOutboxEntriesFailed(
  idempotencyKey: string,
  error: string
): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readwrite');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const index = store.index('idempotencyKey');
    const request = index.getAll(idempotencyKey);

    request.onsuccess = () => {
      const entries = request.result as ClientOutboxEntry[];
      for (const entry of entries) {
        entry.status = 'pending'; // Reset to pending for retry
        entry.error = error;
        entry.lastAttemptAt = Date.now();
        store.put(entry);
      }
    };

    request.onerror = () => reject(request.error);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * Delete outbox entries by idempotency key (on 4xx errors - don't retry)
 */
export async function deleteOutboxEntries(idempotencyKey: string): Promise<void> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readwrite');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const index = store.index('idempotencyKey');
    const request = index.getAll(idempotencyKey);

    request.onsuccess = () => {
      const entries = request.result as ClientOutboxEntry[];
      for (const entry of entries) {
        if (entry.id !== undefined) {
          store.delete(entry.id);
        }
      }
    };

    request.onerror = () => reject(request.error);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
}

/**
 * Get pending points by patrol (for UI badges)
 * Aggregates client outbox entries by patrol
 */
export async function getPendingPointsByPatrol(sectionId: number): Promise<Map<string, number>> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readonly');
    const outboxStore = tx.objectStore(CLIENT_OUTBOX_STORE);
    const outboxIndex = outboxStore.index('sectionId');
    const request = outboxIndex.getAll(sectionId);

    request.onsuccess = () => {
      const entries = request.result as ClientOutboxEntry[];
      const pointsByPatrol = new Map<string, number>();

      for (const entry of entries) {
        if (entry.status === 'pending' || entry.status === 'syncing') {
          const current = pointsByPatrol.get(entry.patrolId) || 0;
          pointsByPatrol.set(entry.patrolId, current + entry.points);
        }
      }

      resolve(pointsByPatrol);
    };

    request.onerror = () => reject(request.error);
  });
}

/**
 * Count total pending entries (client outbox + working scores indicator)
 */
export async function countPendingEntries(sectionId: number): Promise<{
  clientOutbox: number;
  workingScores: number;
}> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction([CLIENT_OUTBOX_STORE, WORKING_SCORES_STORE], 'readonly');

    let clientOutboxCount = 0;
    let workingScoresCount = 0;
    let completed = 0;

    const checkComplete = () => {
      completed++;
      if (completed === 2) {
        resolve({ clientOutbox: clientOutboxCount, workingScores: workingScoresCount });
      }
    };

    // Count client outbox entries (pending and syncing)
    const outboxStore = tx.objectStore(CLIENT_OUTBOX_STORE);
    const outboxIndex = outboxStore.index('sectionId');
    const outboxRequest = outboxIndex.getAll(sectionId);
    outboxRequest.onsuccess = () => {
      const entries = outboxRequest.result as ClientOutboxEntry[];
      clientOutboxCount = entries.filter(e => e.status === 'pending' || e.status === 'syncing').length;
      checkComplete();
    };
    outboxRequest.onerror = () => reject(outboxRequest.error);

    // Count working scores
    const workingStore = tx.objectStore(WORKING_SCORES_STORE);
    const workingIndex = workingStore.index('sectionId');
    const workingRequest = workingIndex.count(sectionId);
    workingRequest.onsuccess = () => {
      workingScoresCount = workingRequest.result;
      checkComplete();
    };
    workingRequest.onerror = () => reject(workingRequest.error);

    tx.onerror = () => reject(tx.error);
  });
}

// ==================== Utility Functions ====================

/**
 * Reset stuck 'syncing' entries back to 'pending'
 * Call this on app startup to recover from interrupted syncs
 */
export async function resetStuckSyncingEntries(): Promise<number> {
  const db = await openDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(CLIENT_OUTBOX_STORE, 'readwrite');
    const store = tx.objectStore(CLIENT_OUTBOX_STORE);
    const index = store.index('status');
    const request = index.getAll('syncing');

    let resetCount = 0;

    request.onsuccess = () => {
      const entries = request.result as ClientOutboxEntry[];
      for (const entry of entries) {
        // Reset to pending
        entry.status = 'pending';
        entry.error = 'Previous sync interrupted';
        store.put(entry);
        resetCount++;
      }
    };

    request.onerror = () => reject(request.error);
    tx.oncomplete = () => resolve(resetCount);
    tx.onerror = () => reject(tx.error);
  });
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

/**
 * Manually trigger a sync by posting a message to the service worker
 * This is a fallback for when background sync API doesn't work (e.g., iOS)
 */
export async function manualSyncPendingScores(): Promise<void> {
  if ('serviceWorker' in navigator) {
    const registration = await navigator.serviceWorker.ready;
    if (registration.active) {
      registration.active.postMessage({ type: 'MANUAL_SYNC' });
    }
  }
}
