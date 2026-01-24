import { useState, useEffect, useCallback } from 'react';
import type { ReactNode } from 'react';
import { fetchSession, fetchSections, ApiError } from '../api';
import { AuthContext } from './AuthContextTypes.ts';
import type { AuthState, AuthContextType } from './AuthContextTypes.ts';

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    isLoading: true,
    isAuthenticated: false,
    user: null,
    csrfToken: null,
    sections: [],
    selectedSectionId: null,
    error: null,
  });

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

  const refreshSections = useCallback(async () => {
    try {
      const sectionsData = await fetchSections();
      setState(prev => ({ ...prev, sections: sectionsData.sections }));
    } catch (err) {
      setState(prev => ({
        ...prev,
        error: err instanceof Error ? err.message : 'Failed to refresh sections',
      }));
    }
  }, []);

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
