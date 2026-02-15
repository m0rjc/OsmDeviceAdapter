import { useCallback, useEffect, useState } from 'react';
import {
  useAppDispatch,
  useAppSelector,
  selectAllTeams,
  selectTeamsLoadState,
  selectTeamsError,
  selectTeamsSaving,
  fetchTeams,
  createTeam,
  updateTeam,
  deleteTeam,
  resetTeamScores,
} from '../../state';
import { Loading } from '../Loading';
import { MessageCard } from '../MessageCard';
import { TeamRow } from './TeamRow';
import { useToast } from '../../hooks';

export function TeamsPage() {
  const dispatch = useAppDispatch();
  const { showToast } = useToast();

  const teams = useAppSelector(selectAllTeams);
  const loadState = useAppSelector(selectTeamsLoadState);
  const error = useAppSelector(selectTeamsError);
  const saving = useAppSelector(selectTeamsSaving);
  const [showResetConfirm, setShowResetConfirm] = useState(false);

  // Fetch teams on mount if not loaded
  useEffect(() => {
    if (loadState === 'uninitialized') {
      dispatch(fetchTeams());
    }
  }, [loadState, dispatch]);

  const handleCreate = useCallback(async () => {
    try {
      await dispatch(createTeam({ name: 'New Team', color: '' })).unwrap();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to create team');
    }
  }, [dispatch, showToast]);

  const handleUpdate = useCallback(async (id: string, name: string, color: string) => {
    try {
      await dispatch(updateTeam({ id, name, color })).unwrap();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to update team');
    }
  }, [dispatch, showToast]);

  const handleDelete = useCallback(async (id: string) => {
    try {
      await dispatch(deleteTeam(id)).unwrap();
      showToast('success', 'Team deleted');
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to delete team');
    }
  }, [dispatch, showToast]);

  const handleResetScores = useCallback(async () => {
    setShowResetConfirm(false);
    try {
      await dispatch(resetTeamScores()).unwrap();
      showToast('success', 'All scores reset to 0');
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to reset scores');
    }
  }, [dispatch, showToast]);

  const handleRefresh = useCallback(() => {
    dispatch(fetchTeams());
  }, [dispatch]);

  if (loadState === 'loading') {
    return <Loading message="Loading teams..." />;
  }

  if (loadState === 'error') {
    return (
      <MessageCard
        title="Error Loading Teams"
        message={error || 'Failed to load teams'}
        action={{ label: 'Retry', onClick: handleRefresh }}
      />
    );
  }

  return (
    <div className="settings-card">
      <div className="settings-header">
        <h2>Ad-hoc Teams</h2>
      </div>

      <div className="settings-section">
        <p className="settings-section-description">
          Create temporary teams for games. These teams are stored locally and are not part of OSM.
          The scoreboard will display these teams when set to "Ad-hoc Teams".
        </p>

        {teams.length === 0 ? (
          <div className="teams-empty">
            <p>No teams created yet. Click "Add Team" to get started.</p>
          </div>
        ) : (
          <div className="teams-list">
            {teams.map((team) => (
              <TeamRow
                key={team.id}
                team={team}
                onUpdate={handleUpdate}
                onDelete={handleDelete}
                disabled={saving}
              />
            ))}
          </div>
        )}
      </div>

      <div className="action-bar">
        <button
          className="btn btn-secondary"
          onClick={() => setShowResetConfirm(true)}
          disabled={saving || teams.length === 0}
        >
          Reset Scores
        </button>
        <button
          className="btn btn-primary"
          onClick={handleCreate}
          disabled={saving}
        >
          Add Team
        </button>
      </div>

      {showResetConfirm && (
        <div className="modal-overlay" onClick={() => setShowResetConfirm(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Reset All Scores</h3>
            </div>
            <div className="modal-body">
              <p>Are you sure you want to reset all team scores to 0? This cannot be undone.</p>
            </div>
            <div className="modal-footer">
              <button className="btn btn-secondary" onClick={() => setShowResetConfirm(false)}>Cancel</button>
              <button className="btn btn-danger" onClick={handleResetScores}>Reset Scores</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
