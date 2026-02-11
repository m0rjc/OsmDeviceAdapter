import {combineReducers, createSelector} from '@reduxjs/toolkit';
import * as patrols from './patrolsSlice.ts';
import * as user from './userSlice';
import * as dialog from './dialogSlice';
import * as ui from './uiSlice';
import type {AppState} from './appSlice';
import * as settings from './settingsSlice';
import patrolsReducer, {selectPatrolById, type UIPatrol} from './patrolsSlice.ts';
import userReducer from './userSlice';
import dialogReducer from './dialogSlice';
import uiReducer from './uiSlice';
import appReducer from './appSlice';
import settingsReducer from './settingsSlice';

export const rootReducer = combineReducers({
    user: userReducer,
    dialog: dialogReducer,
    patrols: patrolsReducer,
    ui: uiReducer,
    app: appReducer,
    settings: settingsReducer,
})

export type RootState = ReturnType<typeof rootReducer>;
export type AppSelector<T> = (state: RootState) => T;
export const createAppSelector = createSelector.withTypes<RootState>();

// Re-export types from slices
export type {PatrolsState, SectionMetadata, UIPatrol} from './patrolsSlice.ts';
export type {UserState} from './userSlice';
export type {DialogState} from './dialogSlice';
export type {UiState} from './uiSlice';
export type {AppState};
export type {SettingsStateType as SettingsState, SectionSettingsState, PatrolInfo} from './settingsSlice';

// Slice state extractors
const selectPatrolState: AppSelector<patrols.PatrolsState> = (state) => state.patrols;
const selectUserState: AppSelector<user.UserState> = (state) => state.user;
const selectDialogState: AppSelector<dialog.DialogState> = (state) => state.dialog;
const selectUiState: AppSelector<ui.UiState> = (state) => state.ui;
const _selectSettingsState: AppSelector<settings.SettingsStateType> = (state) => state.settings;
void _selectSettingsState; // Reserved for future use

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

/**
 * Select the list of user changes for the currently selected section.
 */
export const selectChangesForCurrentSection: AppSelector<Array<UserChange>> =
    createAppSelector([
            selectSelectedSection,
            selectPatrolState,
            selectUiState
        ],
        (section, patrolsState, uiState): UserChange[] =>
            section?.patrols
                .map( (key:string) : UIPatrol|undefined => selectPatrolById(patrolsState,key))
                .filter((p: patrols.UIPatrol | undefined): p is patrols.UIPatrol => p !== undefined)
                .map((p: UIPatrol): UserChange => ({
                patrolId: p.id,
                name: p.name,
                score: ui.selectUserScoreForPatrolKey(uiState, p.key)
            })).filter((s: UserChange): boolean => s.score !== 0) ?? []
    );


// App-level selectors for settings slice
export const selectSettingsForSection = (state: RootState, sectionId: number): settings.SectionSettingsState | null =>
    settings.selectSettingsBySectionId(state.settings, sectionId);
export const selectPatrolColorsForSection = (state: RootState, sectionId: number): Record<string, string> =>
    settings.selectPatrolColorsBySectionId(state.settings, sectionId);
export const selectPatrolsForSettings = (state: RootState, sectionId: number): settings.PatrolInfo[] =>
    settings.selectPatrolsBySectionId(state.settings, sectionId);
export const selectSettingsLoadState = (state: RootState, sectionId: number) =>
    settings.selectSettingsLoadState(state.settings, sectionId);
export const selectIsSavingSettings = (state: RootState, sectionId: number): boolean =>
    settings.selectIsSaving(state.settings, sectionId);
export const selectSettingsSaveError = (state: RootState, sectionId: number): string | undefined =>
    settings.selectSaveError(state.settings, sectionId);