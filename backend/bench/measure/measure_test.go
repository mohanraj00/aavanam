package measure

import (
	"errors"
	"testing"
	"time"
)

func TestPercentileIsTheNearestRank(t *testing.T) {
	var times []time.Duration
	for i := 1; i <= 20; i++ {
		times = append(times, time.Duration(i)*time.Millisecond)
	}
	for q, want := range map[float64]time.Duration{0.5: 10 * time.Millisecond, 0.95: 19 * time.Millisecond, 1: 20 * time.Millisecond} {
		if got := Percentile(times, q); got != want {
			t.Fatalf("percentile %g is %v, not %v", q, got, want)
		}
	}
	if Percentile(nil, 0.5) != 0 {
		t.Fatal("the percentile of no values is not 0")
	}
}

func TestConcurrentCountsEachCallAndStopsAtAFailure(t *testing.T) {
	r, err := Concurrent("x", 4, 100, 0, func(int) ([]byte, error) { return nil, nil }, nil)
	if err != nil || r.Count != 100 || r.Clients != 4 {
		t.Fatalf("result %+v, error %v", r, err)
	}
	_, err = Run("x", 10, 0, func(i int) ([]byte, error) { return nil, nil },
		func([]byte) error { return errors.New("bad body") })
	if err == nil {
		t.Fatal("a failed check gave no error")
	}
}

func TestAStatusOtherThanOkIsAFailedCall(t *testing.T) {
	r, err := Run("x", 10, 0, func(i int) ([]byte, error) { return nil, nil }, func([]byte) error {
		return &StatusError{Status: "failed", Code: "deadline_exceeded"}
	})
	if err != nil || r.Count != 10 || r.Failed != 10 || r.Failure != "failed deadline_exceeded" {
		t.Fatalf("result %+v, error %v", r, err)
	}
	report := &Report{Shape: "S0", Factor: 3, Runs: []StorageRun{{Storage: "pg", Measures: []Result{
		{Name: OpenContext, P95: 1, Target: 25, Known: "#128", Failed: 1, Failure: "failed deadline_exceeded"}}}}}
	if misses := report.Misses(); len(misses) != 1 {
		t.Fatalf("misses %v, not one", misses)
	}
}

func TestMissesUseTheFactor(t *testing.T) {
	r := &Report{Shape: "S0", Factor: 3, Runs: []StorageRun{{Storage: "mem", Measures: []Result{
		{Name: ReadRow, P95: 14, Target: 5}, {Name: SearchText, P95: 301, Target: 100}, {Name: "other", P95: 1000}}}}}
	if misses := r.Misses(); len(misses) != 1 {
		t.Fatalf("misses %v, not one", misses)
	}
}

func TestLimitStopsAfterTheMinimumCount(t *testing.T) {
	r, err := Run("x", 1000, time.Nanosecond, func(int) ([]byte, error) { time.Sleep(time.Microsecond); return nil, nil }, nil)
	if err != nil || r.Count < MinCalls || r.Count >= 1000 {
		t.Fatalf("result %+v, error %v", r, err)
	}
}

func TestKnownMissesDoNotFail(t *testing.T) {
	known, err := ParseKnown("pg/read_row=#128")
	if err != nil {
		t.Fatal(err)
	}
	r := &Report{Shape: "S0", Factor: 3, Runs: []StorageRun{
		{Storage: "pg", Measures: []Result{{Name: ReadRow, P95: 50, Target: 5}}},
		{Storage: "mem", Measures: []Result{{Name: ReadRow, P95: 50, Target: 5}}}}}
	r.MarkKnown(known)
	if misses := r.Misses(); len(misses) != 1 || r.Runs[0].Measures[0].Known != "#128" {
		t.Fatalf("misses %v", misses)
	}
	if _, err := ParseKnown("read_row"); err == nil {
		t.Fatal("a known miss with no storage and no issue gave no error")
	}
}
