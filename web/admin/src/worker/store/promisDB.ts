export type UnitOfWork<T> = (tx: IDBTransaction) => Promise<T>

export async function inTransaction<T>(db : IDBDatabase, stores: string[], mode: IDBTransactionMode, fn: UnitOfWork<T>) : Promise<T> {
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

export function read<T>(store: IDBObjectStore, key: IDBValidKey | IDBKeyRange) : Promise<T> {
    return new Promise((resolve, reject) => {
        const request = store.get(key);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(request.result);
    })
}

export function getAll<V>(store: IDBObjectStore) : Promise<V[]> {
    return new Promise((resolve,reject) => {
        const request = store.getAll();
        request.onerror = () => reject(request.error);
        request.onsuccess = () => {
            resolve(request.result);
        };
    });
}

export function put<T>(store: IDBObjectStore, value: T, key?: IDBValidKey): Promise<IDBValidKey> {
    return new Promise((resolve, reject) => {
        const request = key !== undefined ? store.put(value, key) : store.put(value);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(request.result);
    });
}

export function deleteRecord(store: IDBObjectStore, key: IDBValidKey | IDBKeyRange) : Promise<void> {
    return new Promise((resolve, reject) => {
        const request = store.delete(key);
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve();
    })
}

export function getAllFromIndex<V>(index: IDBIndex, range?: IDBKeyRange): Promise<V[]> {
    return new Promise((resolve, reject) => {
        const request = range ? index.getAll(range) : index.getAll();
        request.onerror = () => reject(request.error);
        request.onsuccess = () => resolve(request.result);
    });
}
