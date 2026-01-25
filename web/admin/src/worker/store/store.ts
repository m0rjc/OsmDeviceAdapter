import {deleteRecord, getAllFromIndex, inTransaction, put, read} from "./promisDB";
import * as model from "../../types/model"

const DB_NAME = 'penguin-patrol-scores';
const DB_VERSION = 1;
const SECTIONS_STORE = 'sections';
const PATROL_SCORES_STORE = 'patrol_scores';
const INDEX_USERID_SECTIONID = 'userId_sectionId';
const INDEX_USERID_RETRY_AFTER = 'userId_retryAfter';
const INDEX_SECTIONS_USERID = 'userId';

/** How long before we allow retry if we get an unexpected server error (milliseconds) */
const SERVER_ERROR_RETRY_TIME = 1000 * 60 * 5;

/** Represents a section of patrols for a user */
export class Section {
    public readonly key: string;
    public readonly userId: number;
    public readonly id: number;
    public name: string;
    public groupName: string;
    /** The timestamp of the last successful sync (milliseconds) */
    public lastRefresh: number = 0;

    /**
     * @param userId The ID of the user who owns this section
     * @param id The unique ID of the section
     * @param name The display name of the section
     * @param groupName The display name of the group this section belongs to
     */
    public constructor(userId: number, id: number, name: string, groupName: string) {
        this.key = getSectionKey(userId, id);
        this.userId = userId;
        this.id = id;
        this.name = name;
        this.groupName = groupName;
    }
}

/**
 * Patrol represents the score state for a patrol, combining:
 * - committedScore: The last known score from the server
 * - pendingScoreDelta: Local changes not yet synced to the server
 * - retryAfter: Sync retry timestamp in milliseconds (0 = sync now, positive = retry after timestamp, -1 = permanent error)
 * - lockTimeout: Timestamp when a sync lock expires (0 = not locked)
 * - lockId: Unique identifier for the sync lock holder
 */
export class Patrol {
    public readonly key: string;
    public readonly userId: number;
    public readonly sectionId: number;
    public patrolName: string;
    public readonly patrolId: string;

    /** The last known score from the server */
    public committedScore: number = 0;

    /** Local changes not yet synced to the server */
    public pendingScoreDelta: number = 0;

    /**
     * retryAfter is the timestamp (milliseconds) to retry after:
     * - 0 = can be immediately retried
     * - positive value = retry after this timestamp
     * - -1 = permanent error, don't retry (client should acknowledge and clear)
     */
    public retryAfter: number = 0;

    /**
     * lockTimeout is the timestamp (milliseconds) when the sync lock expires:
     * - 0 = not locked
     * - positive value = locked until this timestamp
     */
    public lockTimeout: number = 0;

    /**
     * lockId uniquely identifies who holds the sync lock.
     * Only the holder with this ID can release the lock.
     */
    public lockId?: string;

    /** errorMessage contains the error description for failed sync attempts (both temporary and permanent) */
    public errorMessage?: string;

    /**
     * @param userId The ID of the user who owns this patrol
     * @param sectionId The ID of the section this patrol belongs to
     * @param patrolId The unique ID of the patrol (string to support OSM special patrols like empty/"" or negative IDs)
     * @param name The display name of the patrol
     * @param committedScore The last known score from the server
     */
    public constructor(userId: number, sectionId: number, patrolId: string, name: string, committedScore: number = 0) {
        this.key = getPatrolKey(userId, sectionId, patrolId);
        this.userId = userId;
        this.sectionId = sectionId;
        this.patrolId = patrolId;
        this.patrolName = name;
        this.committedScore = committedScore;
    }
}

/** Result of a patrol sync from the server */
type SyncPatrolResult = { id: string, name: string, score: number };

/**
 * Open the patrol points store for the given user.
 * @param userId The ID of the user to open the store for
 * @returns A promise that resolves to the opened PatrolPointsStore
 */
export function OpenPatrolPointsStore(userId: number): Promise<PatrolPointsStore> {
    return new Promise<PatrolPointsStore>((resolve, reject) => {
        const request = indexedDB.open(DB_NAME, DB_VERSION);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(new PatrolPointsStore(request.result, userId));
        request.onupgradeneeded = handleDatabaseInstall;
    });
}

/** Handles the initial database setup and upgrades */
function handleDatabaseInstall(event: IDBVersionChangeEvent) {
    const request = event.target as IDBOpenDBRequest;
    const db = request.result;
    console.log(`[PatrolPointsStore] Installing database v${DB_VERSION}`);

    // Create the sections store with keyPath
    const sectionsStore = db.createObjectStore(SECTIONS_STORE, { keyPath: 'key' });
    sectionsStore.createIndex(INDEX_SECTIONS_USERID, 'userId');

    // Create the patrol scores store with keyPath
    const patrolsStore = db.createObjectStore(PATROL_SCORES_STORE, { keyPath: 'key' });

    // Index for getting all scores for a user in a specific section
    patrolsStore.createIndex(INDEX_USERID_SECTIONID, ['userId', 'sectionId']);

    // Index for finding entries that need syncing (by userId and retryAfter time)
    patrolsStore.createIndex(INDEX_USERID_RETRY_AFTER, ['userId', 'retryAfter']);

    console.log(`[PatrolPointsStore] Created stores: sections, patrol_scores`);
}

/** Unit of work for batching multiple store operations into a single transaction */
export class UnitOfWork {
    private operations: Array<(tx: IDBTransaction) => Promise<void>> = [];
    private readonly db: IDBDatabase;
    private readonly userId: number;

    constructor(
        db: IDBDatabase,
        userId: number
    ) {
        this.userId = userId;
        this.db = db;
    }

    /** Update the committed score from the server. Clears pendingScoreDelta, error state, and lock. */
    setCommittedScore(sectionId: number, patrolId: string, score: number, patrolName: string): this {
        this.operations.push(async (tx) => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const newState = new Patrol(this.userId, sectionId, patrolId, patrolName, score);
            // Ensure lock fields are cleared (constructor sets them to default values)
            newState.lockTimeout = 0;
            newState.lockId = undefined;
            newState.retryAfter = 0;
            newState.errorMessage = undefined;
            await put(store, newState);
        });
        return this;
    }

    /** Mark the patrol and section as needing retry after a given date. */
    setRetryAfter(sectionId: number, patrolId: string, retryAfter: Date, errorMessage?: string): this {
        this.operations.push(async (tx) => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const key = getPatrolKey(this.userId, sectionId, patrolId);
            const existing = await read<Patrol>(store, key);

            if (!existing) {
                throw new Error(`Patrol ${patrolId} does not exist in section ${sectionId}`);
            }

            existing.retryAfter = retryAfter.getTime();
            existing.errorMessage = errorMessage; // Allow any old message to be cleared
            await put(store, existing);
        });
        return this;
    }

    /** Mark the patrol as permanently failed with an error message. Client should acknowledge and clear. */
    setError(sectionId: number, patrolId: string, errorMessage: string): this {
        this.operations.push(async (tx) => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const key = getPatrolKey(this.userId, sectionId, patrolId);
            const existing = await read<Patrol>(store, key);

            if (!existing) {
                throw new Error(`Patrol ${patrolId} does not exist in section ${sectionId}`);
            }

            existing.retryAfter = -1; // -1 indicates permanent error, don't retry
            existing.errorMessage = errorMessage;
            await put(store, existing);
        });
        return this;
    }

    /** Commit all queued operations in a single transaction */
    async commit(): Promise<void> {
        if (this.operations.length === 0) {
            return; // Nothing to do
        }

        return inTransaction(this.db, [PATROL_SCORES_STORE], "readwrite", async (tx) => {
            for (const operation of this.operations) {
                await operation(tx);
            }
        });
    }
}

/** Provides access to the local IndexedDB store for patrol scores and sections */
export class PatrolPointsStore {
    private readonly userId: number;
    private readonly db: IDBDatabase;

    /**
     * @param db The IDBDatabase instance
     * @param userId The ID of the current user
     */
    public constructor(db: IDBDatabase, userId: number) {
        this.db = db;
        this.userId = userId;
    }

    /** Close the database connection */
    public close(): void {
        this.db.close();
    }

    /** Create a new unit of work for batching multiple operations into a single transaction */
    newUnitOfWork(): UnitOfWork {
        return new UnitOfWork(this.db, this.userId);
    }

    /** Get all sections for the current user */
    async getSections(): Promise<Section[]> {
        return inTransaction<Section[]>(this.db, [SECTIONS_STORE], "readonly", async (tx: IDBTransaction): Promise<Section[]> => {
            const store = tx.objectStore(SECTIONS_STORE);
            const index = store.index(INDEX_SECTIONS_USERID);
            const range = IDBKeyRange.only(this.userId);
            return getAllFromIndex<Section>(index, range);
        });
    }

    /** Deletes all section and patrol data for the current user from the local store */
    public deleteUserData(): Promise<void> {
        return inTransaction<void>(this.db, [SECTIONS_STORE, PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            // Delete all sections for this user
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionsIndex = sectionsStore.index(INDEX_SECTIONS_USERID);
            const sectionsRange = IDBKeyRange.only(this.userId);

            await new Promise<void>((resolve, reject) => {
                const request = sectionsIndex.openCursor(sectionsRange);
                request.onerror = () => reject(request.error);
                request.onsuccess = () => {
                    const cursor = request.result;
                    if (cursor) {
                        cursor.delete();
                        cursor.continue();
                    } else {
                        resolve();
                    }
                };
            });

            // Delete all patrols for this user
            const patrolsStore = tx.objectStore(PATROL_SCORES_STORE);
            const patrolsIndex = patrolsStore.index(INDEX_USERID_SECTIONID);
            // Use a range that matches all entries starting with this userId
            const patrolsRange = IDBKeyRange.bound([this.userId], [this.userId, []]);

            await new Promise<void>((resolve, reject) => {
                const request = patrolsIndex.openCursor(patrolsRange);
                request.onerror = () => reject(request.error);
                request.onsuccess = () => {
                    const cursor = request.result;
                    if (cursor) {
                        cursor.delete();
                        cursor.continue();
                    } else {
                        resolve();
                    }
                };
            });

            console.log(`[PatrolPointsStore] Deleted all data for user ${this.userId}`);
        });
    }

    /**
     * Updates the local list of sections to match the provided list.
     * Sections not in the new list will be deleted along with their patrols.
     * @param sections The new list of sections
     * @returns A promise that resolves to true if any sections were added, deleted, or changed.
     */
    public setCanonicalSectionList(sections: model.Section[]): Promise<boolean> {
        return inTransaction<boolean>(this.db, [SECTIONS_STORE, PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<boolean> => {
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const index = sectionsStore.index(INDEX_SECTIONS_USERID);
            const range = IDBKeyRange.only(this.userId);

            // Get existing sections for this user
            const existingSections = await getAllFromIndex<Section>(index, range);
            const existingSectionsMap = new Map(existingSections.map(s => [s.id, s]));
            const canonicalSectionIds = new Set(sections.map(s => s.id));

            let changed = false;

            // Add or update sections from the canonical list
            for (const section of sections) {
                const existing = existingSectionsMap.get(section.id);
                if (!existing || existing.name !== section.name) {
                    changed = true;
                    const sectionRecord = new Section(this.userId, section.id, section.name, section.groupName);
                    await put(sectionsStore, sectionRecord);
                }
            }

            // Delete sections that are no longer in the canonical list
            for (const existingSection of existingSections) {
                if (!canonicalSectionIds.has(existingSection.id)) {
                    changed = true;
                    console.log(`[PatrolPointsStore] Deleting section ${existingSection.id} and its patrols`);
                    await deleteSectionAndPatrols(tx, this.userId, existingSection.id);
                }
            }

            if (changed) {
                console.log(`[PatrolPointsStore] Updated canonical section list: ${sections.length} sections (changes detected)`);
            } else {
                console.log(`[PatrolPointsStore] Updated canonical section list: ${sections.length} sections (no changes)`);
            }
            return changed;
        });
    }

    /**
     * Updates the local list of patrols for a section to match the provided list.
     * Patrols not in the new list will be deleted.
     * Preserves pending scores for existing patrols.
     * @param sectionId The ID of the section to update
     * @param patrols The new list of patrols and their current scores
     * @returns The updated patrol list (same as calling getScoresForSection immediately after)
     */
    public setCanonicalPatrolList(sectionId: number, patrols: SyncPatrolResult[]): Promise<Patrol[]> {
        return inTransaction<Patrol[]>(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<Patrol[]> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);

            // Get existing patrols for this section
            const existingPatrols = await getAllFromIndex<Patrol>(index, range);
            const canonicalPatrolIds = new Set(patrols.map(p => p.id));

            // Add or update patrols from the canonical list
            for (const patrol of patrols) {
                await setScoreAndPreservePendingState(store, this.userId, sectionId, patrol.id, patrol.name, patrol.score);
            }

            // Delete patrols that are no longer in the canonical list
            for (const existingPatrol of existingPatrols) {
                if (!canonicalPatrolIds.has(existingPatrol.patrolId)) {
                    console.log(`[PatrolPointsStore] Deleting patrol ${existingPatrol.patrolId} from section ${sectionId}`);
                    await deletePatrol(tx, this.userId, sectionId, existingPatrol.patrolId);
                }
            }

            // Update the section's last update timestamp
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionKey = getSectionKey(this.userId, sectionId);
            const section = await read<Section>(sectionsStore, sectionKey);
            if (section) {
                section.lastRefresh = Date.now();
                await put(sectionsStore, section);
            }

            console.log(`[PatrolPointsStore] Updated canonical patrol list for section ${sectionId}: ${patrols.length} patrols`);

            // Return the updated patrol list
            return getAllFromIndex<Patrol>(index, range);
        });
    }

    /** Add points to the store for the given patrol in the given section. The patrol must already exist. */
    async addPendingPoints(sectionId: number, patrolId: string, pointsDelta: number): Promise<number> {
        console.log(`[PatrolPointsStore] Adding points: section=${sectionId}, patrol=${patrolId}, delta=${pointsDelta}`);
        return inTransaction<number>(this.db, [PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<number> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const key: string = getPatrolKey(this.userId, sectionId, patrolId);
            const existing: Patrol = await read<Patrol>(store, key);

            let newPendingDelta: number;
            if (existing) {
                console.log(`[PatrolPointsStore] Found existing entry with pending delta ${existing.pendingScoreDelta}`);
                newPendingDelta = existing.pendingScoreDelta + pointsDelta;
                existing.pendingScoreDelta = newPendingDelta;
                await put(store, existing);
            } else {
                throw new Error(`Patrol ${patrolId} does not exist in section ${sectionId}`);
            }

            console.log(`[PatrolPointsStore] New pending delta: ${newPendingDelta}`);
            return newPendingDelta;
        });
    }

    /** Get all scores for the current user and section */
    async getScoresForSection(sectionId: number): Promise<Patrol[]> {
        return inTransaction<Patrol[]>(this.db, [PATROL_SCORES_STORE], "readonly", async (tx: IDBTransaction): Promise<Patrol[]> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);
            return getAllFromIndex<Patrol>(index, range);
        });
    }

    /**
     * Return all pending entries for a section that can be synced now
     * (pendingScoreDelta != 0, retryAfter <= now, and not locked)
     */
    getPendingForSyncNow(sectionId: number): Promise<Patrol[]> {
        return inTransaction<Patrol[]>(this.db, [PATROL_SCORES_STORE], "readonly", async (tx: IDBTransaction): Promise<Patrol[]> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const index: IDBIndex = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);
            const now = Date.now();

            return new Promise((resolve, reject) => {
                const results: Patrol[] = [];
                const request = index.openCursor(range);

                request.onerror = () => reject(request.error);
                request.onsuccess = () => {
                    const cursor = request.result;
                    if (cursor) {
                        const patrol = cursor.value as Patrol;
                        // Only include entries with pending changes, not locked, and ready to retry
                        if (patrol.pendingScoreDelta !== 0 &&
                            patrol.lockTimeout <= now &&
                            patrol.retryAfter <= now &&
                            patrol.retryAfter >= 0) { // Exclude permanent errors (-1)
                            results.push(patrol);
                        }
                        cursor.continue();
                    } else {
                        resolve(results);
                    }
                };
            });
        });
    }

    /** Get the soonest retryAfter time (milliseconds) for scheduling next sync attempt. Returns null if no pending entries. */
    getSoonestRetryAfter(): Promise<number | null> {
        return inTransaction<number | null>(this.db, [PATROL_SCORES_STORE], "readonly", async (tx: IDBTransaction): Promise<number | null> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const index: IDBIndex = store.index(INDEX_USERID_RETRY_AFTER);

            return new Promise((resolve, reject) => {
                // Open cursor on userId_retryAfter index for this user, starting from retryAfter = 0
                // Skip entries with retryAfter = -1 (permanent errors)
                const range = IDBKeyRange.bound([this.userId, 0], [this.userId, Infinity]);
                const request = index.openCursor(range);

                request.onerror = () => reject(request.error);
                request.onsuccess = () => {
                    const cursor = request.result;
                    if (cursor) {
                        const entry = cursor.value as Patrol;
                        // Only consider entries with pending changes
                        if (entry.pendingScoreDelta !== 0) {
                            resolve(entry.retryAfter);
                        } else {
                            cursor.continue();
                        }
                    } else {
                        resolve(null);
                    }
                };
            });
        });
    }

    /** Get all entries with permanent errors (retryAfter = -1) that the client should acknowledge */
    getFailedEntries(): Promise<Patrol[]> {
        return inTransaction<Patrol[]>(this.db, [PATROL_SCORES_STORE], "readonly", async (tx: IDBTransaction): Promise<Patrol[]> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const index: IDBIndex = store.index(INDEX_USERID_RETRY_AFTER);

            // Get all entries for this user where retryAfter = -1 (permanent errors)
            const range = IDBKeyRange.only([this.userId, -1]);

            return new Promise((resolve, reject) => {
                const results: Patrol[] = [];
                const request = index.openCursor(range);

                request.onerror = () => reject(request.error);
                request.onsuccess = () => {
                    const cursor = request.result;
                    if (cursor) {
                        const score = cursor.value as Patrol;
                        // Only include entries with pending changes
                        if (score.pendingScoreDelta !== 0) {
                            results.push(score);
                        }
                        cursor.continue();
                    } else {
                        resolve(results);
                    }
                };
            });
        });
    }

    /**
     * Atomically acquire a sync lock and get all pending entries for a section.
     * This is a combined operation that locks and retrieves in a single transaction.
     * @param sectionId The section to sync
     * @param lockDurationMs How long to hold the lock (default 30 seconds)
     * @returns Object containing the lock ID and list of pending patrols
     */
    async acquirePendingForSync(sectionId: number, lockDurationMs: number = 30000): Promise<{ lockId: string, pending: Patrol[] }> {
        const lockId = crypto.randomUUID();
        const lockExpiry = Date.now() + lockDurationMs;

        return inTransaction<{ lockId: string, pending: Patrol[] }>(
            this.db,
            [PATROL_SCORES_STORE],
            "readwrite",
            async (tx: IDBTransaction): Promise<{ lockId: string, pending: Patrol[] }> => {
                const store = tx.objectStore(PATROL_SCORES_STORE);
                const index = store.index(INDEX_USERID_SECTIONID);
                const range = IDBKeyRange.only([this.userId, sectionId]);
                const now = Date.now();

                const pending: Patrol[] = [];

                await new Promise<void>((resolve, reject) => {
                    const request = index.openCursor(range);
                    request.onerror = () => reject(request.error);
                    request.onsuccess = async () => {
                        const cursor = request.result;
                        if (cursor) {
                            const patrol = cursor.value as Patrol;
                            // Check if this patrol should be synced now:
                            // - Has pending changes
                            // - Not locked (or lock expired)
                            // - Retry window open (not in future, not permanent error)
                            if (patrol.pendingScoreDelta !== 0 &&
                                patrol.lockTimeout <= now &&
                                patrol.retryAfter <= now &&
                                patrol.retryAfter >= 0) { // Exclude permanent errors (-1)

                                // Lock it
                                patrol.lockTimeout = lockExpiry;
                                patrol.lockId = lockId;
                                await put(store, patrol);

                                // Add to results
                                pending.push(patrol);
                            }
                            cursor.continue();
                        } else {
                            resolve();
                        }
                    };
                });

                console.log(`[PatrolPointsStore] Acquired sync lock ${lockId} for section ${sectionId}: ${pending.length} patrols locked until ${new Date(lockExpiry).toISOString()}`);
                return { lockId, pending };
            }
        );
    }

    /**
     * Release the sync lock for a section, clearing lockTimeout and lockId for entries with the matching lockId.
     * @param sectionId The section to unlock
     * @param lockId The lock ID that was returned from acquireSyncLock
     */
    async releaseSyncLock(sectionId: number, lockId: string): Promise<void> {
        return inTransaction<void>(this.db, [PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);

            await new Promise<void>((resolve, reject) => {
                const request = index.openCursor(range);
                request.onerror = () => reject(request.error);
                request.onsuccess = async () => {
                    const cursor = request.result;
                    if (cursor) {
                        const patrol = cursor.value as Patrol;
                        // Only unlock entries that have this specific lock ID
                        if (patrol.lockId === lockId) {
                            patrol.lockTimeout = 0;
                            patrol.lockId = undefined;
                            await put(store, patrol);
                        }
                        cursor.continue();
                    } else {
                        resolve();
                    }
                };
            });

            console.log(`[PatrolPointsStore] Released sync lock ${lockId} for section ${sectionId}`);
        });
    }

    /**
     * Mark all pending entries in a section as failed. Allow some retry later to prevent them
     * becoming stuck.
     * Used when a catastrophic error occurs during sync (e.g., network failure, server error).
     * @param sectionId The section containing the failed entries
     * @param errorMessage The error message to set
     */
    async markAllPendingAsFailed(sectionId: number, errorMessage: string): Promise<void> {
        return inTransaction<void>(this.db, [PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);

            await new Promise<void>((resolve, reject) => {
                const request = index.openCursor(range);
                request.onerror = () => reject(request.error);
                request.onsuccess = async () => {
                    const cursor = request.result;
                    if (cursor) {
                        const patrol = cursor.value as Patrol;
                        // Only mark entries with pending changes
                        if (patrol.pendingScoreDelta !== 0) {
                            patrol.retryAfter = Date.now() + SERVER_ERROR_RETRY_TIME;
                            patrol.errorMessage = errorMessage;
                            await put(store, patrol);
                        }
                        cursor.continue();
                    } else {
                        resolve();
                    }
                };
            });

            console.log(`[PatrolPointsStore] Marked all pending entries in section ${sectionId} as failed: ${errorMessage}`);
        });
    }
}

/**
 * Generates a unique key for a section record.
 * @param userId The ID of the user
 * @param sectionId The ID of the section
 * @returns A string key
 */
function getSectionKey(userId: number, sectionId: number): string {
    return `${userId}:${sectionId}`;
}

/** Remove a section and its patrols from the store. For example if a section has been removed from the server. */
async function deleteSectionAndPatrols(tx: IDBTransaction, userId: number, sectionId: number): Promise<void> {
    // Delete the section record
    const sectionsStore = tx.objectStore(SECTIONS_STORE);
    const sectionKey = getSectionKey(userId, sectionId);
    await deleteRecord(sectionsStore, sectionKey);

    // Delete all patrols in this section
    const patrolsStore = tx.objectStore(PATROL_SCORES_STORE);
    const index = patrolsStore.index(INDEX_USERID_SECTIONID);
    const range = IDBKeyRange.only([userId, sectionId]);

    await new Promise<void>((resolve, reject) => {
        const request = index.openCursor(range);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => {
            const cursor = request.result;
            if (cursor) {
                cursor.delete();
                cursor.continue();
            } else {
                resolve();
            }
        };
    });
}

/** Clear the score entry for the given patrol (removes it from the store completely). For example if a patrol has been removed */
async function deletePatrol(tx: IDBTransaction, userId: number, sectionId: number, patrolId: string): Promise<void> {
    const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
    const key = getPatrolKey(userId, sectionId, patrolId);
    return deleteRecord(store, key);
}

/**
 * Updates a patrol's committed score, preserving any pending local changes or error states.
 * @param patrolStore The IDBObjectStore for patrols
 * @param userId The ID of the user
 * @param sectionId The ID of the section
 * @param patrolId The ID of the patrol (string to support OSM special patrols)
 * @param patrolName The name of the patrol
 * @param score The new committed score
 */
async function setScoreAndPreservePendingState(patrolStore: IDBObjectStore, userId: number, sectionId: number, patrolId: string, patrolName: string, score: number) {
    const key: string = getPatrolKey(userId, sectionId, patrolId);
    const existing: Patrol = await read<Patrol>(patrolStore, key);

    if (existing) {
        // Update existing patrol: set score
        existing.committedScore = score;
        // Update the name in case it changed on the server
        existing.patrolName = patrolName;
        await put(patrolStore, existing);
    } else {
        // Create new entry with the committed score
        const entry = new Patrol(userId, sectionId, patrolId, patrolName, score);
        await put(patrolStore, entry);
    }
}

/**
 * Generates a unique key for a patrol record.
 * @param userId The ID of the user
 * @param sectionId The ID of the section
 * @param patrolId The ID of the patrol (string to support OSM special patrols like empty or negative IDs)
 * @returns A string key
 */
function getPatrolKey(userId: number, sectionId: number, patrolId: string): string {
    return `${userId}:${sectionId}:${patrolId}`;
}
