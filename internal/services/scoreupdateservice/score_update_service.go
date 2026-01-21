package scoreupdateservice

import (
	"context"
	"errors"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

type ScoreUpdateService struct {
	osmClient *osm.Client
	conns     *db.Connections
}

func New(osmClient *osm.Client, conns *db.Connections) *ScoreUpdateService {
	return &ScoreUpdateService{osmClient: osmClient, conns: conns}
}

type UpdateRequest struct {
	PatrolID string
	Delta    int
}

type UpdateResponse struct {
	PatrolID         string
	PatrolName       string
	Success          bool
	IsTemporaryError *bool
	RetryAfter       *time.Time
	ErrorMessage     *string
	PreviousScore    *int
	NewScore         *int
}

func (srv *ScoreUpdateService) UpdateScores(ctx context.Context, user types.User, sectionId int, requests []UpdateRequest) ([]UpdateResponse, error) {
	profile, err := srv.osmClient.FetchOSMProfile(ctx, user)
	if err != nil {
		return nil, err
	}

	term, err := profile.Data.GetCurrentTermForSection(sectionId)
	if err != nil {
		return nil, err
	}

	userId := profile.Data.UserID
	locks := NewPatrolLockSet(srv.conns.Redis, userId, 60*time.Second)
	for _, request := range requests {
		locks.AddPatrol(sectionId, request.PatrolID)
	}
	err = locks.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer locks.Release(ctx)

	currentScores, _, err := srv.osmClient.FetchPatrolScores(ctx, user, sectionId, term.TermID)
	if err != nil {
		return nil, err
	}

	results := make([]UpdateResponse, len(currentScores))
	for i, request := range requests {
		currentScore := findPatrolScore(currentScores, request.PatrolID)
		if currentScore == nil {
			results[i] = newPatrolNotFoundResponse(&request)
			// TODO: Log error
			continue
		}

		if !locks.IsHeld(sectionId, request.PatrolID) {
			results[i] = newLockedUpdateResponse(request, currentScore)
			// TODO: Instrument and log collision
			continue
		}

		newScore := currentScore.Score + request.Delta
		err = srv.osmClient.UpdatePatrolScore(ctx, user, sectionId, request.PatrolID, newScore)
		if err != nil {
			modelResponse := newOsmErrorUpdateResponse(sectionId, &request, currentScore, err)
			abandonRemainingWork(requests, results, currentScores, i, modelResponse)
			// TODO: Logging
			break
		}
	}
	return results, nil
}

func newPatrolNotFoundResponse(request *UpdateRequest) UpdateResponse {
	return UpdateResponse{
		PatrolID:         request.PatrolID,
		Success:          false,
		IsTemporaryError: toPtr(false),
		ErrorMessage:     toPtr("Patrol not found"),
	}
}

func newLockedUpdateResponse(request UpdateRequest, currentScore *types.PatrolScore) UpdateResponse {
	return UpdateResponse{
		PatrolID:         request.PatrolID,
		PatrolName:       currentScore.Name,
		Success:          false,
		IsTemporaryError: toPtr(true),
		RetryAfter:       toPtr(time.Now().Add(30 * time.Second)),
		ErrorMessage:     toPtr("Patrol is being updated by another user. Please try again later."),
		PreviousScore:    toPtr(currentScore.Score),
		NewScore:         toPtr(currentScore.Score),
	}
}

func newOsmErrorUpdateResponse(sectionId int, request *UpdateRequest, currentScore *types.PatrolScore, err error) *UpdateResponse {
	response := UpdateResponse{
		PatrolID:         request.PatrolID,
		PatrolName:       currentScore.Name,
		Success:          false,
		IsTemporaryError: toPtr(true),
		RetryAfter:       toPtr(time.Now().Add(60 * time.Second)),
		ErrorMessage:     toPtr(err.Error()),
		PreviousScore:    toPtr(currentScore.Score),
		NewScore:         toPtr(currentScore.Score),
	}
	var userBlock *osm.ErrUserBlocked
	if errors.As(err, &userBlock) {
		response.RetryAfter = toPtr(userBlock.BlockedUntil)
	} else if errors.Is(err, osm.ErrServiceBlocked) {
		response.RetryAfter = toPtr(time.Now().Add(6 * time.Hour)) // TODO: Decide how long to back off on these
	} else if errors.Is(err, osm.ErrUnauthorized) {
		response.IsTemporaryError = toPtr(false)
	}
	return &response
}

func abandonRemainingWork(requests []UpdateRequest, results []UpdateResponse, currentScores []types.PatrolScore, abandonFromIndex int, modelResponse *UpdateResponse) {
	for i := abandonFromIndex; i < len(results); i++ {
		currentScore := findPatrolScore(currentScores, requests[i].PatrolID)
		if currentScore == nil {
			results[i] = newPatrolNotFoundResponse(&requests[i])
		} else {
			results[i] = UpdateResponse{
				PatrolID:         requests[i].PatrolID,
				PatrolName:       currentScore.Name,
				Success:          modelResponse.Success,
				IsTemporaryError: modelResponse.IsTemporaryError,
				ErrorMessage:     modelResponse.ErrorMessage,
				RetryAfter:       modelResponse.RetryAfter,
				PreviousScore:    toPtr(currentScore.Score),
				NewScore:         toPtr(currentScore.Score),
			}
		}
	}
}

func findPatrolScore(scores []types.PatrolScore, patrolId string) *types.PatrolScore {
	for _, score := range scores {
		if score.ID == patrolId {
			return &score
		}
	}
	return nil
}

func toPtr[T any](value T) *T {
	return &value
}
