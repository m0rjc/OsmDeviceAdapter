package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type HealthResponse struct {
	Status string `json:"status"`
}

type ReadyResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Redis    string `json:"redis"`
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
}

func ReadyHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := ReadyResponse{
			Status:   "ready",
			Database: "ok",
			Redis:    "ok",
		}

		// Check database connection
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		sqlDB, err := deps.DB.DB()
		if err != nil || sqlDB.PingContext(ctx) != nil {
			response.Database = "error"
			response.Status = "not ready"
		}

		// Check Redis connection
		if err := deps.RedisClient.Client().Ping(ctx).Err(); err != nil {
			response.Redis = "error"
			response.Status = "not ready"
		}

		w.Header().Set("Content-Type", "application/json")
		if response.Status != "ready" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(response)
	}
}
