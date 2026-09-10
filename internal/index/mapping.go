package index

import (
	"fmt"
	"os"

	mmap "github.com/blevesearch/mmap-go"
)

// mapping is a read-only view of an index file.
//
// By default it is a shared, read-only memory map (PROT_READ, MAP_SHARED), for
// two reasons that matter when replicas run side by side over a shared volume:
// the file's pages exist once in the host page cache no matter how many
// processes map it, and those pages are reclaimable, so memory pressure evicts
// them instead of OOM-killing a container. They are also invisible to the Go
// collector, which otherwise sizes its heap around hundreds of megabytes of
// byte slices it can never free.
//
// The trade is that a map is a live view rather than a snapshot: rewriting an
// index in place under a running reader produces torn reads, not stale ones.
// Replace an index by building into a new directory and swapping, never by
// overwriting the files a reader may still hold.
type mapping struct {
	data []byte
	mm   mmap.MMap
	f    *os.File
}

// openMapping maps path, or reads it onto the heap when mapped is false.
func openMapping(path string, mapped bool) (*mapping, error) {
	if !mapped {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		return &mapping{data: b}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	// mmap rejects a zero-length mapping, and an index legitimately can have an
	// empty hot list when no prefix was hot enough to earn one.
	if info.Size() == 0 {
		f.Close()
		return &mapping{}, nil
	}
	mm, err := mmap.Map(f, mmap.RDONLY, 0)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("map %s: %w", path, err)
	}
	return &mapping{data: mm, mm: mm, f: f}, nil
}

func (m *mapping) Close() error {
	if m == nil {
		return nil
	}
	var err error
	if m.mm != nil {
		err = m.mm.Unmap()
		m.mm = nil
	}
	if m.f != nil {
		if cerr := m.f.Close(); err == nil {
			err = cerr
		}
		m.f = nil
	}
	m.data = nil
	return err
}
