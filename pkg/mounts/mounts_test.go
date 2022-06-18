package mounts_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/ethersphere/bee/pkg/storage/mock"
	"github.com/ethersphere/bee/pkg/swarm"
)

type mockLookuperPublisher struct {
	lk       sync.Mutex
	existing map[string]swarm.Address
}

func (m *mockLookuperPublisher) Get(_ context.Context, id string, version int64) (swarm.Address, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	ref, found := m.existing[id]
	if !found {
		return swarm.ZeroAddress, errors.New("not found")
	}
	return ref, nil
}

func (m *mockLookuperPublisher) Put(_ context.Context, id string, version int64, ref swarm.Address) error {
	m.lk.Lock()
	defer m.lk.Unlock()

	m.existing[id] = ref
	return nil
}

func TestMounts(t *testing.T) {
	st := mock.NewStorer()
	lp := &mockLookuperPublisher{existing: make(map[string]swarm.Address)}

	mnts := mounts.New(lp, lp, st)

	t.Run("empty", func(t *testing.T) {
		mntList, err := mnts.Get(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
		if len(mntList) != 0 {
			t.Fatal("invalid no of mountsi:", len(mntList))
		}
	})
	t.Run("create some mounts", func(t *testing.T) {
		err := mnts.Put(context.TODO(), []string{"a", "b", "c"})
		if err != nil {
			t.Fatal(err)
		}
		mntList, err := mnts.Get(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
		if len(mntList) != 3 {
			t.Fatal("invalid no of mounts:", len(mntList))
		}
		foundA, foundB, foundC := false, false, false
		for _, m := range mntList {
			if m == "a" {
				foundA = true
			}
			if m == "b" {
				foundB = true
			}
			if m == "c" {
				foundC = true
			}
		}
		if !foundA || !foundB || !foundC {
			t.Fatal("incorrect mount list", mntList)
		}
	})
	t.Run("add mount", func(t *testing.T) {
		err := mnts.Put(context.TODO(), []string{"a", "b", "c", "testlargename"})
		if err != nil {
			t.Fatal(err)
		}
		mntList, err := mnts.Get(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
		if len(mntList) != 4 {
			t.Fatal("invalid no of mounts:", len(mntList))
		}
		found := false
		for _, m := range mntList {
			if m == "testlargename" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("not found new mount")
		}
	})
}
