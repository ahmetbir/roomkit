package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	journalName  = "journal.jsonl"
	snapName     = "snapshot.json"
	maxLine      = 4096
	evictDivisor = 100 // at the cap, evict MaxKeys/evictDivisor keys in one pass
)

type line[D any] struct {
	Seq uint64 `json:"seq"`
	At  int64  `json:"at"`
	D   D      `json:"d"`
}

// record journals d and then applies it. A delta the journal cannot take is
// dropped and counted, so memory never holds what a restart would lose.
func (s *Store[D, R]) record(d D, at time.Time) {
	if !s.accepts(d) {
		return
	}
	if s.size > s.o.MaxJournal {
		s.tryCompact(at)
	}
	if !s.writeLine(d, at) {
		s.dropped.Add(1)
		return
	}
	s.apply(d, at)
}

func (s *Store[D, R]) accepts(d D) bool {
	return s.sch.Key(d) != "" && !s.sch.Empty(d)
}

// writeLine appends one journal line and flushes it. On an error the writer
// is reset (bufio errors are sticky), the failure is logged once per episode,
// and the next line starts on a fresh line in case this one was torn.
func (s *Store[D, R]) writeLine(d D, at time.Time) bool {
	if s.size > 4*s.o.MaxJournal { // compaction keeps failing: stop growing the file
		return false
	}
	s.seq++
	b, err := json.Marshal(line[D]{Seq: s.seq, At: at.Unix(), D: d})
	if err != nil || len(b) >= maxLine {
		return false
	}
	if s.torn {
		b = append([]byte{'\n'}, b...)
	}
	b = append(b, '\n')
	if _, err = s.w.Write(b); err == nil {
		err = s.w.Flush()
	}
	if err != nil {
		s.w.Reset(s.journal)
		s.torn = true
		if !s.failing {
			s.failing = true
			s.o.Log.Error("ledger: journal write failed, dropping records", "err", err)
		}
		return false
	}
	if s.failing {
		s.failing = false
		s.o.Log.Info("ledger: journal writes recovered", "dropped", s.dropped.Load())
	}
	s.torn = false
	s.size += int64(len(b))
	return true
}

// apply folds d into its key's record, creating (and evicting at the cap)
// as needed. Seen only moves forward.
func (s *Store[D, R]) apply(d D, at time.Time) {
	key := s.sch.Key(d)
	e, ok := s.entries[key]
	if !ok && len(s.entries) >= s.o.MaxKeys {
		s.evict()
	}
	e.R = s.sch.Fold(e.R, d, at)
	if at.After(e.Seen) {
		e.Seen = at
	}
	s.entries[key] = e
}

// evict removes a batch of keys, the least recently seen first (ties:
// smallest key). Batching keeps the O(n log n) pass off the per-insert path.
func (s *Store[D, R]) evict() {
	type cand struct {
		key  string
		seen time.Time
	}
	all := make([]cand, 0, len(s.entries))
	for k, e := range s.entries {
		all = append(all, cand{k, e.Seen})
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if !a.seen.Equal(b.seen) {
			return a.seen.Before(b.seen)
		}
		return a.key < b.key
	})
	n := max(1, s.o.MaxKeys/evictDivisor)
	for _, c := range all[:min(n, len(all))] {
		delete(s.entries, c.key)
	}
}

// endLine appends '\n' when the journal ends inside a torn line.
func (s *Store[D, R]) endLine() error {
	if s.size == 0 {
		return nil
	}
	r, err := os.Open(filepath.Join(s.dir, journalName))
	if err != nil {
		return err
	}
	defer r.Close()
	last := make([]byte, 1)
	if _, err := r.ReadAt(last, s.size-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	n, err := s.journal.Write([]byte{'\n'})
	s.size += int64(n)
	return err
}

// load reads the snapshot, then journal lines newer than it. Torn, garbage
// and over-long lines are skipped; lines at or below the snapshot's seq were
// already counted (crash between rename and truncate).
func (s *Store[D, R]) load() error {
	if f, err := os.Open(filepath.Join(s.dir, snapName)); err == nil {
		var snap snapshot[R]
		err = json.NewDecoder(bufio.NewReader(f)).Decode(&snap)
		f.Close()
		if err != nil {
			return fmt.Errorf("ledger: snapshot: %w", err)
		}
		s.seq = snap.Seq
		for k, e := range snap.Entries {
			s.entries[k] = e
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.Open(filepath.Join(s.dir, journalName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	return eachLine(f, func(b []byte) {
		var l line[D]
		if json.Unmarshal(b, &l) != nil || l.Seq <= s.seq {
			return
		}
		if s.accepts(l.D) {
			s.apply(l.D, time.Unix(l.At, 0))
		}
		s.seq = l.Seq
	})
}

// eachLine calls fn for every line shorter than maxLine; longer ones are skipped whole.
func eachLine(r io.Reader, fn func([]byte)) error {
	br := bufio.NewReaderSize(r, maxLine)
	skip := false
	for {
		b, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			skip = true
			continue
		}
		if !skip && len(b) > 0 {
			fn(b)
		}
		skip = false
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
