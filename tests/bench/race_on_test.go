//go:build race

package bench

// The race detector slows everything several times over, so performance gates
// use a lower floor under it. Run "make bench" or "go test" without -race for real numbers.
const raceEnabled = true
