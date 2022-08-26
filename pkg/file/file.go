package file

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/ethersphere/bee/pkg/file/joiner"
	"github.com/ethersphere/bee/pkg/file/splitter"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
	"go.uber.org/atomic"
)

type writeOp struct {
	start int64
	end   int64
	buf   []byte
	tmsp  int64
}

type Reader interface {
	io.ReaderAt
	io.ReadSeeker
}

func isZeroAddress(ref swarm.Address) bool {
	if ref.Equal(swarm.ZeroAddress) {
		return true
	}
	zeroAddr := make([]byte, 32)
	if swarm.NewAddress(zeroAddr).Equal(ref) {
		return true
	}
	return false
}

func New(addr swarm.Address, store store.PutGetter, encrypt bool) *BeeFile {
	synced := atomic.NewBool(!isZeroAddress(addr))
	return &BeeFile{
		store:     store,
		encrypt:   encrypt,
		reference: addr,
		synced:    *synced,
	}
}

type BeeFile struct {
	mtx            sync.Mutex
	rdrUseful      atomic.Bool
	rdrOff         atomic.Int64
	rdr            Reader
	wOff           atomic.Int64
	size           atomic.Int64
	writesInFlight []*writeOp
	reference      swarm.Address
	synced         atomic.Bool
	store          store.PutGetter
	sf             singleflight.Group
	encrypt        bool

	inmem *atomic.Uint64
}

func (f *BeeFile) synchronize() func() {
	f.mtx.Lock()
	return func() {
		f.mtx.Unlock()
	}
}

func (f *BeeFile) reader() (io.ReaderAt, error) {
	if !f.rdrUseful.Load() && !isZeroAddress(f.reference) {
		// use singleflight
		_, err, _ := f.sf.Do("create reader", func() (res interface{}, err error) {
			f.rdr, _, err = joiner.New(context.Background(), f.store, f.reference)
			if err != nil {
				return nil, err
			}
			_, err = f.rdr.Seek(f.rdrOff.Load(), 0)
			if err != nil {
				return nil, err
			}
			f.rdrUseful.Store(true)
			return f.rdr, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return &inmemWrappedReader{f}, nil
}

type patch writeOp

type inmemWrappedReader struct {
	f *BeeFile
}

func (i *inmemWrappedReader) ReadAt(buf []byte, off int64) (n int, err error) {
	patches := i.getPatches(off, off+int64(len(buf)))
	if len(patches) == 1 &&
		(len(patches[0].buf) == len(buf) || (off+int64(len(patches[0].buf)) == i.f.size.Load())) {
		copy(buf, patches[0].buf)
		return len(patches[0].buf), nil
	}
	if i.f.rdr != nil {
		n, err = i.f.rdr.ReadAt(buf, off)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		for _, p := range patches {
			copy(buf[p.start:p.end], p.buf)
		}
		return n, err
	}
	return 0, io.EOF
}

func (i *inmemWrappedReader) getPatches(start, end int64) (patches []*patch) {
	defer i.f.synchronize()()

	for _, v := range i.f.writesInFlight {
		if start >= v.end {
			continue
		}
		if end < v.start {
			break
		}
		var (
			patchStart, patchEnd int64
			patchBuf             []byte
		)
		if start >= v.start {
			patchStart = 0
			patchBuf = v.buf[start-v.start:]
		} else {
			patchStart = v.start - start
			patchBuf = v.buf
		}
		if v.end >= end {
			patchEnd = (end - start)
		} else {
			patchEnd = v.end - start
		}
		patchBuf = patchBuf[:patchEnd-patchStart]
		patches = append(patches, &patch{patchStart, patchEnd, patchBuf, 0})
	}
	return patches
}

func (f *BeeFile) Read(b []byte) (n int, err error) {
	rdr, err := f.reader()
	if err != nil {
		return 0, err
	}
	n, err = rdr.ReadAt(b, f.rdrOff.Load())
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	f.rdrOff.Add(int64(n))
	return n, err
}

func (f *BeeFile) ReadAt(b []byte, off int64) (n int, err error) {
	rdr, err := f.reader()
	if err != nil {
		return 0, err
	}
	return rdr.ReadAt(b, off)
}

func (f *BeeFile) Write(b []byte) (n int, err error) {
	off := f.wOff.Add(int64(len(b)))

	bcopy := make([]byte, len(b))
	copy(bcopy, b)

	newOp := &writeOp{
		start: off - int64(len(b)),
		end:   off,
		buf:   bcopy,
		tmsp:  time.Now().UnixNano(),
	}
	f.enqueueWriteOp(newOp)
	return len(b), nil
}

func (f *BeeFile) WriteAt(b []byte, off int64) (n int, err error) {
	bcopy := make([]byte, len(b))
	copy(bcopy, b)

	newOp := &writeOp{
		start: off,
		end:   off + int64(len(b)),
		buf:   bcopy,
		tmsp:  time.Now().UnixNano(),
	}
	f.enqueueWriteOp(newOp)
	return len(b), nil
}

func (f *BeeFile) enqueueWriteOp(op *writeOp) {
	defer f.synchronize()()

	if f.writesInFlight == nil {
		f.writesInFlight = make([]*writeOp, 0)
	}

	idx := 0
	for ; idx < len(f.writesInFlight); idx++ {
		if f.writesInFlight[idx].start > op.start {
			break
		}
	}
	switch {
	case idx == 0:
		f.writesInFlight = append([]*writeOp{op}, f.writesInFlight...)
	case idx == len(f.writesInFlight):
		f.writesInFlight = append(f.writesInFlight, op)
	default:
		f.writesInFlight = append(f.writesInFlight[:idx], append([]*writeOp{op}, f.writesInFlight[idx:]...)...)
	}
	f.writesInFlight = merge(f.writesInFlight)
	if f.writesInFlight[len(f.writesInFlight)-1].end > f.size.Load() {
		f.size.Store(f.writesInFlight[len(f.writesInFlight)-1].end)
	}
	f.synced.Store(false)
	return
}

func merge(writeOps []*writeOp) (merged []*writeOp) {
	for _, op := range writeOps {
		if len(merged) == 0 || merged[len(merged)-1].end < op.start {
			// No overlap
			merged = append(merged, op)
		} else {
			prev := merged[len(merged)-1]
			if op.end > prev.end {
				old := prev.end
				prev.end = op.end
				prev.buf = append(prev.buf, make([]byte, prev.end-old)...)
				var idxSrc, idxDst, tmsp int64
				if op.tmsp > prev.tmsp {
					idxDst = op.start - prev.start
					tmsp = op.tmsp
				} else {
					idxDst = old - prev.start
					idxSrc = old - op.start
					tmsp = prev.tmsp
				}
				copy(prev.buf[idxDst:], op.buf[idxSrc:])
				prev.tmsp = tmsp
			} else {
				if op.tmsp > prev.tmsp {
					start := op.start - prev.start
					end := start + (op.end - op.start)
					copy(prev.buf[start:end], op.buf)
					prev.tmsp = op.tmsp
				}
			}
		}
	}
	return merged
}

func (f *BeeFile) Sync() error {
	f.rdrOff.Store(int64(0))
	rdr, err := f.reader()
	if err != nil {
		return err
	}
	splitter := splitter.NewSimpleSplitter(f.store, storage.ModePutUpload)
	ref, err := splitter.Split(context.Background(), &readCloser{rdr: rdr}, f.size.Load(), f.encrypt)
	if err != nil {
		return err
	}
	f.reference = ref
	f.synced.Store(true)
	f.rdrUseful.Store(false)
	f.writesInFlight = nil
	return nil
}

func (f *BeeFile) Truncate(sz int64) error {
	f.size.Store(sz)
	return nil
}

func (f *BeeFile) Close() (swarm.Address, error) {
	if !f.synced.Load() {
		err := f.Sync()
		if err != nil {
			return swarm.ZeroAddress, err
		}
	}
	return f.reference, nil
}

type readCloser struct {
	off atomic.Int64
	rdr io.ReaderAt
}

func (r *readCloser) Read(buf []byte) (n int, err error) {
	currentOff := r.off.Load()
	r.off.CAS(currentOff, currentOff+int64(len(buf)))
	return r.rdr.ReadAt(buf, currentOff)
}

func (r *readCloser) Close() error {
	return nil
}
