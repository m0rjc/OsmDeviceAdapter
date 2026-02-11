import {deleteRecord, getAllFromIndex, inTransaction, put, read} from "./promisDB";
import * as model from "../../types/model"

const DB_NAME = 'penguin-patrol-scores';
const DB_VERSION = 2;
const SECTIONS_STORE = 'sections';
const PATROL_SCORES_STORE = 'patrol_scores';
const USER_METADATA_STORE = 'user_metadata';
const INDEX_USERID_SECTIONID = 'userId_sectionId';
const INDEX_USERID_RETRY_AFTER = 'userId_retryAfter';
const INDEX_SECTIONS_USERID = 'userId';

/** How long before we allow retry if we get an unexpected server error (milliseconds) */
const SERVER_ERROR_RETRY_TIME = 1000 * 60 * 5;

/** Stores user-level metadata including revision tracking */
export class UserMetadata {
    public readonly userId: number;
    /** User's display name */
    public userName?: string;
    /** CSRF token for authenticated requests */
    public csrfToken?: string;
    /** Global revision number for section list changes */
    public sectionsListRevision: number = 0;
    /** Last error message from profile/section list fetch (undefined if no error) */
    public lastError?: string;
    /** Timestamp of last error (milliseconds, undefined if no error) */
    public lastErrorTime?: number;

    constructor(userId: number, userName?: string, csrfToken?: string) {
        this.userId = userId;
        this.userName = userName;
        this.csrfToken = csrfToken;
    }
}

/** Represents a section of patrols for a user */
export class Section {
    public readonly key: string;
    public readonly userId: number;
    public readonly id: number;
    public name: string;
    public groupName: string;
    /** The timestamp of the last successful sync (milliseconds) */
    public lastRefresh: number = 0;
    /** UI revision number incremented on each state change to detect stale messages */
    public uiRevision: number = 0;
    /** Last error message from section refresh (undefined if no error) */
    public lastError?: string;
    /** Timestamp of last error (milliseconds, undefined if no error) */
    public lastErrorTime?: number;

    /** Sync lock timeout (0 = unlocked, positive = locked until timestamp in ms) */
    public syncLockTimeout: number = 0;
    /** Sync lock ID (UUID of lock holder, undefined if unlocked) */
    public syncLockId?: string;
    /** Timestamp of last sync attempt (milliseconds, for client-side rate limiting) */
    public lastSyncAttempt: number = 0;
    /** Calculated next retry time (milliseconds, for auto-retry scheduling) */
    public nextRetryTime: number = 0;

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
    const oldVersion = event.oldVersion;
    console.log(`[PatrolPointsStore] Upgrading database from v${oldVersion} to v${DB_VERSION}`);

    // Version 1: Initial schema
    if (oldVersion < 1) {
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

    // Version 2: Add user metadata store for section list revision tracking
    if (oldVersion < 2) {
        db.createObjectStore(USER_METADATA_STORE, { keyPath: 'userId' });
        console.log(`[PatrolPointsStore] Created store: user_metadata`);
    }
}

/** Unit of work for batching multiple store operations into a single transaction */
export class UnitOfWork {
    private operations: Array<(tx: IDBTransaction) => Promise<void>> = [];
    private readonly db: IDBDatabase;
    private readonly userId: number;
    private modifiedSections: Set<number> = new Set();

    constructor(
        db: IDBDatabase,
        userId: number
    ) {
        this.userId = userId;
        this.db = db;
    }

    /** Update the committed score from the server. Clears pendingScoreDelta, error state, and lock. */
    setCommittedScore(sectionId: number, patrolId: string, score: number, patrolName: string): this {
        this.modifiedSections.add(sectionId);
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
        this.modifiedSections.add(sectionId);
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
        this.modifiedSections.add(sectionId);
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

        return inTransaction(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readwrite", async (tx) => {
            for (const operation of this.operations) {
                await operation(tx);
            }

            // Bump UI revision for all modified sections
            for (const sectionId of this.modifiedSections) {
                await bumpSectionUiRevision(tx, this.userId, sectionId);
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

    /**
     * Get user metadata including CSRF token and userName.
     * Returns a default UserMetadata if not found (first time user).
     */
    async getUserMetadata(): Promise<UserMetadata> {
        return inTransaction<UserMetadata>(this.db, [USER_METADATA_STORE], "readonly", async (tx: IDBTransaction): Promise<UserMetadata> => {
            const metadataStore = tx.objectStore(USER_METADATA_STORE);
            let metadata = await read<UserMetadata>(metadataStore, this.userId);

            if (!metadata) {
                // Return a default instance for first-time users
                metadata = new UserMetadata(this.userId);
            }

            return metadata;
        });
    }

    /**
     * Set user metadata (userName, csrfToken).
     * This should be called after successful authentication to persist session info.
     */
    async setUserMetadata(userName: string, csrfToken: string): Promise<void> {
        return inTransaction<void>(this.db, [USER_METADATA_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const metadataStore = tx.objectStore(USER_METADATA_STORE);
            let metadata = await read<UserMetadata>(metadataStore, this.userId);

            if (!metadata) {
                metadata = new UserMetadata(this.userId, userName, csrfToken);
            } else {
                metadata.userName = userName;
                metadata.csrfToken = csrfToken;
            }

            await put(metadataStore, metadata);
            console.log(`[PatrolPointsStore] Updated user metadata for user ${this.userId}`);
        });
    }

    /**
     * Clear user metadata (called on logout or session expiry).
     * Preserves sectionsListRevision and error state but clears auth tokens.
     */
    async clearUserAuth(): Promise<void> {
        return inTransaction<void>(this.db, [USER_METADATA_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const metadataStore = tx.objectStore(USER_METADATA_STORE);
            let metadata = await read<UserMetadata>(metadataStore, this.userId);

            if (metadata) {
                metadata.userName = undefined;
                metadata.csrfToken = undefined;
                await put(metadataStore, metadata);
                console.log(`[PatrolPointsStore] Cleared user auth for user ${this.userId}`);
            }
        });
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

    /**
     * Set error state for a section and bump its UI revision.
     * Used when section refresh fails but we want to preserve cached data.
     * @param sectionId The section ID
     * @param errorMessage The error message
     * @returns The new UI revision number
     */
    async setSectionError(sectionId: number, errorMessage: string): Promise<number> {
        return inTransaction<number>(this.db, [SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<number> => {
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionKey = getSectionKey(this.userId, sectionId);
            const section = await read<Section>(sectionsStore, sectionKey);

            if (!section) {
                throw new Error(`Section ${sectionId} not found for user ${this.userId}`);
            }

            section.lastError = errorMessage;
            section.lastErrorTime = Date.now();
            section.uiRevision++;
            await put(sectionsStore, section);

            return section.uiRevision;
        });
    }

    /**
     * Set error state for user profile/section list and bump sections list revision.
     * Used when profile fetch or section list fetch fails.
     * @param errorMessage The error message
     * @returns Object with the new sectionsListRevision and error info
     */
    async setProfileError(errorMessage: string): Promise<{
        sectionsListRevision: number,
        lastError: string,
        lastErrorTime: number
    }> {
        return inTransaction(this.db, [USER_METADATA_STORE], "readwrite", async (tx: IDBTransaction) => {
            const metadataStore = tx.objectStore(USER_METADATA_STORE);
            let metadata = await read<UserMetadata>(metadataStore, this.userId);

            if (!metadata) {
                metadata = new UserMetadata(this.userId);
            }

            metadata.lastError = errorMessage;
            metadata.lastErrorTime = Date.now();
            metadata.sectionsListRevision++;
            await put(metadataStore, metadata);

            return {
                sectionsListRevision: metadata.sectionsListRevision,
                lastError: metadata.lastError,
                lastErrorTime: metadata.lastErrorTime
            };
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
     * Clears any profile error state on successful fetch.
     * @param sections The new list of sections
     * @returns Object with changed flag, the new sectionsListRevision, and error state (cleared)
     */
    public setCanonicalSectionList(sections: model.Section[]): Promise<{
        changed: boolean,
        sectionsListRevision: number,
        lastError?: string,
        lastErrorTime?: number
    }> {
        return inTransaction(this.db, [SECTIONS_STORE, PATROL_SCORES_STORE, USER_METADATA_STORE], "readwrite", async (tx: IDBTransaction) => {
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

            // Clear any profile error and get/bump revision
            const metadataStore = tx.objectStore(USER_METADATA_STORE);
            let metadata = await read<UserMetadata>(metadataStore, this.userId);

            if (!metadata) {
                metadata = new UserMetadata(this.userId);
            }

            // Clear error state on successful fetch
            metadata.lastError = undefined;
            metadata.lastErrorTime = undefined;

            if (changed) {
                metadata.sectionsListRevision++;
                console.log(`[PatrolPointsStore] Updated canonical section list: ${sections.length} sections (changes detected)`);
            } else {
                console.log(`[PatrolPointsStore] Updated canonical section list: ${sections.length} sections (no changes)`);
            }

            await put(metadataStore, metadata);

            return {
                changed,
                sectionsListRevision: metadata.sectionsListRevision,
                lastError: undefined,
                lastErrorTime: undefined
            };
        });
    }

    /**
     * Updates the local list of patrols for a section to match the provided list.
     * Patrols not in the new list will be deleted.
     * Preserves pending scores for existing patrols.
     * Clears any error state on successful refresh.
     * @param sectionId The ID of the section to update
     * @param patrols The new list of patrols and their current scores
     * @returns Object with the updated patrol list, the section's uiRevision, and error state (cleared)
     */
    public setCanonicalPatrolList(sectionId: number, patrols: SyncPatrolResult[]): Promise<{
        patrols: Patrol[],
        uiRevision: number,
        lastError?: string,
        lastErrorTime?: number
    }> {
        return inTransaction(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readwrite", async (tx: IDBTransaction) => {
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

            // Update the section's last update timestamp, bump UI revision, and clear error state
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionKey = getSectionKey(this.userId, sectionId);
            const section = await read<Section>(sectionsStore, sectionKey);
            if (!section) {
                throw new Error(`Section ${sectionId} not found for user ${this.userId}`);
            }

            section.lastRefresh = Date.now();
            section.uiRevision++;
            section.lastError = undefined;  // Clear error on successful refresh
            section.lastErrorTime = undefined;
            await put(sectionsStore, section);
            const uiRevision = section.uiRevision;

            console.log(`[PatrolPointsStore] Updated canonical patrol list for section ${sectionId}: ${patrols.length} patrols`);

            // Return the updated patrol list with revision and cleared error state
            const updatedPatrols = await getAllFromIndex<Patrol>(index, range);
            return {
                patrols: updatedPatrols,
                uiRevision,
                lastError: undefined,
                lastErrorTime: undefined
            };
        });
    }

    /** Add points to the store for the given patrol in the given section. The patrol must already exist. */
    async addPendingPoints(sectionId: number, patrolId: string, pointsDelta: number): Promise<number> {
        console.log(`[PatrolPointsStore] Adding points: section=${sectionId}, patrol=${patrolId}, delta=${pointsDelta}`);
        return inTransaction<number>(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<number> => {
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

            // Bump section UI revision
            await bumpSectionUiRevision(tx, this.userId, sectionId);

            console.log(`[PatrolPointsStore] New pending delta: ${newPendingDelta}`);
            return newPendingDelta;
        });
    }

    /** Get all scores for the current user and section with the current UI revision and error state */
    async getScoresForSection(sectionId: number): Promise<{
        patrols: Patrol[],
        uiRevision: number,
        lastError?: string,
        lastErrorTime?: number
    }> {
        return inTransaction(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readonly", async (tx: IDBTransaction) => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);
            const patrols = await getAllFromIndex<Patrol>(index, range);

            // Get the section's UI revision and error state
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionKey = getSectionKey(this.userId, sectionId);
            const section = await read<Section>(sectionsStore, sectionKey);

            return {
                patrols,
                uiRevision: section?.uiRevision ?? 0,
                lastError: section?.lastError,
                lastErrorTime: section?.lastErrorTime
            };
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

                            console.log(`[PatrolPointsStore] Acquiring lock for patrol ${patrol.patrolId} in section ${sectionId}`, {
                                'pendingScoreDelta': patrol.pendingScoreDelta,
                                'lockTimeout': patrol.lockTimeout,
                                'retryAfter': patrol.retryAfter,
                                'now': now,
                                'lockTimeout <= now': patrol.lockTimeout <= now,
                                'retryAfter <= now': patrol.retryAfter <= now
                            });

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
        return inTransaction<void>(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);

            let hadChanges = false;
            await new Promise<void>((resolve, reject) => {
                const request = index.openCursor(range);
                request.onerror = () => reject(request.error);
                request.onsuccess = async () => {
                    const cursor = request.result;
                    if (cursor) {
                        const patrol = cursor.value as Patrol;
                        // Only mark entries with pending changes
                        if (patrol.pendingScoreDelta !== 0) {
                            hadChanges = true;
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

            if (hadChanges) {
                await bumpSectionUiRevision(tx, this.userId, sectionId);
            }

            console.log(`[PatrolPointsStore] Marked all pending entries in section ${sectionId} as failed: ${errorMessage}`);
        });
    }

    /**
     * Atomically acquire a section-level sync lock for cross-tab coordination.
     * Returns null if lock is already held (by another tab or process).
     * @param sectionId The section to lock
     * @param lockDurationMs Lock duration in milliseconds (default 60s)
     * @returns Lock ID if acquired, null if already locked
     */
    async acquireSectionSyncLock(sectionId: number, lockDurationMs: number = 60000): Promise<string | null> {
        const lockId = crypto.randomUUID();
        const lockExpiry = Date.now() + lockDurationMs;

        return inTransaction<string | null>(this.db, [SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<string | null> => {
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionKey = getSectionKey(this.userId, sectionId);
            const section = await read<Section>(sectionsStore, sectionKey);

            if (!section) {
                throw new Error(`Section ${sectionId} not found for user ${this.userId}`);
            }

            const now = Date.now();

            // Check if lock is already held and not expired
            if (section.syncLockTimeout > now) {
                console.log(`[PatrolPointsStore] Section ${sectionId} sync lock held by ${section.syncLockId} until ${new Date(section.syncLockTimeout).toISOString()}`);
                return null;
            }

            // Acquire lock
            section.syncLockTimeout = lockExpiry;
            section.syncLockId = lockId;
            section.uiRevision++; // Bump revision so other tabs see lock state
            await put(sectionsStore, section);

            console.log(`[PatrolPointsStore] Acquired section ${sectionId} sync lock ${lockId} until ${new Date(lockExpiry).toISOString()}`);
            return lockId;
        });
    }

    /**
     * Release the section-level sync lock.
     * Only the lock holder (matching lockId) can release.
     * @param sectionId The section to unlock
     * @param lockId The lock ID that was returned from acquireSectionSyncLock
     */
    async releaseSectionSyncLock(sectionId: number, lockId: string): Promise<void> {
        return inTransaction<void>(this.db, [SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionKey = getSectionKey(this.userId, sectionId);
            const section = await read<Section>(sectionsStore, sectionKey);

            if (!section) {
                throw new Error(`Section ${sectionId} not found for user ${this.userId}`);
            }

            // Only unlock if this lockId matches
            if (section.syncLockId === lockId) {
                section.syncLockTimeout = 0;
                section.syncLockId = undefined;
                section.uiRevision++;
                await put(sectionsStore, section);
                console.log(`[PatrolPointsStore] Released section ${sectionId} sync lock ${lockId}`);
            } else {
                console.warn(`[PatrolPointsStore] Cannot release section ${sectionId} lock: lockId mismatch (held by ${section.syncLockId}, requested ${lockId})`);
            }
        });
    }

    /**
     * Calculate the next optimal sync time for a section with 30s batching window.
     * Returns the timestamp when the next sync should occur, or null if no pending entries.
     * Batching: if soonest retry is 10s away and next is 15s away, returns 15s to batch both.
     * @param sectionId The section to check
     * @returns Timestamp in milliseconds when next sync should occur, or null if no pending
     */
    async getNextSyncTime(sectionId: number): Promise<number | null> {
        const BATCHING_WINDOW_MS = 30000; // 30 seconds

        return inTransaction<number | null>(this.db, [PATROL_SCORES_STORE], "readonly", async (tx: IDBTransaction): Promise<number | null> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);
            const now = Date.now();

            const retryTimes: number[] = [];

            await new Promise<void>((resolve, reject) => {
                const request = index.openCursor(range);
                request.onerror = () => reject(request.error);
                request.onsuccess = () => {
                    const cursor = request.result;
                    if (cursor) {
                        const patrol = cursor.value as Patrol;
                        // Only consider entries with pending changes and non-permanent errors
                        if (patrol.pendingScoreDelta !== 0 && patrol.retryAfter >= 0) {
                            retryTimes.push(patrol.retryAfter);
                        }
                        cursor.continue();
                    } else {
                        resolve();
                    }
                };
            });

            if (retryTimes.length === 0) {
                return null;
            }

            // Sort retry times
            retryTimes.sort((a, b) => a - b);

            // Find the latest retry time within the batching window of the soonest
            const soonest = retryTimes[0];
            const batchDeadline = Math.max(soonest, now) + BATCHING_WINDOW_MS;

            // Find last retry time that falls within the batch window
            let batchedTime = soonest;
            for (const retryTime of retryTimes) {
                if (retryTime <= batchDeadline) {
                    batchedTime = retryTime;
                } else {
                    break; // Times are sorted, so we can stop
                }
            }

            return Math.max(batchedTime, now);
        });
    }

    /**
     * Clear permanent errors (retryAfter = -1) for all pending entries in a section.
     * Resets retryAfter to 0 (ready now) for force sync scenarios.
     * @param sectionId The section to clear errors for
     * @returns Number of entries cleared
     */
    async clearPermanentErrors(sectionId: number): Promise<number> {
        return inTransaction<number>(this.db, [PATROL_SCORES_STORE, SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<number> => {
            const store = tx.objectStore(PATROL_SCORES_STORE);
            const index = store.index(INDEX_USERID_SECTIONID);
            const range = IDBKeyRange.only([this.userId, sectionId]);

            let clearedCount = 0;

            await new Promise<void>((resolve, reject) => {
                const request = index.openCursor(range);
                request.onerror = () => reject(request.error);
                request.onsuccess = async () => {
                    const cursor = request.result;
                    if (cursor) {
                        const patrol = cursor.value as Patrol;
                        // Clear permanent errors only (preserve temporary errors with future timestamps)
                        if (patrol.pendingScoreDelta !== 0 && patrol.retryAfter === -1) {
                            patrol.retryAfter = 0; // Ready to retry now
                            patrol.errorMessage = undefined; // Clear error message
                            await put(store, patrol);
                            clearedCount++;
                        }
                        cursor.continue();
                    } else {
                        resolve();
                    }
                };
            });

            if (clearedCount > 0) {
                await bumpSectionUiRevision(tx, this.userId, sectionId);
            }

            console.log(`[PatrolPointsStore] Cleared ${clearedCount} permanent errors in section ${sectionId}`);
            return clearedCount;
        });
    }

    /**
     * Update section sync timing metadata after a sync operation.
     * Recalculates nextRetryTime and updates lastSyncAttempt.
     * @param sectionId The section that was just synced
     */
    async updateSectionSyncTiming(sectionId: number): Promise<void> {
        const nextRetryTime = await this.getNextSyncTime(sectionId);

        return inTransaction<void>(this.db, [SECTIONS_STORE], "readwrite", async (tx: IDBTransaction): Promise<void> => {
            const sectionsStore = tx.objectStore(SECTIONS_STORE);
            const sectionKey = getSectionKey(this.userId, sectionId);
            const section = await read<Section>(sectionsStore, sectionKey);

            if (!section) {
                throw new Error(`Section ${sectionId} not found for user ${this.userId}`);
            }

            section.lastSyncAttempt = Date.now();
            section.nextRetryTime = nextRetryTime ?? 0;
            section.uiRevision++;
            await put(sectionsStore, section);

            console.log(`[PatrolPointsStore] Updated section ${sectionId} sync timing: nextRetryTime=${nextRetryTime ? new Date(nextRetryTime).toISOString() : 'none'}`);
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

/**
 * Increment the section's UI revision and return the new value.
 * This must be called within a transaction that has write access to the sections store.
 * @param tx The transaction
 * @param userId The user ID
 * @param sectionId The section ID
 * @returns The new UI revision number
 */
async function bumpSectionUiRevision(tx: IDBTransaction, userId: number, sectionId: number): Promise<number> {
    const sectionsStore = tx.objectStore(SECTIONS_STORE);
    const sectionKey = getSectionKey(userId, sectionId);
    const section = await read<Section>(sectionsStore, sectionKey);

    if (!section) {
        throw new Error(`Section ${sectionId} not found for user ${userId}`);
    }

    section.uiRevision++;
    await put(sectionsStore, section);
    return section.uiRevision;
}

