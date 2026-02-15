import { useCallback, useEffect } from 'react';
import {
  useAppDispatch,
  useAppSelector,
  selectAllScoreboards,
  selectScoreboardsLoadState,
  selectScoreboardsError,
  selectScoreboardsSaving,
  selectSections,
  fetchScoreboards,
  changeScoreboardSection,
} from '../../state';
import { Loading } from '../Loading';
import { MessageCard } from '../MessageCard';
import { useToast } from '../../hooks';

export function ScoreboardSettings() {
  const dispatch = useAppDispatch();
  const { showToast } = useToast();

  const scoreboards = useAppSelector(selectAllScoreboards);
  const loadState = useAppSelector(selectScoreboardsLoadState);
  const error = useAppSelector(selectScoreboardsError);
  const saving = useAppSelector(selectScoreboardsSaving);
  const sections = useAppSelector(selectSections);

  useEffect(() => {
    if (loadState === 'uninitialized') {
      dispatch(fetchScoreboards());
    }
  }, [loadState, dispatch]);

  const handleSectionChange = useCallback(async (deviceCodePrefix: string, sectionIdStr: string) => {
    const sectionId = parseInt(sectionIdStr, 10);
    if (isNaN(sectionId)) return;

    const section = sections.find(s => s.id === sectionId);
    const sectionName = section?.name ?? (sectionId === 0 ? 'Ad-hoc Teams' : `Section ${sectionId}`);

    try {
      await dispatch(changeScoreboardSection({ deviceCodePrefix, sectionId, sectionName })).unwrap();
      showToast('success', 'Scoreboard section updated');
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to update section');
    }
  }, [dispatch, sections, showToast]);

  const handleRefresh = useCallback(() => {
    dispatch(fetchScoreboards());
  }, [dispatch]);

  if (loadState === 'loading') {
    return <Loading message="Loading scoreboards..." />;
  }

  if (loadState === 'error') {
    return (
      <MessageCard
        title="Error Loading Scoreboards"
        message={error || 'Failed to load scoreboards'}
        action={{ label: 'Retry', onClick: handleRefresh }}
      />
    );
  }

  if (scoreboards.length === 0) {
    return (
      <div className="settings-section">
        <h3 className="settings-section-title">Scoreboards</h3>
        <p className="settings-section-description">
          No authorized scoreboards found. Pair a device first using the device authorization flow.
        </p>
      </div>
    );
  }

  return (
    <div className="settings-section">
      <h3 className="settings-section-title">Scoreboards</h3>
      <p className="settings-section-description">
        Change which section each scoreboard device displays.
      </p>

      <div className="scoreboards-list">
        {scoreboards.map((board) => (
          <div key={board.deviceCodePrefix} className="scoreboard-row">
            <div className="scoreboard-info">
              <span className="scoreboard-device-code" title={board.clientId}>
                {board.deviceCodePrefix}...
              </span>
              {board.lastUsedAt && (
                <span className="scoreboard-last-used">
                  Last used: {new Date(board.lastUsedAt).toLocaleDateString()}
                </span>
              )}
            </div>
            <select
              value={board.sectionId ?? ''}
              onChange={(e) => handleSectionChange(board.deviceCodePrefix, e.target.value)}
              disabled={saving}
              className="scoreboard-section-select"
            >
              <option value="" disabled>Select section...</option>
              {sections.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
        ))}
      </div>
    </div>
  );
}
