import {deleteRecord, getAllFromIndex, inTransaction, put, read} from "./promisDB.ts";

const DB_NAME = 'penguin-patrol-scores';
const DB_VERSION = 1;
const SECTIONS_STORE = 'sections';
const PATROL_SCORES_STORE = 'patrol_scores';
const INDEX_USERID_SECTIONID = 'userId_sectionId';
const INDEX_USERID_RETRY_AFTER = 'userId_retryAfter';
const INDEX_SECTIONS_USERID = 'userId';

/** Represents a section of patrols for a user */
export class Section {
    public readonly key: string;
    public readonly userId: string;
    public readonly id: string;
    public name: string;
    /** The timestamp of the last successful sync (milliseconds) */
    public lastRefresh: number = 0;

    /**
     * @param userId The ID of the user who owns this section
     * @param id The unique ID of the section
     * @param name The display name of the section
     */
    public constructor(userId: string, id: string, name: string) {
        this.key = getSectionKey(userId, id);
        this.userId = userId;
        this.id = id;
        this.name = name;
    }
}

/**
 * PatrolScore represents the score state for a patrol, combining:
 * - committedScore: The last known score from the server
 * - pendingScoreDelta: Local changes not yet synced to the server
 * - retryAfter: Sync retry timestamp in milliseconds (0 = sync now, positive = retry after timestamp, -1 = permanent error)
 */
export class Patrol {
    public readonly key: string;
    public readonly userId: string;
    public readonly sectionId: string;
    public readonly patrolName: string;
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

    /** errorMessage contains the error description for failed sync attempts (both temporary and permanent) */
    public errorMessage?: string;

    /**
     * @param userId The ID of the user who owns this patrol
     * @param sectionId The ID of the section this patrol belongs to
     * @param patrolId The unique ID of the patrol
     * @param name The display name of the patrol
     * @param committedScore The last known score from the server
     */
    public constructor(userId: string, sectionId: string, patrolId: string, name: string, committedScore: number = 0) {
        this.key = getPatrolKey(userId, sectionId, patrolId);
        this.userId = userId;
        this.sectionId = sectionId;
        this.patrolId = patrolId;
        this.patrolName = name;
        this.committedScore = committedScore;
    }
}

/** Result of a patrol sync from the server */
type SyncPatrolResult = { patrolId: string, patrolName: string, score: number };

/**
 * Open the patrol points store for the given user.
 * @param userId The ID of the user to open the store for
 * @returns A promise that resolves to the opened PatrolPointsStore
 */
export function OpenPatrolPointsStore(userId: string): Promise<PatrolPointsStore> {
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

    // Create the sections store
    const sectionsStore = db.createObjectStore(SECTIONS_STORE);
    sectionsStore.createIndex(INDEX_SECTIONS_USERID, ['userId']);

    // Create the patrol scores store without keyPath (we'll use explicit keys)
    const patrolsStore = db.createObjectStore(PATROL_SCORES_STORE);

    // Index for getting all scores for a user in a specific section
    patrolsStore.createIndex(INDEX_USERID_SECTIONID, ['userId', 'sectionId']);

    // Index for finding entries that need syncing (by userId and retryAfter time)
    patrolsStore.createIndex(INDEX_USERID_RETRY_AFTER, ['userId', 'retryAfter']);

    console.log(`[PatrolPointsStore] Created stores: sections, patrol_scores`);
}

/** Provides access to the local IndexedDB store for patrol scores and sections */
export class PatrolPointsStore {
    private readonly userId: string;
    private readonly db: IDBDatabase;

    /**
     * @param db The IDBDatabase instance
     * @param userId The ID of the current user
     */
    public constructor(db: IDBDatabase, userId: string) {
        this.db = db;
        this.userId = userId;
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
     */
    public setCanonicalSectionList(sections: Section[]): Promise<void> {
        return inTransaction<void>(this.db, [SECTIONS_STORE, PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const index = sectionsStore.index(INDEX_SECTIONS_USERID);
            const range = IDBKeyRange.only(this.userId);

            // Get existing sections for this user
            const existingSections = await getAllFromIndex<Section>(index, range);
            const canonicalSectionIds = new Set(sections.map(s => s.id));

            // Add or update sections from the canonical list
            for (const section of sections) {
                const sectionRecord = new Section(this.userId, section.id, section.name);
                await put(sectionsStore, sectionRecord, sectionRecord.key);
            }

            // Delete sections that are no longer in the canonical list
            for (const existingSection of existingSections) {
                if (!canonicalSectionIds.has(existingSection.id)) {
                    console.log(`[PatrolPointsStore] Deleting section ${existingSection.id} and its patrols`);
                    await deleteSectionAndPatrols(tx, this.userId, existingSection.id);
                }
            }

            console.log(`[PatrolPointsStore] Updated canonical section list: ${sections.length} sections`);
        });
    }

    /**
     * Updates the local list of patrols for a section to match the provided list.
     * Patrols not in the new list will be deleted.
     * @param sectionId The ID of the section to update
     * @param patrols The new list of patrols and their current scores
     */
    public setCanonicalPatrolList(sectionId: string, patrols: SyncPatrolResult[]): Promise<void> {
        return inTransaction<void>(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);

            // Get existing patrols for this section
            const existingPatrols = await getAllFromIndex<Patrol>(index, range);
            const canonicalPatrolIds = new Set(patrols.map(p => p.patrolId));

            // Add or update patrols from the canonical list
            for (const patrol of patrols) {
                await setScoreAndClearPendingState(store, this.userId, sectionId, patrol.patrolId, patrol.patrolName, patrol.score);
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
                await put(sectionsStore, section, sectionKey);
            }

            console.log(`[PatrolPointsStore] Updated canonical patrol list for section ${sectionId}: ${patrols.length} patrols`);
        });
    }

    /** Add points to the store for the given patrol in the given section. The patrol must already exist. */
    async addPoints(sectionId: string, patrolId: string, pointsDelta: number): Promise<number> {
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
                await put(store, existing, key);
            } else {
                throw new Error(`Patrol ${patrolId} does not exist in section ${sectionId}`);
            }

            console.log(`[PatrolPointsStore] New pending delta: ${newPendingDelta}`);
            return newPendingDelta;
        });
    }

    /** Update the committed score from the server. Clears pendingScoreDelta and error state. The patrol must already exist. */
    async setCommittedScore(sectionId: string, patrolId: string, score: number, patrolName?: string): Promise<void> {
        console.log(`[PatrolPointsStore] Setting committed score: section=${sectionId}, patrol=${patrolId}, score=${score}`);
        return inTransaction<void>(this.db, [PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const key: string = getPatrolKey(this.userId, sectionId, patrolId);
            const existing: Patrol = await read<Patrol>(store, key);

            if (!existing) {
                throw new Error(`Patrol ${patrolId} does not exist in section ${sectionId}. Use setCanonicalPatrolList to create patrols.`);
            }

            existing.committedScore = score;
            existing.pendingScoreDelta = 0;
            existing.retryAfter = 0;
            existing.errorMessage = undefined;

            // Update name if provided
            if (patrolName !== undefined) {
                (existing as any).patrolName = patrolName;
            }

            await put(store, existing, key);
        });
    }

    /** Get all scores for the current user and section */
    async getScoresForSection(sectionId: string): Promise<Patrol[]> {
        return inTransaction<Patrol[]>(this.db, [PATROL_SCORES_STORE], "readonly", async (tx: IDBTransaction): Promise<Patrol[]> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);
            return getAllFromIndex<Patrol>(index, range);
        });
    }

    /** Mark the patrol and section as needing retry after a given date. The patrol must already exist. */
    async setRetryAfter(sectionId: string, patrolId: string, retryAfter: Date, errorMessage?: string): Promise<void> {
        return inTransaction<void>(this.db, [PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const key: string = getPatrolKey(this.userId, sectionId, patrolId);
            let existing: Patrol = await read<Patrol>(store, key);

            if (!existing) {
                throw new Error(`Patrol ${patrolId} does not exist in section ${sectionId}`);
            }

            existing.retryAfter = retryAfter.getTime();
            existing.errorMessage = errorMessage; // Allow any old message to be cleared
            await put(store, existing, key);
        });
    }

    /** Mark the patrol as permanently failed with an error message. Client should acknowledge and clear. The patrol must already exist. */
    async setError(sectionId: string, patrolId: string, errorMessage: string): Promise<void> {
        return inTransaction<void>(this.db, [PATROL_SCORES_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const key: string = getPatrolKey(this.userId, sectionId, patrolId);
            let existing: Patrol = await read<Patrol>(store, key);

            if (!existing) {
                throw new Error(`Patrol ${patrolId} does not exist in section ${sectionId}`);
            }

            existing.retryAfter = -1; // -1 indicates permanent error, don't retry
            existing.errorMessage = errorMessage;
            await put(store, existing, key);
        });
    }

    /** Return all pending entries that can be synced now (pendingScoreDelta != 0 and retryAfter <= now) */
    getPendingForSyncNow(): Promise<Patrol[]> {
        return inTransaction<Patrol[]>(this.db, [PATROL_SCORES_STORE], "readonly", async (tx: IDBTransaction): Promise<Patrol[]> => {
            const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
            const index: IDBIndex = store.index(INDEX_USERID_RETRY_AFTER);
            const now = Date.now();

            // Get all entries for this user where retryAfter <= now (in milliseconds)
            const range = IDBKeyRange.bound([this.userId, 0], [this.userId, now]);

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
}

/**
 * Generates a unique key for a section record.
 * @param userId The ID of the user
 * @param sectionId The ID of the section
 * @returns A string key
 */
function getSectionKey(userId: string, sectionId: string): string {
    return `${userId}:${sectionId}`;
}

/** Remove a section and its patrols from the store. For example if a section has been removed from the server. */
async function deleteSectionAndPatrols(tx: IDBTransaction, userId: string, sectionId: string): Promise<void> {
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
async function deletePatrol(tx: IDBTransaction, userId: string, sectionId: string, patrolId: string): Promise<void> {
    const store: IDBObjectStore = tx.objectStore(PATROL_SCORES_STORE);
    const key = getPatrolKey(userId, sectionId, patrolId);
    return deleteRecord(store, key);
}

/**
 * Updates a patrol's committed score and clears any pending local changes or error states.
 * @param patrolStore The IDBObjectStore for patrols
 * @param userId The ID of the user
 * @param sectionId The ID of the section
 * @param patrolId The ID of the patrol
 * @param patrolName The name of the patrol
 * @param score The new committed score
 */
async function setScoreAndClearPendingState(patrolStore: IDBObjectStore, userId: string, sectionId: string, patrolId: string, patrolName: string, score: number) {
    const key: string = getPatrolKey(userId, sectionId, patrolId);
    const existing: Patrol = await read<Patrol>(patrolStore, key);

    if (existing) {
        // Update existing patrol: set score, update name (in case it changed), clear pending state
        existing.committedScore = score;
        existing.pendingScoreDelta = 0;
        existing.retryAfter = 0;
        existing.errorMessage = undefined;
        // Update the name in case it changed on the server
        (existing as any).patrolName = patrolName;
        await put(patrolStore, existing, key);
    } else {
        // Create new entry with the committed score
        const entry = new Patrol(userId, sectionId, patrolId, patrolName, score);
        await put(patrolStore, entry, key);
    }
}

/**
 * Generates a unique key for a patrol record.
 * @param userId The ID of the user
 * @param sectionId The ID of the section
 * @param patrolId The ID of the patrol
 * @returns A string key
 */
function getPatrolKey(userId: string, sectionId: string, patrolId: string): string {
    return `${userId}:${sectionId}:${patrolId}`;
}
