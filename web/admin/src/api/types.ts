// API response types matching the Go backend

export interface SessionResponse {
  authenticated: boolean;
  user?: UserInfo;
  selectedSectionId?: number;
  csrfToken?: string;
}

export interface UserInfo {
  osmUserId: number;
  name: string;
}

export interface SectionsResponse {
  sections: Section[];
}

export interface Section {
  id: number;
  name: string;
  groupName: string;
}

export interface ScoresResponse {
  section: SectionInfo;
  termId: number;
  patrols: Patrol[];
  fetchedAt: string;
}

export interface SectionInfo {
  id: number;
  name: string;
}

export interface Patrol {
  id: string;
  name: string;
  score: number;
}

export interface ScoreUpdate {
  patrolId: string;
  points: number;
}

export interface UpdateRequest {
  updates: ScoreUpdate[];
}

export interface UpdateResponse {
  success: boolean;
  patrols: PatrolResult[];
}

export interface PatrolResult {
  id: string;
  name: string;
  previousScore: number;
  newScore: number;
}

export interface ErrorResponse {
  error: string;
  message: string;
}
