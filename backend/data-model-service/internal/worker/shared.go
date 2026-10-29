package worker

import (
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
)

// Keep the owner API stable while all owners share one delivery supervisor and
// bounded poller. These mechanics do not own SQL, authorization or job outcomes.
type LeaseSupervisor = workers.LeaseSupervisor
type DeliveryPoller = workers.DeliveryPoller
type DeliveryTask = workers.DeliveryTask
type RiverDeliveryRetryPolicy = workers.RiverDeliveryRetryPolicy
type DeliveryMaintenance = workers.MaintenanceScheduler[ports.DeliveryMaintenanceCursor]

var _ ports.DeliveryRetryPolicy = RiverDeliveryRetryPolicy{}

func NewLeaseSupervisor(lease, interval, heartbeatTimeout, workTimeout time.Duration) (*LeaseSupervisor, error) {
	return workers.NewLeaseSupervisor(lease, interval, heartbeatTimeout, workTimeout)
}

func NewDeliveryPoller(concurrency int, pollInterval, claimTimeout time.Duration, supervisor *LeaseSupervisor) (*DeliveryPoller, error) {
	return workers.NewDeliveryPoller(concurrency, pollInterval, claimTimeout, supervisor)
}

func NewDeliveryMaintenance(pageSize, pagesPerStream int, interval, pageTimeout time.Duration) (*DeliveryMaintenance, error) {
	return workers.NewMaintenanceScheduler[ports.DeliveryMaintenanceCursor](pageSize, pagesPerStream, interval, pageTimeout)
}
