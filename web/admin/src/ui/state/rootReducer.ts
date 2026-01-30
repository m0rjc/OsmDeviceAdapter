import {combineReducers, createSelector} from '@reduxjs/toolkit';
import * as patrols from './patrolsSlice.ts';
import * as user from './userSlice';
import * as dialog from './dialogSlice';
import * as ui from './uiSlice';
import * as app from './appSlice';
import patrolsReducer from './patrolsSlice.ts';
import userReducer from './userSlice';
import dialogReducer from './dialogSlice';
import uiReducer from './uiSlice';
import appReducer from './appSlice';

export const rootReducer = combineReducers({
    user: userReducer,
    dialog: dialogReducer,
    patrols: patrolsReducer,
    ui: uiReducer,
    app: appReducer,
})

export type RootState = ReturnType<typeof rootReducer>;
export type AppSelector<T> = (state: RootState) => T;
export const createAppSelector = createSelector.withTypes<RootState>();

// Re-export types from slices
export type {PatrolsState, SectionMetadata, UIPatrol} from './patrolsSlice.ts';
export type {UserState} from './userSlice';
export type {DialogState} from './dialogSlice';
export type {UiState} from './uiSlice';
export type {AppState} from './appSlice';

// Slice state extractors
const selectPatrolState: AppSelector<patrols.PatrolsState> = (state) => state.patrols;
const selectUserState: AppSelector<user.UserState> = (state) => state.user;
const selectDialogState: AppSelector<dialog.DialogState> = (state) => state.dialog;
const selectUiState: AppSelector<ui.UiState> = (state) => state.ui;
const selectAppState: AppSelector<app.AppState> = (state) => state.app;

// App-level selectors for user slice
export const selectUserId = createAppSelector([selectUserState], user.selectUserId);
export const selectUserName = createAppSelector([selectUserState], user.selectUserName);
export const selectIsAuthenticated = createAppSelector([selectUserState], user.selectIsAuthenticated);
export const selectIsLoading = createAppSelector([selectUserState], user.selectIsLoading);

// App-level selectors for dialog slice
export {selectDialogState};
export const selectIsErrorDialogOpen = createAppSelector([selectDialogState], dialog.selectIsErrorDialogOpen);
export const selectErrorTitle = createAppSelector([selectDialogState], dialog.selectErrorTitle);
export const selectErrorMessage = createAppSelector([selectDialogState], dialog.selectErrorMessage);
export const selectGlobalError = createAppSelector([selectDialogState], dialog.selectGlobalError);

// App-level selectors for ui slice
export const selectSelectedSectionId = createAppSelector([selectUiState], ui.selectSelectedSectionId);
export const selectUserScoreForPatrolKey = (state: RootState, patrolKey: string): number =>
    ui.selectUserScoreForPatrolKey(state.ui, patrolKey);


// App-level selectors for patrols slice
export const selectSections = createAppSelector(
    [selectPatrolState],
    patrols.selectSections
);

/**
 * Selects the currently selected section.
 */
export const selectSelectedSection: AppSelector<patrols.SectionMetadata | null> = createAppSelector(
    [selectPatrolState, selectSelectedSectionId],
    (state, id) => id !== null ? patrols.selectSectionById(state, id) : null
);

// The constant EMPTY_KEYS allows REACT to avoid re-rendering the PatrolList component
// when the patrols array changes from empty to empty.
const EMPTY_KEYS = [] as Array<string>;

/**
 * Selects the patrol keys for the currently selected section.
 */
export const selectSelectedPatrolKeys: AppSelector<Array<string>> =
    createAppSelector([selectSelectedSection], (section) => section?.patrols ?? EMPTY_KEYS);

export type UserChange = { patrolId: string, name: string, score: number };

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
type UIPatrolSelectorFactory = () => UIPatrolSelector;
type UIPatrolSelector = (state: RootState, patrolKey: string) => patrols.UIPatrol | undefined;
export const makeSelectPatrolById: UIPatrolSelectorFactory = (): UIPatrolSelector =>
    createAppSelector([
            (state: RootState) => state.patrols,
            (_: RootState, patrolKey: string) => patrolKey
        ],
        patrols.selectPatrolById
    );

export const selectChangesForCurrentSection: AppSelector<Array<UserChange>> =
    createAppSelector([
            selectSelectedSection,
            selectPatrolState,
            selectUiState
        ],
        (section, patrolsState, uiState): UserChange[] =>
            section?.patrols.map((p: string): UserChange => ({
                patrolId: p,
                name: patrols.selectPatrolById(patrolsState, p)?.name ?? 'unknown',
                score: ui.selectUserScoreForPatrolKey(uiState, p)
            })).filter((s: UserChange): boolean => s.score !== 0) ?? []
    );

// App-level selectors for app slice (PWA lifecycle)
export const selectShouldShowUpdatePrompt = createAppSelector([selectAppState], app.selectShouldShowUpdatePrompt);
export const selectUpdateAvailable = createAppSelector([selectAppState], app.selectUpdateAvailable);