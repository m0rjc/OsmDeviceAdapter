import { useEffect, useCallback } from 'react';
import {
  useAppDispatch,
  useAppSelector,
  selectSelectedSection,
  selectSettingsForSection,
  fetchSectionSettings,
  saveSectionSettings,
  updatePatrolColor,
} from '../../state';
import { Loading } from '../../../legacy/components';
import { MessageCard } from '../MessageCard';
import { PatrolColorList } from './PatrolColorList';
import { useToast } from '../../hooks';

/**
 * Settings page for configuring section-specific settings like patrol colors.
 */
export function SettingsPage() {
  const dispatch = useAppDispatch();
  const { showToast } = useToast();

  const selectedSection = useAppSelector(selectSelectedSection);
  const sectionId = selectedSection?.id;
  const settings = useAppSelector((state) =>
    sectionId !== undefined ? selectSettingsForSection(state, sectionId) : null
  );

  // Fetch settings when section changes
  useEffect(() => {
    if (sectionId !== undefined && settings?.loadState === 'uninitialized') {
      dispatch(fetchSectionSettings(sectionId));
    }
  }, [sectionId, settings?.loadState, dispatch]);

  // Handle color change
  const handleColorChange = useCallback(
    (patrolId: string, color: string | null) => {
      if (sectionId === undefined) return;

      // Optimistic update
      dispatch(updatePatrolColor({ sectionId, patrolId, color: color ?? '' }));
    },
    [sectionId, dispatch]
  );

  // Handle save
  const handleSave = useCallback(async () => {
    if (sectionId === undefined || !settings) return;

    try {
      await dispatch(saveSectionSettings({
        sectionId,
        patrolColors: settings.patrolColors,
      })).unwrap();
      showToast('success', 'Settings saved');
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to save settings';
      showToast('error', message);
    }
  }, [sectionId, settings, dispatch, showToast]);

  // Handle refresh
  const handleRefresh = useCallback(() => {
    if (sectionId === undefined) return;
    dispatch(fetchSectionSettings(sectionId));
  }, [sectionId, dispatch]);

  // Render empty states
  if (!selectedSection) {
    return (
      <MessageCard
        title="No Section Selected"
        message="Please select a section to configure settings."
      />
    );
  }

  // Loading state
  if (!settings || settings.loadState === 'loading') {
    return <Loading message="Loading settings..." />;
  }

  // Error state
  if (settings.loadState === 'error') {
    return (
      <MessageCard
        title="Error Loading Settings"
        message={settings.error || 'Failed to load settings'}
        action={{
          label: 'Retry',
          onClick: handleRefresh,
        }}
      />
    );
  }

  return (
    <div className="settings-card">
      <div className="settings-header">
        <h2>Settings: {selectedSection.name}</h2>
      </div>

      <div className="settings-section">
        <h3 className="settings-section-title">Patrol Colors</h3>
        <p className="settings-section-description">
          Configure colors for each patrol. Colors are sent to devices to customize the bargraph display.
        </p>

        <PatrolColorList
          patrols={settings.patrols}
          patrolColors={settings.patrolColors}
          onChange={handleColorChange}
          disabled={settings.saving}
        />
      </div>

      <div className="action-bar">
        <button
          className="btn btn-secondary"
          onClick={handleRefresh}
          disabled={settings.saving}
        >
          Refresh
        </button>
        <button
          className="btn btn-primary"
          onClick={handleSave}
          disabled={settings.saving}
        >
          {settings.saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>

      {settings.saveError && (
        <div className="error-message" style={{ margin: '1rem 1.5rem' }}>
          {settings.saveError}
        </div>
      )}
    </div>
  );
}
