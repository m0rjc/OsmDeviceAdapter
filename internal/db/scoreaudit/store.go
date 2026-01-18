package scoreaudit

import (
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
)

// Create creates a new score audit log entry
func Create(conns *db.Connections, log *db.ScoreAuditLog) error {
	return conns.DB.Create(log).Error
}

// CreateBatch creates multiple score audit log entries in a batch
func CreateBatch(conns *db.Connections, logs []db.ScoreAuditLog) error {
	if len(logs) == 0 {
		return nil
	}
	return conns.DB.Create(&logs).Error
}

// DeleteExpired deletes audit log entries older than the retention period
func DeleteExpired(conns *db.Connections, retention time.Duration) error {
	cutoff := time.Now().Add(-retention)
	return conns.DB.Where("created_at < ?", cutoff).Delete(&db.ScoreAuditLog{}).Error
}
