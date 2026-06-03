package engines

import "errors"

// ErrWorkloadMissing indicates the materialized engine workload no longer exists.
var ErrWorkloadMissing = errors.New("engine workload not found")
