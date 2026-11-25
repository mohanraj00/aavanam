// Package measure times the calls of the budget bench and writes the result file (docs/design/backend.md,
// section 7). A result is for the machine of its run only.
package measure

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Format is the format name of a result file.
const Format = "aavanam-bench/v1"

// Result is the time of one operation: the count of calls and the percentiles in milliseconds.
type Result struct {
	Name    string  `json:"name"`
	Clients int     `json:"clients"`
	Count   int     `json:"count"`
	P50     float64 `json:"p50_ms"`
	P95     float64 `json:"p95_ms"`
	Max     float64 `json:"max_ms"`
	// Target is the p95 target of the design in milliseconds, or 0 if the design has none.
	Target float64 `json:"target_ms,omitempty"`
	// Known is the issue that tracks a known miss of the target, for example "#128".
	Known string `json:"known_miss,omitempty"`
	// Failed is the count of calls whose status is not `ok`. Their times are in the percentiles.
	Failed int `json:"failed,omitempty"`
	// Failure is the status and the first reason code of the first failed call.
	Failure string `json:"failure,omitempty"`
}

// StatusError is the error of a check for a call that gave a status other than `ok`, for example
// `failed` with `deadline_exceeded`. The measure counts the call as failed and continues. Each other
// error of a call or a check stops the measure.
type StatusError struct {
	Status string
	Code   string
}

func (e *StatusError) Error() string { return e.Status + " " + e.Code }

// Call is one timed call. It returns the result body. The check of the body is not in the time.
type Call func(i int) ([]byte, error)

// Check checks the body of a call.
type Check func(body []byte) error

// MinCalls is the smallest count of a measure that the time limit stops.
const MinCalls = 20

// Run makes n calls, one after the other, and returns their times.
func Run(name string, n int, limit time.Duration, call Call, check Check) (Result, error) {
	return Concurrent(name, 1, n, limit, call, check)
}

// Concurrent makes n calls from the given number of clients at the same time. Each client makes its next
// call when its last call ends. If limit is more than 0, no call starts after that time from the start,
// unless the measure has fewer than MinCalls calls. The count of the result is the count of the calls.
// A call whose check gives a StatusError is a failed call. Its time stays in the percentiles.
func Concurrent(name string, clients, n int, limit time.Duration, call Call, check Check) (Result, error) {
	times := make([]time.Duration, n)
	done := make([]bool, n)
	began := time.Now()
	var next atomic.Int64
	var failure error
	var once sync.Once
	var lock sync.Mutex
	failed, first := 0, ""
	var group sync.WaitGroup
	for range clients {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= n || (limit > 0 && i >= MinCalls && time.Since(began) > limit) {
					return
				}
				start := time.Now()
				body, err := call(i)
				times[i], done[i] = time.Since(start), true
				if err == nil && check != nil {
					err = check(body)
				}
				var status *StatusError
				if errors.As(err, &status) {
					lock.Lock()
					if failed++; first == "" {
						first = status.Error()
					}
					lock.Unlock()
					continue
				}
				if err != nil {
					once.Do(func() { failure = fmt.Errorf("%s, call %d: %w", name, i, err) })
					next.Store(int64(n))
					return
				}
			}
		}()
	}
	group.Wait()
	if failure != nil {
		return Result{}, failure
	}
	var made []time.Duration
	for i, t := range times {
		if done[i] {
			made = append(made, t)
		}
	}
	result := Of(name, clients, made)
	result.Failed, result.Failure = failed, first
	return result, nil
}

// Of returns the result of a list of times.
func Of(name string, clients int, times []time.Duration) Result {
	sorted := append([]time.Duration(nil), times...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return Result{Name: name, Clients: clients, Count: len(sorted), P50: ms(Percentile(sorted, 0.50)),
		P95: ms(Percentile(sorted, 0.95)), Max: ms(Percentile(sorted, 1))}
}

// Percentile returns the nearest-rank percentile of a sorted list: the smallest value that is not less
// than the fraction q of the values.
func Percentile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[min(max(rank, 0), len(sorted)-1)]
}

func ms(d time.Duration) float64 { return math.Round(float64(d.Microseconds())) / 1000 }

// Ms returns a duration in milliseconds, for a measure of one event.
func Ms(d time.Duration) float64 { return ms(d) }

// Machine is the description of the machine of a run.
type Machine struct {
	CPU         string `json:"cpu"`
	CPUs        int    `json:"cpus"`
	MemoryBytes uint64 `json:"memory_bytes"`
	OS          string `json:"os"`
	Go          string `json:"go"`
	PostgreSQL  string `json:"postgresql,omitempty"`
	SQLite      string `json:"sqlite,omitempty"`
}

// Describe returns the description of this machine. A value that the machine does not give is empty.
func Describe() Machine {
	m := Machine{CPUs: runtime.NumCPU(), Go: runtime.Version(), OS: runtime.GOOS + "/" + runtime.GOARCH}
	if out, err := exec.Command("uname", "-sr").Output(); err == nil {
		m.OS = strings.TrimSpace(string(out)) + " " + runtime.GOARCH
	}
	switch runtime.GOOS {
	case "darwin":
		m.CPU = sysctl("machdep.cpu.brand_string")
		m.MemoryBytes, _ = strconv.ParseUint(sysctl("hw.memsize"), 10, 64)
	case "linux":
		m.CPU = procValue("/proc/cpuinfo", "model name")
		if kb, err := strconv.ParseUint(strings.Fields(procValue("/proc/meminfo", "MemTotal") + " 0")[0], 10, 64); err == nil {
			m.MemoryBytes = kb << 10
		}
	}
	return m
}

func sysctl(name string) string {
	out, err := exec.Command("sysctl", "-n", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func procValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Run of one storage: the store, the build time, the measures and the sizes.
type StorageRun struct {
	Storage  string             `json:"storage"`
	Store    any                `json:"store"`
	Measures []Result           `json:"measures"`
	Values   map[string]int64   `json:"values,omitempty"`   // sizes in bytes and counts
	Times    map[string]float64 `json:"times_ms,omitempty"` // the time of one event, for example a build
}

// Report is the content of a result file.
type Report struct {
	Format  string            `json:"format"`
	Date    string            `json:"date"`
	Note    string            `json:"note"`
	Machine Machine           `json:"machine"`
	Shape   string            `json:"shape"`
	Calls   int               `json:"calls"`
	Factor  float64           `json:"check_factor"`
	Limit   float64           `json:"time_limit_s,omitempty"`
	Known   map[string]string `json:"known_misses,omitempty"`
	Runs    []StorageRun      `json:"runs"`
}

// NewReport returns a report with the date, the machine and the note of a result.
func NewReport(machine Machine, shape string, calls int, factor float64) *Report {
	return &Report{Format: Format, Date: time.Now().UTC().Format(time.RFC3339), Machine: machine, Shape: shape,
		Calls: calls, Factor: factor,
		Note: "Each value is for the machine of this run only. It is not a claim for a different machine."}
}

// Write writes the report as JSON to a file.
func (r *Report) Write(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Print writes a table of the measures.
func (r *Report) Print(w io.Writer) {
	for _, run := range r.Runs {
		fmt.Fprintf(w, "%s on %s\n", r.Shape, run.Storage)
		for _, m := range run.Measures {
			target := "-"
			if m.Target > 0 {
				target = fmt.Sprintf("%g ms", m.Target)
			}
			if m.Known != "" {
				target += ", known miss " + m.Known
			}
			if m.Failed > 0 {
				target += fmt.Sprintf(", failed %d (%s)", m.Failed, m.Failure)
			}
			fmt.Fprintf(w, "  %-36s clients %2d  count %5d  p50 %9.3f ms  p95 %9.3f ms  max %9.3f ms  target %s\n",
				m.Name, m.Clients, m.Count, m.P50, m.P95, m.Max, target)
		}
		for _, k := range sortedKeys(run.Times) {
			fmt.Fprintf(w, "  %-36s %12.3f ms\n", k, run.Times[k])
		}
		for _, k := range sortedKeys(run.Values) {
			fmt.Fprintf(w, "  %-36s %12d\n", k, run.Values[k])
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// MarkKnown records the issue of each known miss. A key is `storage/measure`, for example
// `pg/open_context`.
func (r *Report) MarkKnown(known map[string]string) {
	for i := range r.Runs {
		for j := range r.Runs[i].Measures {
			m := &r.Runs[i].Measures[j]
			if issue, ok := known[r.Runs[i].Storage+"/"+m.Name]; ok && m.Target > 0 && m.P95 > m.Target {
				m.Known = issue
			}
		}
	}
}

// ParseKnown reads a list of known misses: `storage/measure=issue`, separated by commas.
func ParseKnown(list string) (map[string]string, error) {
	out := map[string]string{}
	for _, entry := range strings.Split(list, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		key, issue, ok := strings.Cut(entry, "=")
		if !ok || !strings.Contains(key, "/") || issue == "" {
			return nil, fmt.Errorf("a known miss is storage/measure=issue, not %q", entry)
		}
		out[key] = issue
	}
	return out, nil
}

// Misses returns each measure with failed calls, and each measure with a target whose p95 is more than
// factor times the target and that is not a known miss. A known miss does not excuse a failed call.
func (r *Report) Misses() []string {
	var out []string
	for _, run := range r.Runs {
		for _, m := range run.Measures {
			if m.Failed > 0 {
				out = append(out, fmt.Sprintf("%s on %s: %s has %d failed calls, the first %s",
					r.Shape, run.Storage, m.Name, m.Failed, m.Failure))
				continue
			}
			if m.Target > 0 && m.P95 > r.Factor*m.Target && m.Known == "" {
				out = append(out, fmt.Sprintf("%s on %s: %s p95 %.3f ms is more than %g times the target %g ms",
					r.Shape, run.Storage, m.Name, m.P95, r.Factor, m.Target))
			}
		}
	}
	return out
}

// The names of the measures with a target in the design.
const (
	OpenContext  = "open_context"
	ReadRow      = "read_row"
	ReadArtifact = "read_artifact_64k"
	SearchText   = "search_text"
)

// targets has the p95 targets of docs/design/backend.md, section 7, in milliseconds. The `S1` column is
// for one client on a server. The `S0` column is for the device, and the bench also applies it to the
// `S0` runs on the storage in memory and on PostgreSQL.
var targets = map[string]map[string]float64{
	"S1": {OpenContext: 25, ReadRow: 10, ReadArtifact: 10, SearchText: 100},
	"S0": {OpenContext: 25, ReadRow: 5, ReadArtifact: 5, SearchText: 100},
}

// Target returns the p95 target of a measure with one client on a shape, or 0.
func Target(shape, name string) float64 { return targets[shape][name] }
