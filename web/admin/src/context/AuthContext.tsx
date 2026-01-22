import { useState, useEffect, useCallback } from 'react';
import type { ReactNode } from 'react';
import { fetchSession, fetchSections, ApiError } from '../api';
import type { Section } from '../api';
import { AuthContext } from './AuthContextTypes';
import type { AuthState, AuthContextType } from './AuthContextTypes';
import { OpenOfflineStore } from '../offlineStore';

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    isLoading: true,
    isAuthenticated: false,
    user: null,
    csrfToken: null,
    sections: [],
    selectedSectionId: null,
    error: null,
    pendingWrites: 0,
  });
  const [previousSections, setPreviousSections] = useState<Section[]>([]);

  const loadSession = useCallback(async () => {
    try {
      const sessionData = await fetchSession();

      if (!sessionData.authenticated) {
        // Redirect to login page
        window.location.href = '/admin/signin';
        return;
      }

      // Load sections
      const sectionsData = await fetchSections();

      // Determine selected section
      let selectedSectionId = sessionData.selectedSectionId ?? null;
      if (selectedSectionId === null && sectionsData.sections.length > 0) {
        selectedSectionId = sectionsData.sections[0].id;
      }

      setState({
        isLoading: false,
        isAuthenticated: true,
        user: sessionData.user ?? null,
        csrfToken: sessionData.csrfToken ?? null,
        sections: sectionsData.sections,
        selectedSectionId,
        error: null,
        pendingWrites: sessionData.pendingWrites ?? 0,
      });
    } catch (err) {
      // Check if it's an unauthorized error - redirect to login page
      if (err instanceof ApiError && err.statusCode === 401) {
        window.location.href = '/admin/signin';
        return;
      }

      setState(prev => ({
        ...prev,
        isLoading: false,
        error: err instanceof Error ? err.message : 'Failed to load session',
      }));
    }
  }, []);

  useEffect(() => {
    loadSession();
  }, [loadSession]);

  const setSelectedSectionId = useCallback((id: number) => {
    setState(prev => ({ ...prev, selectedSectionId: id }));
  }, []);

  const handleSectionAccessLoss = useCallback(async (sectionId: number) => {
    // Check if there are pending updates for this section
    const store = await OpenOfflineStore();
    const allPending = await store.getAllPending();
    const pendingForSection = allPending.filter(e => e.sectionId === sectionId.toString());

    if (pendingForSection.length > 0) {
      const shouldClear = confirm(
        `You no longer have access to this section, but you have ${pendingForSection.length} pending update(s). ` +
        `Clear these updates? (If you regain access, you may want to keep them)`
      );

      if (shouldClear) {
        for (const entry of pendingForSection) {
          await store.clear(entry.sectionId, entry.patrolId);
        }
      }
    }
  }, []);

  const refreshSections = useCallback(async () => {
    try {
      const sectionsData = await fetchSections();
      const newSections = sectionsData.sections;

      // Compare with previous sections to detect access loss
      if (previousSections.length > 0 && state.selectedSectionId) {
        const hadAccess = previousSections.some(s => s.id === state.selectedSectionId);
        const hasAccess = newSections.some(s => s.id === state.selectedSectionId);

        if (hadAccess && !hasAccess) {
          // Lost access to currently selected section
          await handleSectionAccessLoss(state.selectedSectionId);
        }
      }

      // Store previous sections for next comparison
      setPreviousSections(state.sections);

      // Update sections
      setState(prev => ({ ...prev, sections: newSections }));

      // If selected section no longer exists, switch to first available
      if (state.selectedSectionId) {
        const stillExists = newSections.some(s => s.id === state.selectedSectionId);
        if (!stillExists && newSections.length > 0) {
          setState(prev => ({ ...prev, selectedSectionId: newSections[0].id }));
        }
      }
    } catch (err) {
      setState(prev => ({
        ...prev,
        error: err instanceof Error ? err.message : 'Failed to refresh sections',
      }));
    }
  }, [previousSections, state.sections, state.selectedSectionId, handleSectionAccessLoss]);

  // Refresh sections every 5 minutes to detect access changes
  useEffect(() => {
    if (!state.isAuthenticated) return;

    const interval = setInterval(() => {
      refreshSections().catch(console.error);
    }, 5 * 60 * 1000); // 5 minutes

    return () => clearInterval(interval);
  }, [state.isAuthenticated, refreshSections]);

  const logout = useCallback(() => {
    window.location.href = '/admin/logout';
  }, []);

  const value: AuthContextType = {
    ...state,
    setSelectedSectionId,
    refreshSections,
    logout,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
