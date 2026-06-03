package engines

import "errors"

// ErrWorkloadMissing indicates the materialized engine workload no longer exists.
var ErrWorkloadMissing = errors.New("engine workload not found")

// ErrWorkloadOwnership indicates a name collision with another operation's workload.
var ErrWorkloadOwnership = errors.New("engine workload ownership conflict")

// ErrWorkloadAmbiguous indicates more than one labeled engine workload for an operation.
var ErrWorkloadAmbiguous = errors.New("ambiguous engine workload")
