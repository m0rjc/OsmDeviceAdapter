package patrolscoreservice

import "time"

// calculateCacheTTL calculates the cache TTL based on absolute rate limit remaining count.
// Uses adaptive caching strategy with absolute thresholds:
// - > 500 remaining: 1 minute (fresh data when capacity available)
// - 200-500: 5 minutes (baseline)
// - 100-200: 10 minutes (starting to conserve)
// - 50-100: 15 minutes (more conservative)
// - < 50: 30 minutes (very conservative)
func (s *PatrolScoreService) calculateCacheTTL(remaining int) time.Duration {
	switch {
	case remaining > 500:
		return 1 * time.Minute
	case remaining >= 200:
		return 5 * time.Minute
	case remaining >= 100:
		return 10 * time.Minute
	case remaining >= 50:
		return 15 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// determineRateLimitState determines the rate limit state based on remaining requests.
// This is used for reporting in the API response.
func (s *PatrolScoreService) determineRateLimitState(remaining int) RateLimitState {
	if remaining >= 500 {
		return RateLimitStateNone
	}
	return RateLimitStateDegraded
}
