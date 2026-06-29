package notifier

import "GoNetWatch/internal/models"

// Notifier defines the interface for sending lifecycle and alert notifications.
type Notifier interface {
	OnStateChange(target models.Target, result models.MonitorResult, isUp bool) error
	OnStart(targetCount int) error
	OnStop() error
}
