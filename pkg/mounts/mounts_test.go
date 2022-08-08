package mounts_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
		if len(mntList.Mnts) != 0 {
			t.Fatal("invalid no of mountsi:", len(mntList.Mnts))
		}
	})
	t.Run("create some mounts", func(t *testing.T) {
		err := mnts.Put(context.TODO(), mounts.Mounts{
			Mnts: []mounts.MountInfo{
				{
					Name:    "a",
					Batch:   "b1",
					Encrypt: false,
					Pin:     true,
					Created: time.Now().Unix(),
				},
				{
					Name:    "b",
					Batch:   "b2",
					Encrypt: true,
					Pin:     false,
					Created: time.Now().Unix(),
				},
				{
					Name:    "c",
					Batch:   "b3",
					Created: time.Now().Unix(),
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		mntList, err := mnts.Get(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
		if len(mntList.Mnts) != 3 {
			t.Fatal("invalid no of mounts:", len(mntList.Mnts))
		}
		foundA, foundB, foundC := false, false, false
		for _, m := range mntList.Mnts {
			if m.Name == "a" {
				foundA = true
			}
			if m.Name == "b" {
				foundB = true
			}
			if m.Name == "c" {
				foundC = true
			}
		}
		if !foundA || !foundB || !foundC {
			t.Fatal("incorrect mount list", mntList)
		}
	})
	t.Run("add mount", func(t *testing.T) {
		mntList1, err := mnts.Get(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
		mntList1.Mnts = append(mntList1.Mnts, mounts.MountInfo{
			Name:    "testrandomname",
			Batch:   "b4",
			Created: time.Now().Unix(),
		})
		err = mnts.Put(context.TODO(), mntList1)
		if err != nil {
			t.Fatal(err)
		}
		mntList2, err := mnts.Get(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
		if len(mntList2.Mnts) != 4 {
			t.Fatal("invalid no of mounts:", len(mntList2.Mnts))
		}
		found := false
		for _, m := range mntList2.Mnts {
			if m.Name == "testrandomname" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("not found new mount")
		}
	})
}
