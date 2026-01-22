const DB_NAME = 'penguin-patrol-scores';
const DB_VERSION = 3;
const V2 = {
    CLIENT_OUTBOX_STORE: 'client-outbox'
};
const PENDING_POINTS_STORE = 'pending-scores-v3'

export class OutboxEntry {
    public readonly key: string;
    public readonly sectionId: string;
    public readonly patrolId: string;
    public scoreDelta: number;
    /**
     * retryAfter is the POSIX timestamp to retry after:
     * - 0 = can be immediately retried
     * - positive value = retry after this timestamp
     * - -1 = permanent error, don't retry (client should acknowledge and clear)
     */
    public retryAfter: number;
    /** errorMessage contains the error description for failed sync attempts (both temporary and permanent) */
    public errorMessage?: string;

    public constructor(sectionId: string, patrolId: string, scoreDelta: number) {
        this.sectionId = sectionId;
        this.patrolId = patrolId;
        this.scoreDelta = scoreDelta;
        this.retryAfter = 0;
        this.key = getKey(sectionId, patrolId)
    }
}

export function OpenOfflineStore() : Promise<OfflineStore> {
    return new Promise<OfflineStore>((resolve, reject) => {
        const request = indexedDB.open(DB_NAME, DB_VERSION);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(new OfflineStore(request.result));
        request.onupgradeneeded = handleDatabaseUpgrade
    })
}

function handleDatabaseUpgrade(event : IDBVersionChangeEvent) {
    const request = event.target as IDBOpenDBRequest
    const db = request.result;
    console.log(`[OfflineStore] Database upgrade: v${event.oldVersion} -> v${DB_VERSION}`);
    console.log(`[OfflineStore] Existing stores:`, Array.from(db.objectStoreNames));

    switch (event.oldVersion) {
        case 0:
            // This is a new database
            console.log('[OfflineStore] Creating new database');
            createPendingPointsStore(db);
            break;
        case 1: case 2:
            // Versions 1 and 2 have only existed in my personal dev environment, so we can just destroy it.
            console.log('[OfflineStore] Upgrading from v1/v2');
            // Check if old store exists before deleting
            if (db.objectStoreNames.contains(V2.CLIENT_OUTBOX_STORE)) {
                console.log(`[OfflineStore] Deleting old store: ${V2.CLIENT_OUTBOX_STORE}`);
                db.deleteObjectStore(V2.CLIENT_OUTBOX_STORE);
            } else {
                console.log(`[OfflineStore] Old store ${V2.CLIENT_OUTBOX_STORE} not found`);
            }
            // Check if new store doesn't already exist before creating
            if (!db.objectStoreNames.contains(PENDING_POINTS_STORE)) {
                console.log(`[OfflineStore] Creating new store: ${PENDING_POINTS_STORE}`);
                createPendingPointsStore(db);
            } else {
                console.log(`[OfflineStore] Store ${PENDING_POINTS_STORE} already exists`);
            }
            break;
        default:
            console.log(`[OfflineStore] No upgrade needed from v${event.oldVersion}`);
    }
    console.log(`[OfflineStore] Final stores:`, Array.from(db.objectStoreNames));
}

// Create the pending points store
function createPendingPointsStore(db : IDBDatabase) : void {
    const store = db.createObjectStore(PENDING_POINTS_STORE, { keyPath: 'key'});
    store.createIndex('retryAfter', 'retryAfter');
}

type UnitOfWork<T> = (tx: IDBTransaction) => Promise<T>

async function inTransaction<T>(db : IDBDatabase, stores: string[], mode: IDBTransactionMode, fn: UnitOfWork<T>) : Promise<T> {
    const tx = db.transaction(stores, mode);
    const completion = new Promise<void>( (resolve, reject) => {
        tx.oncomplete = () => resolve()
        tx.onerror = () => reject(tx.error)
    });

    const result : T = await fn(tx);

    tx.commit();
    await completion;
    return result;
}

function read<T>(store: IDBObjectStore, key: IDBValidKey | IDBKeyRange) : Promise<T> {
    return new Promise((resolve, reject) => {
        const request = store.get(key);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(request.result);
    })
}

function getAll<V>(store: IDBObjectStore) : Promise<V[]> {
    return new Promise((resolve,reject) => {
       const request = store.getAll();
       request.onerror = () => reject(request.error);
       request.onsuccess = () => {
           resolve(request.result);
       };
    });
}

function put<T>(store: IDBObjectStore, value: T) : Promise<IDBValidKey> {
    return new Promise((resolve, reject) => {
        const request = store.put(value)
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(request.result);
    })

}

function deleteRecord(store: IDBObjectStore, key: IDBValidKey | IDBKeyRange) : Promise<void> {
    return new Promise((resolve, reject) => {
        const request = store.delete(key);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve();
    })
}

export class OfflineStore {
    private readonly db : IDBDatabase;
    public constructor(db : IDBDatabase ) {
        this.db = db;
    }

    /** Add points to the store for the given patrol in the given section. */
    async addPoints(sectionId: string, patrolId: string, scoreDelta: number) : Promise<number> {
        console.log(`[OfflineStore] Adding points: section=${sectionId}, patrol=${patrolId}, delta=${scoreDelta}`);
        return inTransaction<number>(this.db, [PENDING_POINTS_STORE], "readwrite", async (tx : IDBTransaction) : Promise<number> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            const key : string = getKey(sectionId,patrolId);
            const existingScore : OutboxEntry = await read<OutboxEntry>(store, key);
            if (existingScore) {
                console.log(`[OfflineStore] Found existing entry with ${existingScore.scoreDelta} points`);
                scoreDelta += existingScore.scoreDelta;
            }
            const entry = new OutboxEntry(sectionId, patrolId, scoreDelta);
            await put(store, entry);
            console.log(`[OfflineStore] Saved entry:`, entry);
            return scoreDelta;
        })
    }

    /** Mark the patrol and section as needing retry after a given date */
    async setRetryAfter(sectionId: string, patrolId: string, retryAfter: Date, errorMessage?: string) : Promise<void> {
        return inTransaction<void>(this.db, [PENDING_POINTS_STORE], "readwrite", async (tx : IDBTransaction) : Promise<void> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            const key : string = getKey(sectionId,patrolId);
            var existingScore : OutboxEntry = await read<OutboxEntry>(store, key);
            if (!existingScore) {
                existingScore = new OutboxEntry(sectionId,patrolId,0)
            }
            existingScore.retryAfter = retryAfter.getTime()
            existingScore.errorMessage = errorMessage; // Allow any old message to be cleared
            await put(store, existingScore);
        })
    }

    /** Mark the patrol as permanently failed with an error message. Client should acknowledge and clear. */
    async setError(sectionId: string, patrolId: string, errorMessage: string) : Promise<void> {
        return inTransaction<void>(this.db, [PENDING_POINTS_STORE], "readwrite", async (tx : IDBTransaction) : Promise<void> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            const key : string = getKey(sectionId,patrolId);
            var existingScore : OutboxEntry = await read<OutboxEntry>(store, key);
            if (!existingScore) {
                existingScore = new OutboxEntry(sectionId,patrolId,0)
            }
            existingScore.retryAfter = -1; // -1 indicates permanent error, don't retry
            existingScore.errorMessage = errorMessage;
            await put(store, existingScore);
        })
    }

    /** Return all pending entries */
    getAllPending(): Promise<OutboxEntry[]> {
        return inTransaction<OutboxEntry[]>(this.db, [PENDING_POINTS_STORE], "readonly", async (tx : IDBTransaction) : Promise<OutboxEntry[]> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            return getAll<OutboxEntry>(store);
        })
    }

    /** Return all pending entries that can be synced now according to their retryAfter */
    getPendingForSyncNow(): Promise<OutboxEntry[]> {
        return inTransaction<OutboxEntry[]>(this.db, [PENDING_POINTS_STORE], "readonly", async (tx : IDBTransaction) : Promise<OutboxEntry[]> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            const index : IDBIndex = store.index('retryAfter');
            const now = Date.now();
            // Get all entries where retryAfter <= now (ready to sync)
            const range = IDBKeyRange.upperBound(now);
            return new Promise((resolve, reject) => {
                const request = index.getAll(range);
                request.onerror = () => reject(request.error);
                request.onsuccess = () => resolve(request.result);
            });
        });
    }

    /** Get the soonest retryAfter time for scheduling next sync attempt. Returns null if no pending entries. */
    getSoonestRetryAfter(): Promise<number | null> {
        return inTransaction<number | null>(this.db, [PENDING_POINTS_STORE], "readonly", async (tx : IDBTransaction) : Promise<number | null> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            const index : IDBIndex = store.index('retryAfter');
            return new Promise((resolve, reject) => {
                // Open cursor on retryAfter index to get first (lowest) value
                // Skip entries with retryAfter = -1 (permanent errors)
                const request = index.openCursor(IDBKeyRange.lowerBound(0));
                request.onerror = () => reject(request.error);
                request.onsuccess = () => {
                    const cursor = request.result;
                    if (cursor) {
                        const entry = cursor.value as OutboxEntry;
                        resolve(entry.retryAfter);
                    } else {
                        resolve(null);
                    }
                };
            });
        });
    }

    /** Get all entries with permanent errors (retryAfter = -1) that the client should acknowledge */
    getFailedEntries(): Promise<OutboxEntry[]> {
        return inTransaction<OutboxEntry[]>(this.db, [PENDING_POINTS_STORE], "readonly", async (tx : IDBTransaction) : Promise<OutboxEntry[]> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            const index : IDBIndex = store.index('retryAfter');
            // Get all entries where retryAfter = -1 (permanent errors)
            const range = IDBKeyRange.only(-1);
            return new Promise((resolve, reject) => {
                const request = index.getAll(range);
                request.onerror = () => reject(request.error);
                request.onsuccess = () => resolve(request.result);
            });
        });
    }

    /** Clear the pending entry for the given section */
    clear(sectionId: string, patrolId: string): Promise<void> {
        return inTransaction<void>(this.db, [PENDING_POINTS_STORE], "readwrite", async (tx : IDBTransaction) : Promise<void> => {
            const store : IDBObjectStore = tx.objectStore(PENDING_POINTS_STORE);
            return deleteRecord(store, getKey(sectionId, patrolId));
        });
    }
}

function getKey(sectionId: string, patrolId: string) : string {
    return `${sectionId}:${patrolId}`;
}
