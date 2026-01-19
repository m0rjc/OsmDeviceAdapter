import { createContext } from 'react';
import type { UserInfo, Section } from '../api';

export interface AuthState {
  isLoading: boolean;
  isAuthenticated: boolean;
  user: UserInfo | null;
  csrfToken: string | null;
  sections: Section[];
  selectedSectionId: number | null;
  error: string | null;
  pendingWrites: number; // Server-side pending writes count
}

export interface AuthContextType extends AuthState {
  setSelectedSectionId: (id: number) => void;
  refreshSections: () => Promise<void>;
  logout: () => void;
}

export const AuthContext = createContext<AuthContextType | null>(null);
