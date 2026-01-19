// API response types matching the Go backend

export interface SessionResponse {
  authenticated: boolean;
  user?: UserInfo;
  selectedSectionId?: number;
  csrfToken?: string;
  pendingWrites?: number; // Server-side pending count
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
  patrols?: PatrolResult[]; // Present on 200 OK
  status?: string; // 'accepted' on 202 Accepted
  batchId?: string; // Batch ID on 202 Accepted
  entriesCreated?: number; // Number of entries created on 202 Accepted
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
