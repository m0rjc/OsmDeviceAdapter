import {combineReducers, createSelector} from '@reduxjs/toolkit';
import userReducer from './userSlice';
import dialogReducer from './dialogSlice';
import patrolsReducer, {
    type PatrolsState,
    type SectionMetadata,
    selectPatrolFromPatrolState,
    selectSectionsById
} from './patrolsSlice.ts';
import uiReducer, {selectSelectedSectionId, selectUserScoreForPatrolKeyFromUiState, type UiState} from './uiSlice';

export const rootReducer = combineReducers({
    user: userReducer,
    dialog: dialogReducer,
    patrols: patrolsReducer,
    ui: uiReducer,
})

export type RootState = ReturnType<typeof rootReducer>;
export type AppSelector<T> = (state: RootState) => T;
export const createAppSelector = createSelector.withTypes<RootState>();

/**
 * Selects the currently selected section.
 */
export const selectSelectedSection: AppSelector<SectionMetadata | null> = createAppSelector(
    [selectSelectedSectionId, selectSectionsById],
    (id: number | null, records: Record<number, SectionMetadata>): SectionMetadata | null => id === null ? null : records[id]);

// The constant EMPTY_KEYS allows REACT to avoid re-rendering the PatrolList component
// when the patrols array changes from empty to empty.
const EMPTY_KEYS = [] as Array<string>;
/**
 * Selects the patrol keys for the currently selected section.
 */
export const selectSelectedPatrolKeys: AppSelector<Array<string>> =
    createAppSelector([selectSelectedSection], (section: SectionMetadata | null): Array<string> => section?.patrols ?? EMPTY_KEYS);

export type UserChange = { patrolId: string, name: string, score: number };


export const selectChangesForCurrentSection: AppSelector<Array<UserChange>> =
    createAppSelector([
            selectSelectedSection,
            (state: RootState): PatrolsState => state.patrols,
            (state: RootState): UiState => state.ui
        ],
        (section: SectionMetadata | null, patrols: PatrolsState, ui: UiState): UserChange[] =>
            section?.patrols.map((p: string): UserChange => ({
                patrolId: p,
                name: selectPatrolFromPatrolState(patrols, p)?.name ?? 'unknown',
                score: selectUserScoreForPatrolKeyFromUiState(ui, p)
            })).filter((s: UserChange): boolean => s.score !== 0) ?? []
    );