import {
    makeSelectPatrolById,
    selectPatrolColorsForSection,
    selectSelectedSectionId,
    selectUserScoreForPatrolKey,
    setPatrolScore,
    useAppDispatch,
    useAppSelector
} from '../../state';
import {useMemo} from "react";
import {PatrolErrorIcon} from './PatrolErrorIcon';

interface PatrolCardProps {
    patrolId: string;
}

/**
 * Displays a single patrol's score information and input field.
 *
 * Shows:
 * - Patrol name with pending sync badge if applicable
 * - Current committed score
 * - Projected new score (if user has entered points or there are pending points)
 * - Input field for entering points to add
 *
 * The input field clamps values to [-1000, 1000] range.
 */
export function PatrolCard({patrolId}: PatrolCardProps) {
    const selectPatrolById = useMemo(makeSelectPatrolById, []);
    const patrol = useAppSelector(state => selectPatrolById(state, patrolId));
    const userEntry: number = useAppSelector(state => selectUserScoreForPatrolKey(state, patrolId));
    const sectionId = useAppSelector(selectSelectedSectionId);
    const patrolColors = useAppSelector(state =>
        sectionId ? selectPatrolColorsForSection(state, sectionId) : {}
    );
    if (!patrol) return <span>WARN: Patrol id {patrolId} not found in patrol map</span>;

    const colorName = patrolColors[patrol.id];
    const themeClass = colorName ? ` patrol-theme-${colorName}` : '';

    const totalScore = patrol.committedScore + patrol.pendingScore + userEntry;
    const hasNetChange = (userEntry + patrol.pendingScore) !== 0;

    // Show error icon if there's an error message and pending changes (but not if ready to retry)
    const shouldShowErrorIcon =
        patrol.errorMessage &&
        patrol.pendingScore !== 0 &&
        patrol.retryAfter !== 0;

    const dispatch = useAppDispatch();

    function onPointsChange(patrolId: string, value: string) {
        const numericValue = value === '' || value === '-' ? 0 : parseInt(value, 10);
        dispatch(setPatrolScore({key: patrolId, score: numericValue}));
    }

    return (
        <div className={`patrol-card${patrol.pendingScore ? ' has-pending' : ''}${themeClass}`}>
            <div className="patrol-card-header">
        <span className="patrol-name">
          {patrol.name}
            {shouldShowErrorIcon && (
                <PatrolErrorIcon
                    errorMessage={patrol.errorMessage}
                    retryAfter={patrol.retryAfter}
                />
            )}
            {patrol.pendingScore !== 0 && (
                <span className="patrol-pending-badge" title="Pending sync">
              {patrol.pendingScore > 0 ? '+' : ''}{patrol.pendingScore}
            </span>
            )}
        </span>
                <span className="patrol-current-score">{patrol.committedScore}</span>
                {hasNetChange && (
                    <span className="patrol-new-score">
            {totalScore}
          </span>
                )}
            </div>
            <div className="patrol-card-body">
                <div className="patrol-input">
                    <span className="patrol-input-label">Add points:</span>
                    <input
                        type="number"
                        min={-1000}
                        max={1000}
                        value={userEntry === 0 ? '' : userEntry}
                        onChange={e => onPointsChange(patrol.key, e.target.value)}
                        placeholder="0"
                        className={
                            userEntry > 0
                                ? 'positive'
                                : userEntry < 0
                                    ? 'negative'
                                    : ''
                        }
                    />
                </div>
            </div>
        </div>
    );
}
