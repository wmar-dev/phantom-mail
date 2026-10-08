package bench

import "os"

// strict is true under "make bench", which runs this package alone on an
// otherwise idle machine, so the plan's numbers apply exactly. In a normal
// "go test ./..." other packages compete for CPU and disk, so the same gates
// run with relaxed limits that still catch order-of-magnitude regressions;
// the measurements are logged either way.
func strict() bool { return os.Getenv("PM_PERF_STRICT") != "" && !raceEnabled }

// scale relaxes a time limit when the gates are not strict.
func scale(strictLimit float64) float64 {
	if strict() {
		return strictLimit
	}
	return strictLimit * 5
}
