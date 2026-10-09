package verification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// Limits on what a run directory may hold. A bundle beyond them is reported
// (INCONCLUSIVE), never read in part. The delivered fixture bundle is about
// 80 files and 7 MB.
const (
	MaxFiles      = 4096
	MaxFileBytes  = 64 << 20
	MaxTotalBytes = 1 << 30
)

var (
	errMissing = errors.New("missing")      // not in the run directory: INCONCLUSIVE
	errUnsafe  = errors.New("unsafe path")  // escapes, links, non-regular files: FAIL
	errLimit   = errors.New("over a limit") // INCONCLUSIVE
)

// bundle reads one run directory, read only and without following
// anything: the inventory is taken with lstat, every entry must be a
// directory or a regular file, and each file is opened with O_NOFOLLOW and
// O_NONBLOCK under an os.Root and checked to be the inventoried file.
type bundle struct {
	root       *os.Root
	files      map[string]os.FileInfo // regular files by slash path
	violations []string               // symlinks, FIFOs, devices, sockets
	tooMany    bool
	total      int64 // bytes read so far
}

func openBundle(dir string) (*bundle, error) {
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	b := &bundle{root: root, files: map[string]os.FileInfo{}}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		switch t := d.Type(); {
		case t&fs.ModeSymlink != 0:
			b.violations = append(b.violations, p+": a symbolic link")
			if d.IsDir() {
				return fs.SkipDir
			}
		case d.IsDir():
		case t.IsRegular():
			if len(b.files) >= MaxFiles {
				b.tooMany = true
				return fs.SkipAll
			}
			info, err := root.Lstat(p)
			if err != nil {
				return err
			}
			b.files[p] = info
		default:
			b.violations = append(b.violations, p+": not a regular file ("+t.String()+")")
		}
		return nil
	})
	if err != nil {
		root.Close()
		return nil, err
	}
	return b, nil
}

func (b *bundle) close() { b.root.Close() }

// cleanRel accepts only a plain relative slash path inside the directory.
func cleanRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) || strings.ContainsRune(rel, 0) || path.Clean(rel) != rel {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == "." {
			return false
		}
	}
	return true
}

// read returns a file's bytes: errUnsafe for a path that is not a plain
// relative path, or a file that is not the inventoried regular file;
// errMissing for one that is not there; errLimit beyond the limits.
func (b *bundle) read(rel string) ([]byte, error) {
	if !cleanRel(rel) {
		return nil, fmt.Errorf("%w: %q", errUnsafe, rel)
	}
	want, ok := b.files[rel]
	if !ok {
		for _, v := range b.violations {
			if strings.HasPrefix(v, rel+":") {
				return nil, fmt.Errorf("%w: %s", errUnsafe, v)
			}
		}
		// A path under a linked directory is not in the inventory either.
		for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
			if info, err := b.root.Lstat(dir); err == nil && info.Mode()&fs.ModeSymlink != 0 {
				return nil, fmt.Errorf("%w: %s goes through a symbolic link", errUnsafe, rel)
			}
		}
		return nil, fmt.Errorf("%w: %s", errMissing, rel)
	}
	if want.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes (limit %d)", errLimit, rel, want.Size(), MaxFileBytes)
	}
	if b.total+want.Size() > MaxTotalBytes {
		return nil, fmt.Errorf("%w: more than %d bytes in all", errLimit, MaxTotalBytes)
	}
	f, err := b.root.OpenFile(filepath.FromSlash(rel), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", errUnsafe, rel, err)
	}
	defer f.Close()
	got, err := f.Stat()
	if err != nil || !got.Mode().IsRegular() || !os.SameFile(got, want) {
		return nil, fmt.Errorf("%w: %s is not the regular file the inventory found", errUnsafe, rel)
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > MaxFileBytes {
		return nil, fmt.Errorf("%w: %s grew past the limit", errLimit, rel)
	}
	b.total += int64(len(raw))
	return raw, nil
}

// strictJSON decodes one JSON document into dst: no duplicate member
// anywhere, no unknown field, nothing after it.
func strictJSON(raw []byte, dst any) error {
	if err := noDuplicates(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("more than one JSON document")
	}
	return nil
}

func noDuplicates(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	type frame struct {
		object bool
		keys   map[string]bool
		key    bool // the next token is a key
	}
	var stack []*frame
	for {
		t, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		top := func() *frame {
			if len(stack) == 0 {
				return nil
			}
			return stack[len(stack)-1]
		}
		if f := top(); f != nil && f.object && f.key {
			if k, ok := t.(string); ok {
				if f.keys[k] {
					return fmt.Errorf("duplicate JSON member %q", k)
				}
				f.keys[k], f.key = true, false
				continue
			}
		}
		switch t {
		case json.Delim('{'):
			stack = append(stack, &frame{object: true, keys: map[string]bool{}, key: true})
			continue
		case json.Delim('['):
			stack = append(stack, &frame{})
			continue
		case json.Delim('}'), json.Delim(']'):
			stack = stack[:len(stack)-1]
		}
		if f := top(); f != nil && f.object {
			f.key = true // a value ended: a key comes next
		}
	}
}
