package ledger

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// toy schema: a delta adds Wins to the record under K.
type delta struct {
	K    string `json:"k"`
	Wins int    `json:"w"`
}

type record struct {
	Wins int `json:"wins"`
}

type toy struct{}

func (toy) Key(d delta) string                         { return d.K }
func (toy) Fold(r record, d delta, _ time.Time) record { r.Wins += d.Wins; return r }
func (toy) Empty(d delta) bool                         { return d.Wins == 0 }

type store = Store[delta, record]

var t0 = time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func open(t *testing.T, dir string, c *clock, o Options) *store {
	t.Helper()
	o.Now = c.Now
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler) // failure-path tests log on purpose
	}
	s, err := Open[delta, record](dir, toy{}, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func wins(s *store, k string) (int, bool) {
	r, _, ok := s.Get(k)
	return r.Wins, ok
}

func do(s *store, f func(*store)) {
	s.call(func(s *store) (bool, error) { f(s); return false, nil })
}

func TestRecordGetView(t *testing.T) {
	c := &clock{t0}
	s := open(t, t.TempDir(), c, Options{})
	defer s.Close()
	s.Record(delta{"aa", 3})
	s.Record(delta{"bb", 5})
	c.now = t0.Add(time.Hour)
	s.Record(delta{"aa", 1})
	s.Record(delta{"", 9})   // no key: not recorded
	s.Record(delta{"cc", 0}) // empty: not recorded
	r, seen, ok := s.Get("aa")
	if !ok || r.Wins != 4 || !seen.Equal(t0.Add(time.Hour)) {
		t.Fatalf("get %+v %v %v", r, seen, ok)
	}
	var n, sum int
	s.View(func(all map[string]Entry[record]) {
		n = len(all)
		for _, e := range all {
			sum += e.R.Wins
		}
	})
	if n != 2 || sum != 9 {
		t.Fatalf("view: %d entries, %d wins", n, sum)
	}
	if _, ok := wins(s, "zz"); ok {
		t.Fatal("unknown key")
	}
}

func TestReopenKeepsEverythingAndPerms(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	s := open(t, dir, c, Options{})
	s.Record(delta{"aa", 2})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"snapshot.json", "journal.jsonl", "stats.lock"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, err, st)
		}
	}
	s = open(t, dir, c, Options{})
	defer s.Close()
	if w, _ := wins(s, "aa"); w != 2 {
		t.Fatalf("after reopen %d", w)
	}
}

func TestRecoverTruncatedJournal(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	s := open(t, dir, c, Options{})
	s.Record(delta{"aa", 1})
	s.Record(delta{"aa", 1})
	s.crash() // power loss: journal flushed, no snapshot
	f, _ := os.OpenFile(filepath.Join(dir, journalName), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"seq":3,"at":1,"d":{"k":"aa","w":`)
	f.Close()
	s = open(t, dir, c, Options{})
	if w, _ := wins(s, "aa"); w != 2 {
		t.Fatalf("recovered %d", w)
	}
	s.Record(delta{"aa", 1}) // written after the torn line
	s.crash()
	s = open(t, dir, c, Options{})
	defer s.Close()
	if w, _ := wins(s, "aa"); w != 3 {
		t.Fatalf("the record after a torn line was lost: %d", w)
	}
}

func TestNoDoubleCountAfterCrashBetweenRenameAndTruncate(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	s := open(t, dir, c, Options{})
	s.Record(delta{"aa", 3})
	s.snapshotOnly() // snapshot renamed into place, journal not yet truncated
	s.crash()
	s = open(t, dir, c, Options{})
	defer s.Close()
	if w, _ := wins(s, "aa"); w != 3 {
		t.Fatalf("double counted: %d", w)
	}
}

func TestCompactionEmptiesJournalAndKeepsState(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	s := open(t, dir, c, Options{})
	s.Record(delta{"aa", 2})
	s.compactNow()
	if st, _ := os.Stat(filepath.Join(dir, journalName)); st.Size() != 0 {
		t.Fatalf("journal not emptied: %d", st.Size())
	}
	s.Record(delta{"aa", 1}) // lands in the fresh journal
	s.crash()
	s = open(t, dir, c, Options{})
	defer s.Close()
	if w, _ := wins(s, "aa"); w != 3 {
		t.Fatalf("snapshot + journal: %d", w)
	}
}

func TestJournalRolloverCompacts(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	s := open(t, dir, c, Options{MaxJournal: 200})
	defer s.Close()
	for range 20 {
		s.Record(delta{"aa", 1})
	}
	var n int
	do(s, func(s *store) { n = s.attempts })
	st, _ := os.Stat(filepath.Join(dir, journalName))
	if n == 0 || st.Size() > 400 {
		t.Fatalf("no rollover: attempts %d, journal %d", n, st.Size())
	}
	if w, _ := wins(s, "aa"); w != 20 {
		t.Fatalf("wins %d", w)
	}
}

func TestCapAndPrune(t *testing.T) {
	c := &clock{t0}
	s := open(t, t.TempDir(), c, Options{MaxKeys: 2, MaxIdle: 24 * time.Hour})
	defer s.Close()
	s.Record(delta{"old", 1})
	c.now = t0.Add(time.Hour)
	s.Record(delta{"mid", 1})
	c.now = t0.Add(2 * time.Hour)
	s.Record(delta{"new", 1})
	if _, ok := wins(s, "old"); ok {
		t.Fatal("cap evicts the least recently seen")
	}
	c.now = t0.Add(25*time.Hour + 30*time.Minute) // cut-off t0+1h30: "mid" goes, "new" stays
	s.compactNow()
	if _, ok := wins(s, "mid"); ok {
		t.Fatal("idle keys are pruned at compaction")
	}
	if _, ok := wins(s, "new"); !ok {
		t.Fatal("recent key kept")
	}
}

func TestEvictionRunsInBatches(t *testing.T) {
	c := &clock{t0}
	s := open(t, t.TempDir(), c, Options{MaxKeys: 500})
	defer s.Close()
	for i := range 501 {
		c.now = t0.Add(time.Duration(i) * time.Second)
		s.Record(delta{string(rune('A'+i%26)) + strings.Repeat("x", i/26), 1})
	}
	var n int
	do(s, func(s *store) { n = len(s.entries) })
	if n != 500-500/evictDivisor+1 {
		t.Fatalf("keys after one batch eviction: %d", n)
	}
}

func TestRecordNeverBlocks(t *testing.T) {
	s := &store{in: make(chan any, 1)}
	if !s.Record(delta{"a", 1}) || s.Record(delta{"b", 1}) || s.Dropped() != 1 {
		t.Fatal("a full buffer drops and counts")
	}
}

func TestWriteFailureIsNotSticky(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	s := open(t, dir, c, Options{})
	do(s, func(s *store) { s.journal.Close() }) // every write now fails (EBADF)
	s.Record(delta{"aa", 1})
	s.Record(delta{"aa", 1})
	if _, ok := wins(s, "aa"); ok || s.Dropped() != 2 {
		t.Fatalf("failed writes must drop and count: dropped %d", s.Dropped())
	}
	do(s, func(s *store) {
		f, err := os.OpenFile(filepath.Join(dir, journalName), os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Error(err)
			return
		}
		s.journal = f
		s.w.Reset(f)
	})
	s.Record(delta{"aa", 2})
	if w, _ := wins(s, "aa"); w != 2 {
		t.Fatalf("writes did not recover: %d", w)
	}
	s.crash()
	s = open(t, dir, c, Options{})
	defer s.Close()
	if w, _ := wins(s, "aa"); w != 2 {
		t.Fatalf("after reopen %d", w)
	}
}

func TestCompactionBackoffAndJournalCap(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	const maxJ = 300
	s := open(t, dir, c, Options{MaxJournal: maxJ})
	defer s.Close()
	do(s, func(s *store) { s.dir = filepath.Join(dir, "missing") }) // snapshots now fail
	attempts := func() (n int) { do(s, func(s *store) { n = s.attempts }); return }
	for range 10 {
		s.Record(delta{"aa", 1})
	}
	if a := attempts(); a != 1 {
		t.Fatalf("attempts within a minute of a failure: %d", a)
	}
	c.now = c.now.Add(61 * time.Second)
	s.Record(delta{"aa", 1})
	if a := attempts(); a != 2 {
		t.Fatalf("attempts after a minute: %d", a)
	}
	for range 40 {
		s.Record(delta{"aa", 1})
	}
	do(s, func(*store) {}) // drain the queue
	st, _ := os.Stat(filepath.Join(dir, journalName))
	if s.Dropped() == 0 || st.Size() > 4*maxJ+maxLine {
		t.Fatalf("journal must stop growing: size %d dropped %d", st.Size(), s.Dropped())
	}
	do(s, func(s *store) { s.dir = dir })
	c.now = c.now.Add(61 * time.Second)
	s.Record(delta{"aa", 1})
	w, _ := wins(s, "aa")
	if st, _ := os.Stat(filepath.Join(dir, journalName)); st.Size() > maxLine || w != 52-int(s.Dropped()) {
		t.Fatalf("recovery: journal %d, wins %d, dropped %d", st.Size(), w, s.Dropped())
	}
}

func TestDataDirTightenedTo0700(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir, &clock{t0}, Options{})
	defer s.Close()
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode().Perm())
	}
}

func TestOverlongLineDropped(t *testing.T) {
	s := open(t, t.TempDir(), &clock{t0}, Options{})
	defer s.Close()
	s.Record(delta{strings.Repeat("x", maxLine), 1})
	if _, ok := wins(s, strings.Repeat("x", maxLine)); ok || s.Dropped() != 1 {
		t.Fatalf("over-long line kept, dropped %d", s.Dropped())
	}
}

func TestReplaySkipsGarbageLines(t *testing.T) {
	dir := t.TempDir()
	junk := strings.Repeat("z", 3*maxLine) + "\n" + "{not json}\n" +
		`{"seq":5,"at":1,"d":{"k":"aa","w":4}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, journalName), []byte(junk), 0o600); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir, &clock{t0}, Options{})
	defer s.Close()
	if w, _ := wins(s, "aa"); w != 4 {
		t.Fatalf("line after garbage lost: %d", w)
	}
}

func TestCloseIsIdempotentAndFinal(t *testing.T) {
	s := open(t, t.TempDir(), &clock{t0}, Options{})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Record(delta{"aa", 1}) {
		t.Fatal("record after close accepted")
	}
	if _, ok := wins(s, "aa"); ok {
		t.Fatal("get after close")
	}
	ran := false
	s.View(func(map[string]Entry[record]) { ran = true })
	if ran {
		t.Fatal("view ran after close")
	}
}

func TestCloseAppliesOrCountsEveryRecord(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	s := open(t, dir, c, Options{Buffer: 64})
	const writers, each = 4, 300
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				s.Record(delta{"aa", 1})
			}
		}()
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	dropped := s.Dropped()
	if s.Record(delta{"aa", 1}) || s.Dropped() != dropped+1 {
		t.Fatal("record after close must be refused and counted")
	}
	s2 := open(t, dir, c, Options{})
	defer s2.Close()
	w, _ := wins(s2, "aa")
	if uint64(w)+dropped != writers*each {
		t.Fatalf("kept %d + dropped %d != %d: records lost silently", w, dropped, writers*each)
	}
}

// Two stores never write one directory at once: the second Open is refused
// until the first has closed (blue/green handoff).
func TestOpenRefusesALockedDir(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	a := open(t, dir, c, Options{})
	if _, err := Open[delta, record](dir, toy{}, Options{Now: c.Now}); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open: %v, want ErrLocked", err)
	}
	a.Record(delta{"aa", 2})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b := open(t, dir, c, Options{})
	defer b.Close()
	if w, ok := wins(b, "aa"); !ok || w != 2 {
		t.Fatalf("handed-off store lost the first store's record: %d %v", w, ok)
	}
}

// A store that dies without Close (power loss) releases the lock too.
func TestCrashReleasesTheLock(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	a := open(t, dir, c, Options{})
	a.crash()
	b := open(t, dir, c, Options{})
	b.Close()
}

type keeperToy struct{ toy }

func (keeperToy) Keep(r record) bool { return r.Wins >= 100 }

func TestKeeperSurvivesKeyCapFlood(t *testing.T) {
	c := &clock{t0}
	s, err := Open[delta, record](t.TempDir(), keeperToy{}, Options{MaxKeys: 3, Now: c.Now, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Record(delta{"vet", 100}) // oldest, but kept
	for i := range 30 {
		c.now = t0.Add(time.Duration(i+1) * time.Hour)
		s.Record(delta{string(rune('a' + i)), 1})
	}
	if _, ok := wins(s, "vet"); !ok {
		t.Fatal("a kept record was evicted")
	}
	if _, ok := wins(s, "a"); ok {
		t.Fatal("unkept records go")
	}
}

func TestEvictionFallsBackWhenEveryKeyIsKept(t *testing.T) {
	c := &clock{t0}
	s, _ := Open[delta, record](t.TempDir(), keeperToy{}, Options{MaxKeys: 2, Now: c.Now, Log: slog.New(slog.DiscardHandler)})
	defer s.Close()
	s.Record(delta{"a", 100})
	c.now = t0.Add(time.Hour)
	s.Record(delta{"b", 100})
	c.now = t0.Add(2 * time.Hour)
	s.Record(delta{"c", 100})
	var n int
	do(s, func(s *store) { n = len(s.entries) })
	if _, ok := wins(s, "a"); ok || n != 2 {
		t.Fatalf("least recently seen must go when all are kept: %d keys", n)
	}
}

// atRec remembers the time Fold saw, to compare live against replay.
type atRec struct {
	Wins int   `json:"wins"`
	At   int64 `json:"at"`
}

type atSchema struct{}

func (atSchema) Key(d delta) string { return d.K }
func (atSchema) Fold(r atRec, d delta, at time.Time) atRec {
	return atRec{r.Wins + d.Wins, at.UnixNano()}
}
func (atSchema) Empty(d delta) bool { return d.Wins == 0 }

func TestFoldSeesTheSameTimeLiveAndOnReplay(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0.Add(123_456_789 * time.Nanosecond)}
	o := Options{Now: c.Now, Log: slog.New(slog.DiscardHandler)}
	s, _ := Open[delta, atRec](dir, atSchema{}, o)
	s.Record(delta{"aa", 1})
	live, seen, _ := s.Get("aa")
	s.crash() // replay from the journal
	s, _ = Open[delta, atRec](dir, atSchema{}, o)
	replayed, seen2, _ := s.Get("aa")
	if live != replayed || !seen.Equal(seen2) {
		t.Fatalf("live %+v %v, replayed %+v %v", live, seen, replayed, seen2)
	}
	s.Close() // and from the snapshot
	s, _ = Open[delta, atRec](dir, atSchema{}, o)
	defer s.Close()
	if snap, seen3, _ := s.Get("aa"); snap != live || !seen.Equal(seen3) {
		t.Fatalf("snapshot %+v %v", snap, seen3)
	}
}

func TestOpenRejectsForeignSnapshotVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, snapName), []byte(`{"v":2,"seq":1,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open[delta, record](dir, toy{}, Options{}); err == nil {
		t.Fatal("a snapshot with another version must not load")
	}
	b, _ := os.ReadFile(filepath.Join(dir, snapName))
	if !strings.Contains(string(b), `"v":2`) {
		t.Fatal("foreign snapshot was touched")
	}
	// the failed Open released the lock
	if err := os.Remove(filepath.Join(dir, snapName)); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir, &clock{t0}, Options{})
	s.Close()
}
