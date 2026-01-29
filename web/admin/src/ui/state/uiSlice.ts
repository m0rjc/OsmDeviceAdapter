import {createEntityAdapter, createSlice, type EntityAdapter, type PayloadAction} from "@reduxjs/toolkit";
import {type AppSelector, type RootState} from "./rootReducer.ts";
import {patrolKeyBelongsToSection} from "./patrolsSlice.ts";

export type UserScoreEntry = {
    /** Patrol key */
    key: string;
    /** User's score */
    score: number;
};


const entityAdapter : EntityAdapter<UserScoreEntry, string> = createEntityAdapter<UserScoreEntry,string>({
    selectId: (entry:UserScoreEntry):string => entry.key
});

const initialState = entityAdapter.getInitialState({
    selectedSectionId: null as number | null,
});

const uiSlice = createSlice({
    name: 'ui',
    initialState,
    reducers: {
        setSelectedSectionId: (state, action) => {
            state.selectedSectionId = action.payload;
        },

        setPatrolScore: (state, action: PayloadAction<UserScoreEntry>):void => {
            entityAdapter.upsertOne(state, action.payload)
        },

        /**
         * Clear user score entries for a specific section.
         * Typically called after successfully submitting scores.
         * @param state
         * @param action Payload containing the sectionId to clear entries for
         */
        clearUserEntriesForSection: (state, action: PayloadAction<{sectionId: number}>):void => {
            const {sectionId} = action.payload;
            const allEntries = entityAdapter.getSelectors().selectAll(state);
            const keysToRemove = allEntries
                .filter(entry => patrolKeyBelongsToSection(entry.key, sectionId))
                .map(entry => entry.key);
            entityAdapter.removeMany(state, keysToRemove);
        }
    }
})

export const {setSelectedSectionId, setPatrolScore, clearUserEntriesForSection} = uiSlice.actions;
export type UiState = ReturnType<typeof uiSlice.reducer>;

/**
 * Selects the currently selected section ID.
 * @param state
 */
export const selectSelectedSectionId : AppSelector<number|null> = (state:RootState):number|null => state.ui.selectedSectionId;

/**
 * Selects the user's score for a patrol.
 * @param state root state
 * @param patrolKey patrol key (sectionId:patrolId)
 */
export const selectUserScoreForPatrolKey : (state:RootState, patrolKey: string) => number =
    (state: RootState, patrolKey: string):number => entityAdapter.getSelectors().selectById(state.ui, patrolKey)?.score ?? 0;

export const selectUserScoreForPatrolKeyFromUiState : (state:UiState, patrolKey: string) => number =
    (state : UiState, patrolKey: string):number => entityAdapter.getSelectors().selectById(state, patrolKey)?.score ?? 0;

export default uiSlice.reducer;