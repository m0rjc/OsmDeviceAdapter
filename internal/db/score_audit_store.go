package db

import (
	"time"
)

// CreateScoreAuditLog creates a new score audit log entry
func CreateScoreAuditLog(conns *Connections, log *ScoreAuditLog) error {
	return conns.DB.Create(log).Error
}

// CreateScoreAuditLogs creates multiple score audit log entries in a batch
func CreateScoreAuditLogs(conns *Connections, logs []ScoreAuditLog) error {
	if len(logs) == 0 {
		return nil
	}
	return conns.DB.Create(&logs).Error
}

// DeleteExpiredScoreAuditLogs deletes audit log entries older than the retention period
func DeleteExpiredScoreAuditLogs(conns *Connections, retention time.Duration) error {
	cutoff := time.Now().Add(-retention)
	return conns.DB.Where("created_at < ?", cutoff).Delete(&ScoreAuditLog{}).Error
}
