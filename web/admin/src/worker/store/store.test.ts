import { OpenPatrolPointsStore, PatrolPointsStore, Section, Patrol } from './store'

import "fake-indexeddb/auto";
import { IDBFactory } from "fake-indexeddb";

describe('PatrolPointsStore', () => {
  let store: PatrolPointsStore
  const userId = 123

  beforeEach(async () => {
    // Whenever you want a fresh indexedDB
    indexedDB = new IDBFactory();

    // Each test gets a fresh store
    store = await OpenPatrolPointsStore(userId)
  })

  describe('ambient test',  () => {
    it('FakeDB does not hang on a simple PUT', async () => {
      const dbName = 'ambient-test-db'
      const storeName = 'test-store'

      console.log('[jest runtime]', {
        node: process.version,
        platform: process.platform,
        arch: process.arch,
        execPath: process.execPath,
      });

      if (globalThis.structuredClone) {
        console.log('[structuredClone]', {
          supported: true,
        });
      }

      const db = await new Promise<IDBDatabase>((resolve, reject) => {
        const request = indexedDB.open(dbName, 1)
        request.onupgradeneeded = () => {
          request.result.createObjectStore(storeName, { keyPath: 'id' })
        }
        request.onsuccess = () => resolve(request.result)
        request.onerror = () => reject(request.error)
      })

      const tx = db.transaction(storeName, 'readwrite')
      const store = tx.objectStore(storeName)
      const data = { id: 1, name: 'test' }
      
      await new Promise<void>((resolve, reject) => {
        const request = store.put(data)
        request.onsuccess = () => resolve()
        request.onerror = () => reject(request.error)
      })

      const result = await new Promise<any>((resolve, reject) => {
        const request = db.transaction(storeName).objectStore(storeName).get(1)
        request.onsuccess = () => resolve(request.result)
        request.onerror = () => reject(request.error)
      })

      expect(result).toEqual(data)
      db.close()
      indexedDB.deleteDatabase(dbName)
    })

    it('should not hang in the cleanup hook on the second test', async () => {
      // Do nothing
    });
  });

  describe('Section Management', () => {
    it('should start with no sections', async () => {
      const sections = await store.getSections()
      expect(sections).toEqual([])
    })

    it('should add sections via setCanonicalSectionList', async () => {
      const sections = [
        new Section(userId, 1, 'Beavers'),
        new Section(userId, 2, 'Cubs'),
      ]

      await store.setCanonicalSectionList(sections)

      const retrieved = await store.getSections()
      expect(retrieved).toHaveLength(2)
      expect(retrieved.find(s => s.id === 1)?.name).toBe('Beavers')
    })

    it('should update section names', async () => {
      const sections = [new Section(userId, 1, 'Beavers')]
      await store.setCanonicalSectionList(sections)

      const updated = [new Section(userId, 1, 'Beavers Updated')]
      await store.setCanonicalSectionList(updated)

      const retrieved = await store.getSections()
      expect(retrieved[0].name).toBe('Beavers Updated')
    })

    it('should delete sections not in canonical list', async () => {
      const sections = [
        new Section(userId, 1, 'Beavers'),
        new Section(userId, 2, 'Cubs'),
      ]
      await store.setCanonicalSectionList(sections)

      await store.setCanonicalSectionList([sections[0]])

      const retrieved = await store.getSections()
      expect(retrieved).toHaveLength(1)
      expect(retrieved[0].id).toBe(1)
    })

    it('should delete section patrols when section is removed', async () => {
      const sections = [
        new Section(userId, 1, 'Beavers'),
        new Section(userId, 2, 'Cubs'),
      ]
      await store.setCanonicalSectionList(sections)

      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])

      // Remove section-1
      await store.setCanonicalSectionList([sections[1]])

      // Patrols for section-1 should be gone
      const patrols = await store.getScoresForSection(1)
      expect(patrols).toHaveLength(0)
    })
  })

  describe('Patrol Management', () => {
    beforeEach(async () => {
      // Setup a section for patrol tests
      await store.setCanonicalSectionList([
        new Section(userId, 1, 'Beavers')
      ])
    })

    it('should create patrols from canonical list', async () => {
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
        { id: "2", name: 'Blue', score: 20 },
      ])

      const patrols = await store.getScoresForSection(1)
      expect(patrols).toHaveLength(2)
      expect(patrols.find(p => p.patrolId === "1")?.committedScore).toBe(10)
      expect(patrols.find(p => p.patrolId === "1")?.pendingScoreDelta).toBe(0)
    })

    it('should update patrol scores and names', async () => {
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])

      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red Team', score: 15 },
      ])

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].patrolName).toBe('Red Team')
      expect(patrols[0].committedScore).toBe(15)
    })

    it('should delete patrols not in canonical list', async () => {
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
        { id: "2", name: 'Blue', score: 20 },
      ])

      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])

      const patrols = await store.getScoresForSection(1)
      expect(patrols).toHaveLength(1)
      expect(patrols[0].patrolId).toBe("1")
    })

    it('should preserve pending deltas when updating patrol list', async () => {
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])

      // Add local pending changes
      await store.addPendingPoints(1, "1", 5)

      // Update the canonical list (but don't change this patrol)
      const patrols = await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])

      // Pending delta should be preserved
      expect(patrols[0].pendingScoreDelta).toBe(5)
    })

    it('should update section lastRefresh timestamp', async () => {
      const beforeTime = Date.now()

      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])

      const sections = await store.getSections()
      expect(sections[0].lastRefresh).toBeGreaterThanOrEqual(beforeTime)
    })
  })

  describe('Score Updates', () => {
    beforeEach(async () => {
      await store.setCanonicalSectionList([
        new Section(userId, 1, 'Beavers')
      ])
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])
    })

    it('should add points to existing patrol', async () => {
      const delta = await store.addPendingPoints(1, "1", 5)

      expect(delta).toBe(5)

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].committedScore).toBe(10)
      expect(patrols[0].pendingScoreDelta).toBe(5)
    })

    it('should accumulate multiple point additions', async () => {
      await store.addPendingPoints(1, "1", 5)
      const delta = await store.addPendingPoints(1, "1", 3)

      expect(delta).toBe(8)

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].pendingScoreDelta).toBe(8)
    })

    it('should handle negative point additions', async () => {
      await store.addPendingPoints(1, "1", 5)
      const delta = await store.addPendingPoints(1, "1", -2)

      expect(delta).toBe(3)

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].pendingScoreDelta).toBe(3)
    })

    it('should throw error when adding points to non-existent patrol', async () => {
      await expect(
        store.addPendingPoints(1, "999", 5)
      ).rejects.toThrow('Patrol 999 does not exist')
    })

    it('should clear pending delta when setting committed score', async () => {
      await store.addPendingPoints(1, "1", 5)
      await store.setCommittedScore(1, "1", 15, 'Red')

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].committedScore).toBe(15)
      expect(patrols[0].pendingScoreDelta).toBe(0)
    })

    it('should update patrol name when setting committed score', async () => {
      await store.setCommittedScore(1, "1", 12, 'Red Team')

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].patrolName).toBe('Red Team')
      expect(patrols[0].committedScore).toBe(12)
    })
  })

  describe('Error Handling', () => {
    beforeEach(async () => {
      await store.setCanonicalSectionList([
        new Section(userId, 1, 'Beavers')
      ])
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])
    })

    it('should set retry after timestamp', async () => {
      await store.addPendingPoints(1, "1", 5)

      const retryTime = new Date(Date.now() + 60000) // 1 minute from now
      await store.setRetryAfter(1, "1", retryTime, 'Temporary error')

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].retryAfter).toBe(retryTime.getTime())
      expect(patrols[0].errorMessage).toBe('Temporary error')
      expect(patrols[0].pendingScoreDelta).toBe(5) // Should preserve pending delta
    })

    it('should set permanent error', async () => {
      await store.addPendingPoints(1, "1", 5)
      await store.setError(1, "1", 'Permanent failure')

      const patrols = await store.getScoresForSection(1)
      expect(patrols[0].retryAfter).toBe(-1)
      expect(patrols[0].errorMessage).toBe('Permanent failure')
    })

    it('should throw error when setting retry for non-existent patrol', async () => {
      const retryTime = new Date(Date.now() + 60000)
      await expect(
        store.setRetryAfter(1, "999", retryTime)
      ).rejects.toThrow('Patrol 999 does not exist')
    })

    it('should throw error when setting error for non-existent patrol', async () => {
      await expect(
        store.setError(1, "999", 'Error message')
      ).rejects.toThrow('Patrol 999 does not exist')
    })
  })

  describe('Pending Sync Queries', () => {
    beforeEach(async () => {
      await store.setCanonicalSectionList([
        new Section(userId, 1, 'Beavers')
      ])
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
        { id: "2", name: 'Blue', score: 20 },
        { id: "3", name: 'Green', score: 30 },
      ])
    })

    it('should return patrols ready to sync now', async () => {
      await store.addPendingPoints(1, "1", 5)
      await store.addPendingPoints(1, "2", 3)

      const pending = await store.getPendingForSyncNow(1)
      expect(pending).toHaveLength(2)
      expect(pending.map(p => p.patrolId).sort()).toEqual(["1", "2"])
    })

    it('should not return patrols without pending changes', async () => {
      await store.addPendingPoints(1, "1", 5)
      // patrol-2 has no pending changes

      const pending = await store.getPendingForSyncNow(1)
      expect(pending).toHaveLength(1)
      expect(pending[0].patrolId).toBe("1")
    })

    it('should not return patrols with retryAfter in future', async () => {
      await store.addPendingPoints(1, "1", 5)

      const futureDate = new Date(Date.now() + 60000) // 1 minute from now
      await store.setRetryAfter(1, "1", futureDate)

      const pending = await store.getPendingForSyncNow(1)
      expect(pending).toHaveLength(0)
    })

    it('should return patrols with retryAfter in past', async () => {
      await store.addPendingPoints(1, "1", 5)

      const pastDate = new Date(Date.now() - 60000) // 1 minute ago
      await store.setRetryAfter(1, "1", pastDate)

      const pending = await store.getPendingForSyncNow(1)
      expect(pending).toHaveLength(1)
      expect(pending[0].patrolId).toBe("1")
    })

    it('should not return patrols with permanent errors', async () => {
      await store.addPendingPoints(1, "1", 5)
      await store.setError(1, "1", 'Permanent failure')

      const pending = await store.getPendingForSyncNow(1)
      expect(pending).toHaveLength(0)
    })

    it('should get failed entries', async () => {
      await store.addPendingPoints(1, "1", 5)
      await store.setError(1, "1", 'Permanent failure')

      const failed = await store.getFailedEntries()
      expect(failed).toHaveLength(1)
      expect(failed[0].errorMessage).toBe('Permanent failure')
    })

    it('should not return failed entries without pending changes', async () => {
      // Don't add any points
      await store.setError(1, "1", 'Error but no pending changes')

      const failed = await store.getFailedEntries()
      expect(failed).toHaveLength(0)
    })

    it('should get soonest retry time', async () => {
      await store.addPendingPoints(1, "1", 5)
      await store.addPendingPoints(1, "2", 3)

      const retryTime1 = new Date(Date.now() + 60000) // 1 minute
      const retryTime2 = new Date(Date.now() + 120000) // 2 minutes

      await store.setRetryAfter(1, "1", retryTime1)
      await store.setRetryAfter(1, "2", retryTime2)

      const soonest = await store.getSoonestRetryAfter()
      expect(soonest).toBe(retryTime1.getTime())
    })

    it('should return null when no pending entries', async () => {
      const soonest = await store.getSoonestRetryAfter()
      expect(soonest).toBeNull()
    })

    it('should ignore permanent errors when finding soonest retry', async () => {
      await store.addPendingPoints(1, "1", 5)
      await store.addPendingPoints(1, "2", 3)

      await store.setError(1, "1", 'Permanent')
      const retryTime = new Date(Date.now() + 60000)
      await store.setRetryAfter(1, "2", retryTime)

      const soonest = await store.getSoonestRetryAfter()
      expect(soonest).toBe(retryTime.getTime())
    })
  })

  describe('User Isolation', () => {
    it('should isolate data between users', async () => {
      const user1Store = await OpenPatrolPointsStore(1)
      const user2Store = await OpenPatrolPointsStore(2)

      await user1Store.setCanonicalSectionList([
        new Section(1, 1, 'User 1 Section')
      ])

      await user2Store.setCanonicalSectionList([
        new Section(2, 1, 'User 2 Section')
      ])

      const user1Sections = await user1Store.getSections()
      const user2Sections = await user2Store.getSections()

      expect(user1Sections[0].name).toBe('User 1 Section')
      expect(user2Sections[0].name).toBe('User 2 Section')
    })

    it('should isolate patrols between users', async () => {
      const user1Store = await OpenPatrolPointsStore(1)
      const user2Store = await OpenPatrolPointsStore(2)

      // Both users have a section with same ID
      await user1Store.setCanonicalSectionList([new Section(1, 1, 'Section')])
      await user2Store.setCanonicalSectionList([new Section(2, 1, 'Section')])

      await user1Store.setCanonicalPatrolList(1, [
        { id: "1", name: 'User 1 Patrol', score: 10 }
      ])

      await user2Store.setCanonicalPatrolList(1, [
        { id: "1", name: 'User 2 Patrol', score: 20 }
      ])

      const user1Patrols = await user1Store.getScoresForSection(1)
      const user2Patrols = await user2Store.getScoresForSection(1)

      expect(user1Patrols[0].patrolName).toBe('User 1 Patrol')
      expect(user2Patrols[0].patrolName).toBe('User 2 Patrol')
    })

    it('should isolate pending entries between users', async () => {
      const user1Store = await OpenPatrolPointsStore(1)
      const user2Store = await OpenPatrolPointsStore(2)

      await user1Store.setCanonicalSectionList([new Section(1, 1, 'Section')])
      await user2Store.setCanonicalSectionList([new Section(2, 1, 'Section')])

      await user1Store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Patrol', score: 10 }
      ])
      await user2Store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Patrol', score: 10 }
      ])

      await user1Store.addPendingPoints(1, "1", 5)
      await user2Store.addPendingPoints(1, "1", 3)

      const user1Pending = await user1Store.getPendingForSyncNow(1)
      const user2Pending = await user2Store.getPendingForSyncNow(1)

      expect(user1Pending[0].pendingScoreDelta).toBe(5)
      expect(user2Pending[0].pendingScoreDelta).toBe(3)
    })
  })

  describe('deleteUserData', () => {
    it('should delete all sections and patrols for user', async () => {
      await store.setCanonicalSectionList([
        new Section(userId, 1, 'Beavers')
      ])
      await store.setCanonicalPatrolList(1, [
        { id: "1", name: 'Red', score: 10 },
      ])

      await store.deleteUserData()

      const sections = await store.getSections()
      const patrols = await store.getScoresForSection(1)

      expect(sections).toHaveLength(0)
      expect(patrols).toHaveLength(0)
    })

    it('should only delete current user data, not other users', async () => {
      const user2Store = await OpenPatrolPointsStore(2)

      await store.setCanonicalSectionList([
        new Section(userId, 1, 'User 1 Section')
      ])
      await user2Store.setCanonicalSectionList([
        new Section(2, 1, 'User 2 Section')
      ])

      await store.deleteUserData()

      const user1Sections = await store.getSections()
      const user2Sections = await user2Store.getSections()

      expect(user1Sections).toHaveLength(0)
      expect(user2Sections).toHaveLength(1)
    })
  })
})
