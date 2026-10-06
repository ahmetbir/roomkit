package ledger

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type snapshot[R any] struct {
	V       int                 `json:"v"`
	Seq     uint64              `json:"seq"`
	Entries map[string]Entry[R] `json:"entries"`
}

// writeSnapshot streams the snapshot to a temp file, fsyncs, renames it into
// place and fsyncs the directory. Entries are encoded one at a time, so the
// whole store is never marshalled into one buffer.
func (s *Store[D, R]) writeSnapshot() error {
	if err := s.w.Flush(); err != nil {
		s.w.Reset(s.journal)
	}
	tmp := filepath.Join(s.dir, snapName+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 64<<10)
	err = s.encodeSnapshot(bw)
	if err == nil {
		err = bw.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(s.dir, snapName))
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	d, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// encodeSnapshot writes {"v":1,"seq":n,"entries":{key:Entry,…}}; errors surface at Flush.
func (s *Store[D, R]) encodeSnapshot(w *bufio.Writer) error {
	w.WriteString(`{"v":1,"seq":`)
	w.WriteString(strconv.FormatUint(s.seq, 10))
	w.WriteString(`,"entries":{`)
	first := true
	for k, e := range s.entries {
		kb, err := json.Marshal(k)
		if err != nil {
			return err
		}
		eb, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if !first {
			w.WriteByte(',')
		}
		first = false
		w.Write(kb)
		w.WriteByte(':')
		w.Write(eb)
	}
	_, err := w.WriteString("}}\n")
	return err
}

// compact prunes idle entries, writes the snapshot and empties the journal.
func (s *Store[D, R]) compact(now time.Time) error {
	cut := now.Add(-s.o.MaxIdle)
	for k, e := range s.entries {
		if e.Seen.Before(cut) {
			delete(s.entries, k)
		}
	}
	if err := s.writeSnapshot(); err != nil {
		return err
	}
	if err := s.journal.Truncate(0); err != nil {
		return err
	}
	s.size, s.torn, s.compactedAt = 0, false, now
	return nil
}
