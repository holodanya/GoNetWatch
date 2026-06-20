package notifier

import "GoNetWatch/internal/models"

// Notifier is an interface for sending notifications about monitor results and lifecycle events
type Notifier interface {
	// OnStateChange sends a notification when a target's state changes (UP/DOWN transition)
	OnStateChange(target models.Target, result models.MonitorResult, isUp bool) error
	// OnStart sends a notification when the monitoring service starts
	OnStart(targetCount int) error
	// OnStop sends a notification when the monitoring service stops
	OnStop() error
}
