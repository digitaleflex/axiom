package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
)

const (
	// fileMode is the state file permission: owner read/write only (#81).
	fileMode = 0o600
	// dirMode is the parent directory permission when the store creates it.
	dirMode = 0o700
	// formatVersion is the on-disk record format version.
	formatVersion = 1
	// defaultMaxLogRecords triggers compaction once the append log reaches
	// this many records (approximately; compaction resets the counter).
	defaultMaxLogRecords = 4096
)

// record is one line of the append log. Entry is kept as raw bytes so the CRC
// can be verified over exactly what was written.
type record struct {
	V     int             `json:"v"`
	Seq   int             `json:"seq"`
	CRC   string          `json:"crc"`
	Entry json.RawMessage `json:"entry"`
}

// encodeRecord marshals e into a checksummed log line (without the trailing
// newline).
func encodeRecord(seq int, e Entry) ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("state: marshal entry: %w", err)
	}
	return json.Marshal(record{
		V:     formatVersion,
		Seq:   seq,
		CRC:   fmt.Sprintf("%08x", crc32.ChecksumIEEE(raw)),
		Entry: raw,
	})
}

// decodeRecord validates and decodes one log line. It returns ok=false for a
// malformed line, a version mismatch, a CRC mismatch or an invalid phase.
func decodeRecord(line []byte) (Entry, bool) {
	var r record
	if err := json.Unmarshal(line, &r); err != nil {
		return Entry{}, false
	}
	if r.V != formatVersion || len(r.Entry) == 0 {
		return Entry{}, false
	}
	if fmt.Sprintf("%08x", crc32.ChecksumIEEE(r.Entry)) != r.CRC {
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(r.Entry, &e); err != nil {
		return Entry{}, false
	}
	if !e.Phase.valid() {
		return Entry{}, false
	}
	return e, true
}

// loadFile reads and replays the append log at path. A missing file yields no
// entries and no error. A damaged file yields the recoverable entries plus a
// *CorruptionError; only an unreadable file yields a hard error.
func loadFile(path string) ([]Entry, *CorruptionError, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("state: read %s: %w", path, err)
	}
	lines := bytes.Split(data, []byte{'\n'})

	lastNonEmpty := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if len(bytes.TrimSpace(lines[i])) > 0 {
			lastNonEmpty = i
			break
		}
	}

	var out []Entry
	var corrupt *CorruptionError
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		e, ok := decodeRecord(bytes.TrimSpace(line))
		if ok {
			out = append(out, e)
			continue
		}
		corrupt = &CorruptionError{Line: i + 1, Tail: i == lastNonEmpty}
		if corrupt.Tail {
			// Incomplete append from a crash: drop just this record.
			corrupt.Dropped = 1
			break
		}
		// Mid-file corruption: stop trusting everything after it.
		for j := i; j <= lastNonEmpty; j++ {
			if len(bytes.TrimSpace(lines[j])) > 0 {
				corrupt.Dropped++
			}
		}
		break
	}
	if corrupt != nil {
		corrupt.Recovered = len(out)
	}
	return out, corrupt, nil
}

// appendLocked writes one record and fsyncs it. A crash can only lose the
// record being written, never a previously flushed one.
func (s *Store) appendLocked(e *Entry) error {
	if s.path == "" {
		return nil
	}
	if s.file == nil {
		if err := s.reopenAppendLocked(); err != nil {
			return err
		}
	}
	s.seq++
	line, err := encodeRecord(s.seq, *e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if _, err := s.file.Write(line); err != nil {
		return fmt.Errorf("state: append: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("state: fsync: %w", err)
	}
	s.appends++
	return nil
}

// reopenAppendLocked (re)opens the append handle, creating the file mode 0600
// and the parent directory mode 0700 when needed.
func (s *Store) reopenAppendLocked() error {
	if s.path == "" {
		return nil
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return fmt.Errorf("state: create dir: %w", err)
		}
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("state: open %s: %w", s.path, err)
	}
	// Enforce 0600 even if the file pre-existed with looser permissions.
	if err := f.Chmod(fileMode); err != nil {
		_ = f.Close()
		return fmt.Errorf("state: chmod: %w", err)
	}
	s.file = f
	return nil
}

// maybeCompactLocked compacts best-effort once the log grows past the bound; a
// compaction failure must not fail the mutation that already succeeded.
func (s *Store) maybeCompactLocked() {
	if s.maxLogRecords <= 0 || s.appends < s.maxLogRecords {
		return
	}
	if err := s.compactLocked(); err != nil {
		s.log.Warn("state: compaction failed", "path", s.path, "error", err)
	}
}

// compactLocked rewrites the whole store as a fresh snapshot using an atomic
// tmp+fsync+rename, then reopens the append handle.
func (s *Store) compactLocked() error {
	if s.path == "" {
		return nil
	}
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}

	entries := s.sortedEntriesLocked()
	var buf bytes.Buffer
	seq := 0
	for _, e := range entries {
		seq++
		line, err := encodeRecord(seq, e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("state: compact tmp: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("state: compact write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("state: compact fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: compact close: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: compact rename: %w", err)
	}
	fsyncDir(filepath.Dir(s.path))

	s.seq = seq
	s.appends = seq
	return s.reopenAppendLocked()
}

// fsyncDir flushes a directory entry after an atomic rename so the rename is
// durable. Best-effort: some filesystems do not support directory fsync.
func fsyncDir(dir string) {
	if dir == "" {
		dir = "."
	}
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
