package fs_test

import (
	"bytes"
	"flag"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"testing/iotest"
	"time"

	fs "github.com/aloknerurkar/bee-afs/pkg/fuse"
	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/storage/mock"
	logger "github.com/ipfs/go-log/v2"
)

var debug = flag.Bool("debug", false, "FUSE debug logs")
var logs = flag.Bool("logs", false, "Enable logs")

type testStorer struct {
	*mock.MockStorer
	mp map[string][]byte
}

func newTestFs(st store.PutGetter) (*fs.BeeFs, string, func(), error) {
	if *logs {
		logger.SetLogLevel("*", "Debug")
	}
	mntDir, err := ioutil.TempDir("", "tmpfuse")
	if err != nil {
		return nil, "", func() {}, err
	}

	pk, _ := crypto.GenerateSecp256k1Key()
	signer := crypto.NewDefaultSigner(pk)
	owner, err := signer.EthereumAddress()
	if err != nil {
		return nil, "", func() {}, err
	}

	lk := lookuper.New(st, owner)
	pb := publisher.New(st, signer)

	fsImpl, err := fs.New(st, lk, pb)
	if err != nil {
		return nil, "", func() {}, err
	}
	srv := fuse.NewFileSystemHost(fsImpl)
	srv.SetCapReaddirPlus(true)
	sched := make(chan struct{})
	var fuseArgs []string
	if *debug {
		fuseArgs = []string{"-d"}
	}
	go func() {
		close(sched)
		if !srv.Mount(mntDir, fuseArgs) {
			panic("mount returned false")
		}
	}()
	<-sched

	time.Sleep(time.Second)

	return fsImpl, mntDir, func() {
		srv.Unmount()
		time.Sleep(time.Second)
		os.RemoveAll(mntDir)
	}, nil
}

func TestFileBasic(t *testing.T) {
	st := mock.NewStorer()
	_, mntDir, closer, err := newTestFs(st)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()

	time.Sleep(time.Second)

	content := []byte("hello world")
	fn := filepath.Join(mntDir, "file1")

	if err := os.WriteFile(fn, content, 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, err := os.ReadFile(fn); err != nil {
		t.Fatalf("ReadFile: %v", err)
	} else if bytes.Compare(got, content) != 0 {
		t.Fatalf("ReadFile: got %q, want %q", got, content)
	}

	f, err := os.Open(fn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("Fstat: %v", err)
	} else if int(fi.Size()) != len(content) {
		t.Errorf("got size %d want 5", fi.Size())
	}
	if got, want := uint32(fi.Mode()), uint32(0755); got != want {
		t.Errorf("Fstat: got mode %o, want %o", got, want)
	}
	if err := f.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestMultiDirWithFiles(t *testing.T) {
	entries := []struct {
		path    string
		isDir   bool
		size    int64
		content []byte
	}{
		{
			path:  "dir1",
			isDir: true,
		},
		{
			path:  "dir2",
			isDir: true,
		},
		{
			path:  "dir3",
			isDir: true,
		},
		{
			path: "file1",
			size: 1024 * 1024,
		},
		{
			path: "dir1/file11",
			size: 1024 * 512,
		},
		{
			path: "dir1/file12",
			size: 1024 * 1024,
		},
		{
			path: "dir3/file31",
			size: 1024 * 1024,
		},
		{
			path: "dir3/file32",
			size: 1024 * 1024,
		},
		{
			path: "dir3/file33",
			size: 1024,
		},
		{
			path:  "dir2/dir4",
			isDir: true,
		},
		{
			path:  "dir2/dir4/dir5",
			isDir: true,
		},
		{
			path: "dir2/dir4/file241",
			size: 1024 * 1024,
		},
		{
			path: "dir2/dir4/dir5/file2451",
			size: 1024 * 1024,
		},
	}

	st := mock.NewStorer()
	_, mntDir, closer, err := newTestFs(st)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()

	t.Run("create structure", func(t *testing.T) {
		for idx, v := range entries {
			if v.isDir {
				err := os.Mkdir(filepath.Join(mntDir, v.path), 0755)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				f, err := os.Create(filepath.Join(mntDir, v.path))
				if err != nil {
					t.Fatal(err)
				}
				buf := make([]byte, 1024)
				var off int64 = 0
				for off < v.size {
					rand.Read(buf)
					n, err := f.Write(buf)
					if err != nil {
						t.Fatal(err)
					}
					if n != 1024 {
						t.Fatalf("wrote %d bytes exp %d", n, 1024)
					}
					entries[idx].content = append(entries[idx].content, buf...)
					off += int64(n)
				}
				err = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	})

	verify := func(t *testing.T, mnt string) {
		t.Helper()
		for _, v := range entries {
			st, err := os.Stat(filepath.Join(mnt, v.path))
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().IsDir() != v.isDir {
				t.Fatalf("isDir expected: %t found: %t", v.isDir, st.Mode().IsDir())
			}
			if !v.isDir {
				if st.Size() != v.size {
					t.Fatalf("expected size %d found %d", v.size, st.Size())
				}
				if got, err := ioutil.ReadFile(filepath.Join(mnt, v.path)); err != nil {
					t.Fatalf("ReadFile: %v", err)
				} else if bytes.Compare(got, v.content) != 0 {
					t.Fatalf("ReadFile: got %q, want %q", got[:30], v.content[:30])
				}
			}
		}
	}

	t.Run("verify structure", func(t *testing.T) {
		verify(t, mntDir)
	})

	t.Run("fstest", func(t *testing.T) {
		pathsToFind := []string{
			"dir1", "dir2", "dir3", "file1", "dir1/file11", "dir1/file12",
			"dir3/file31", "dir3/file32", "dir3/file33", "dir2/dir4", "dir2/dir4/dir5",
			"dir2/dir4/file241", "dir2/dir4/dir5/file2451",
		}
		fuseMount := os.DirFS(mntDir)
		err := fstest.TestFS(fuseMount, pathsToFind...)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("iotest on files", func(t *testing.T) {
		for _, v := range entries {
			if !v.isDir {
				f, err := os.Open(filepath.Join(mntDir, v.path))
				if err != nil {
					t.Fatal(err)
				}
				err = iotest.TestReader(f, v.content)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	})

	// t.Run("unmount and mount and verify", func(t *testing.T) {
	// 	closer()
	// 	time.Sleep(time.Second)
	// 	_, mntDir, closer, err = newTestFs(st)
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	time.Sleep(time.Second)
	// 	verify(t, mntDir)
	// })
}
