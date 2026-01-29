import {
    createEntityAdapter,
    createSlice,
    type EntityAdapter, type PayloadAction,
} from '@reduxjs/toolkit'
import * as model from "../../types/model";
import type {LoadingState} from "./loadState.ts";
import {type AppSelector, createAppSelector, type RootState} from "./rootReducer.ts";

/**
 * UI representation of a patrol combining server state with local user input.
 */
export type UIPatrol = model.PatrolScore & {
    /** Internal key used by the entity adapter */
    key: string;
}

function makePatrolKey(sectionId: number, patrolId: string) : string {
    return `${sectionId}:${patrolId}`;
}

/**
 * Check if a patrol key belongs to a specific section.
 * @param patrolKey The composite key (sectionId:patrolId)
 * @param sectionId The section ID to check
 * @returns true if the key belongs to the section
 */
export function patrolKeyBelongsToSection(patrolKey: string, sectionId: number): boolean {
    return patrolKey.startsWith(`${sectionId}:`);
}

const entityAdapter : EntityAdapter<UIPatrol, string> = createEntityAdapter({
    selectId:  (p: UIPatrol):string => p.key,
    sortComparer: (a: UIPatrol, b: UIPatrol):number => a.name.localeCompare(b.name)
});

export type SectionMetadata = model.Section & {
    /** Patrol keys ordered by patrol name */
    patrols: Array<string>,
    state: LoadingState,
    version: number,
    error?: string
}

const initialState = entityAdapter.getInitialState({
    sectionIdListVersion: -1,
    sectionListError: undefined as string | undefined,
    sectionListState: 'uninitialized' as LoadingState,
    sectionIds: [] as number[],
    sections: {} as Record<number, SectionMetadata>
});

export type SetCanonicalPatrolsPayload = {sectionId: number, version: number, patrols: Array<model.PatrolScore>};
export type SetCanonicalSectionsPayload = {version: number, sections: Array<model.Section>};
export type VersionedErrorPayload = {version: number, error: string};
export type SectionErrorPayload = {sectionId: number, version: number, error: string};

const patrolsSlice = createSlice({
    name: 'patrols',
    initialState: initialState,
    reducers: {
        /**
         * Updates state to reflect a canonical section list.
         * Cleans out old patrols if the section has been removed.
         * @param state
         * @param action
         */
        setCanonicalSectionList: (state, action: PayloadAction<SetCanonicalSectionsPayload>) => {
            const {version, sections} = action.payload;
            if (version <= state.sectionIdListVersion) {
                return;
            }
            const existingSectionIds = new Set(state.sectionIds);

            // Merge new sections into state
            sections.forEach(section => {
                const meta = state.sections[section.id] || {state: 'uninitialized', version: -1, patrols: []};
                state.sections[section.id] = {...section, ...meta};
                existingSectionIds.delete(section.id);
            });

            // Clean deleted sections and their patrols from the state
            existingSectionIds.forEach(sectionId => {
                entityAdapter.removeMany(state, state.sections[sectionId].patrols);
                delete state.sections[sectionId];
            });

            // Update metadata
            const sortedSections = [...sections].sort((a, b) => a.name.localeCompare(b.name));
            state.sectionIdListVersion = version;
            state.sectionIds = sortedSections.map(s => s.id);
            state.sectionListError = undefined;
            state.sectionListState = 'ready';
        },

        /**
         * Updates state to reflect an error loading the section list.
         * @param state
         * @param action
         */
        setSectionListLoadError: (state, action: PayloadAction<VersionedErrorPayload>) => {
          if(action.payload.version <= state.sectionIdListVersion) return;
          state.sectionListError = action.payload.error;
          state.sectionListState = 'error';
        },

        /**
         * - Adds new patrols
         * - Updates existing patrols
         * - Removes patrols not in the new list
         */
        setCanonicalPatrols: (state, action: PayloadAction<SetCanonicalPatrolsPayload>) => {
            const sectionId:number = action.payload.sectionId;
            const serverPatrols: model.PatrolScore[] = action.payload.patrols;
            const stateMeta: SectionMetadata = state.sections[sectionId] || {state: 'uninitialized', version: -1, patrols: []};
            const statePatrolsKeys: Set<string> = new Set(stateMeta.patrols);

            if (stateMeta.version >= action.payload.version) {
                return;
            }

            const upserts: UIPatrol[] = serverPatrols.map( (p:model.PatrolScore):UIPatrol => ({
                ...p,
                key: makePatrolKey(sectionId, p.id)
            }));
            upserts.sort((a:UIPatrol, b:UIPatrol):number => a.name.localeCompare(b.name));
            entityAdapter.upsertMany(state, upserts);

            // Work out which patrols were removed
            upserts.forEach(p => statePatrolsKeys.delete(p.key));
            entityAdapter.removeMany(state, [...statePatrolsKeys]);

            state.sections[sectionId] = {
                ...stateMeta,
                state: 'ready',
                version: action.payload.version,
                patrols: upserts.map(p => p.key),
                error: undefined
            };
            if(!state.sectionIds.includes(sectionId)) {state.sectionIds.push(sectionId)}
        },

        /**
         * Updates the loading state for a section.
         * @param state
         * @param action
         */
        setSectionState: (state, action: PayloadAction<{sectionId: number, stateName: LoadingState}>) => {
          const {sectionId, stateName} = action.payload;
          state.sections[sectionId].state = stateName;
        },

        /**
         * Updates the error state for a section.
         * @param state
         * @param action
         */
        setSectionError: (state, action: PayloadAction<SectionErrorPayload>) => {
            const {sectionId, version, error} = action.payload;
            const meta = state.sections[sectionId] || {state: 'uninitialized', version: -1, patrols: []};
            if (meta.version >= version) {
                return;
            }
            state.sections[sectionId] = {...meta, version, state: 'error', error};
        }
    },
});

export const {setCanonicalPatrols,setSectionListLoadError,setCanonicalSectionList,setSectionState,setSectionError} = patrolsSlice.actions;
export type PatrolsState = ReturnType<typeof patrolsSlice.reducer>;

export type UISection = Pick<SectionMetadata, 'id'|'name'|'groupName'|'state'|'error'>;

const selectSectionIds : AppSelector<Array<number>> = (state:RootState) : Array<number> => state.patrols.sectionIds
export const selectSectionsById : AppSelector<Record<number,SectionMetadata>> = (state:RootState) : Record<number, SectionMetadata> => state.patrols.sections

/**
 * Selects all section header data sorted by name
 */
export const selectSections : AppSelector<Array<UISection>> = createAppSelector(
    [selectSectionIds, selectSectionsById],
    // sectionIds is already sorted by section name
    (sectionIds: Array<number>, sections:Record<number,SectionMetadata>):Array<UISection> =>
        sectionIds.map(id => sections[id]));

type UIPatrolSelector = (state:RootState, patrolKey: string) => UIPatrol | undefined;
type UIPatrolSelectorFactory = () => UIPatrolSelector;

/**
 * A Selector Factory allows memoisation of the selector function. This avoids cache thrashing
 * when different patrols use the same selector with different patrol IDs.
 * Inside the patrol card use:
 *
 * ```typescript
 * const selectPatrolById = useMemo(makeSelectPatrolById, []);
 * const patrol = useSelector(state => selectPatrolById(state, patrolKey));
 * ```
 */
export const makeSelectPatrolById : UIPatrolSelectorFactory = ():UIPatrolSelector =>
    createAppSelector([
        (state:RootState):PatrolsState => state.patrols,
        (_:RootState, patrolKey:string):string => patrolKey
    ],
        (patrols:PatrolsState, patrolKey:string):UIPatrol|undefined =>
            entityAdapter.getSelectors().selectById(patrols, patrolKey)
    );

export const selectPatrolFromPatrolState : (state:PatrolsState, patrolKey:string) => UIPatrol|undefined =
    (state:PatrolsState, patrolKey:string):UIPatrol|undefined => entityAdapter.getSelectors().selectById(state, patrolKey);

export default patrolsSlice.reducer;
