package file_test

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/aloknerurkar/bee-afs/pkg/file"
	"github.com/ethersphere/bee/pkg/storage/mock"
	"github.com/ethersphere/bee/pkg/swarm"
)

func TestFileBasic(t *testing.T) {
	st := mock.NewStorer()

	runTests := func(t *testing.T, f *file.BeeFile, testBuf []byte) {
		t.Run("write", func(t *testing.T) {
			n, err := f.Write(testBuf)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(testBuf) {
				t.Fatalf("invalid length of write exp: %d found: %d", len(testBuf), n)
			}
		})

		t.Run("read", func(t *testing.T) {
			newBuf := make([]byte, len(testBuf))
			n, err := f.Read(newBuf)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(newBuf) {
				t.Fatalf("invalid length of read exp: %d found: %d", len(testBuf), n)
			}
			if bytes.Compare(newBuf, testBuf) != 0 {
				t.Fatal("read bytes not equal")
			}
		})

		var ref swarm.Address
		var err error

		t.Run("close", func(t *testing.T) {
			ref, err = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			// Additional close without write should give same address
			ref2, err := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !ref2.Equal(ref) {
				t.Fatal("addresses dont match after no write")
			}
		})

		t.Run("read from reference", func(t *testing.T) {
			f = file.New(ref, st, false)

			newBuf := make([]byte, len(testBuf))
			n, err := f.Read(newBuf)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(newBuf) {
				t.Fatalf("invalid length of read exp: %d found: %d", len(testBuf), n)
			}
			if bytes.Compare(newBuf, testBuf) != 0 {
				t.Fatal("read bytes not equal")
			}

			ref2, err := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !ref2.Equal(ref) {
				t.Fatal("addresses dont match after no write")
			}
		})
	}

	t.Run("small file", func(t *testing.T) {
		f := file.New(swarm.ZeroAddress, st, false)
		testBuf := []byte("hello world")
		runTests(t, f, testBuf)
	})
	t.Run("big file", func(t *testing.T) {
		f := file.New(swarm.ZeroAddress, st, false)
		testBuf := make([]byte, 1024*1024)
		rand.Read(testBuf)
		runTests(t, f, testBuf)
	})
}

func TestFileHybrid(t *testing.T) {
	st := mock.NewStorer()
	f := file.New(swarm.ZeroAddress, st, false)
	testBuf := make([]byte, 4*4096)
	rand.Read(testBuf)

	t.Run("write and sync", func(t *testing.T) {
		n, err := f.Write(testBuf)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(testBuf) {
			t.Fatalf("invalid length of write exp: %d found: %d", len(testBuf), n)
		}
		err = f.Sync()
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("read after sync", func(t *testing.T) {
		newBuf := make([]byte, len(testBuf))
		n, err := f.Read(newBuf)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(newBuf) {
			t.Fatalf("invalid length of read exp: %d found: %d", len(testBuf), n)
		}
		if bytes.Compare(newBuf, testBuf) != 0 {
			t.Fatal("read bytes not equal")
		}
	})

	t.Run("writeAt after sync", func(t *testing.T) {
		rand512b := make([]byte, 512)

		rand.Read(rand512b)

		n, err := f.WriteAt(rand512b, 400)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(rand512b) {
			t.Fatalf("invalid length of read exp: %d found: %d", len(rand512b), n)
		}

		copy(testBuf[400:400+len(rand512b)], rand512b)

		rand256b := make([]byte, 256)
		rand.Read(rand256b)

		n, err = f.WriteAt(rand256b, 2000)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(rand256b) {
			t.Fatalf("invalid length of read exp: %d found: %d", len(rand512b), n)
		}

		copy(testBuf[2000:2000+len(rand256b)], rand256b)
	})

	t.Run("readAt after hybrid state", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			start := int64(i * 1024)

			buf := make([]byte, 1024)
			n, err := f.ReadAt(buf, start)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(buf) {
				t.Fatalf("invalid length of read exp: %d found: %d", len(buf), n)
			}
			if bytes.Compare(buf, testBuf[start:start+1024]) != 0 {
				t.Fatal("read bytes not equal")
			}
		}
	})

	t.Run("close after hybrid state then read", func(t *testing.T) {
		ref, err := f.Close()
		if err != nil {
			t.Fatal(err)
		}

		f = file.New(ref, st, false)

		newBuf := make([]byte, len(testBuf))
		n, err := f.Read(newBuf)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(newBuf) {
			t.Fatalf("invalid length of read exp: %d found: %d", len(testBuf), n)
		}
		if bytes.Compare(newBuf, testBuf) != 0 {
			t.Fatal("read bytes not equal")
		}
	})
}
