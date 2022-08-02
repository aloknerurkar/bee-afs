package mounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/ethersphere/bee/pkg/file/joiner"
	"github.com/ethersphere/bee/pkg/file/splitter"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
)

const userMounts string = "userMounts"

type MountInfo struct {
	Name    string
	Batch   string
	Encrypt bool
	Pin     bool
	Created int64
}

type Mounts struct {
	Mnts []MountInfo
}

type UserMounts interface {
	Get(ctx context.Context) (Mounts, error)
	Put(ctx context.Context, mnts Mounts) error
}

func isAllZeroes(addr swarm.Address) bool {
	return swarm.NewAddress(make([]byte, 32)).Equal(addr)
}

type userMountsImpl struct {
	lk lookuper.Lookuper
	pb publisher.Publisher
	st store.PutGetter
}

func New(lk lookuper.Lookuper, pb publisher.Publisher, st store.PutGetter) UserMounts {
	return &userMountsImpl{lk, pb, st}
}

func NewReadOnly(lk lookuper.Lookuper, st store.PutGetter) UserMounts {
	return &userMountsImpl{lk: lk, st: st}
}

func (u *userMountsImpl) Get(ctx context.Context) (Mounts, error) {
	ref, err := u.lk.Get(ctx, userMounts, time.Now().Unix())
	if err != nil || isAllZeroes(ref) {
		// return empty for now
		return Mounts{}, nil
	}
	reader, _, err := joiner.New(ctx, u.st, ref)
	if err != nil {
		return Mounts{}, fmt.Errorf("failed creating reader %w", err)
	}
	buf, err := io.ReadAll(reader)
	if err != nil {
		return Mounts{}, fmt.Errorf("failed to read %w", err)
	}
	mntList := Mounts{}
	err = json.Unmarshal(buf, &mntList)
	if err != nil {
		return Mounts{}, fmt.Errorf("failed unmarshaling data %w", err)
	}
	return mntList, nil
}

type mountsReader struct {
	*bytes.Buffer
}

func (m *mountsReader) Close() error {
	m.Reset()
	return nil
}

func (u *userMountsImpl) Put(ctx context.Context, mnts Mounts) error {
	if u.pb == nil {
		return errors.New("publisher not configured")
	}

	if len(mnts.Mnts) == 0 {
		return errors.New("mount list empty")
	}

	buf, err := json.Marshal(mnts)
	if err != nil {
		return fmt.Errorf("failed marshaling data %w", err)
	}

	sp := splitter.NewSimpleSplitter(u.st, storage.ModePutUpload)
	addr, err := sp.Split(ctx, &mountsReader{bytes.NewBuffer(buf)}, int64(len(buf)), false)
	if err != nil {
		return fmt.Errorf("failed splitter %w", err)
	}

	err = u.pb.Put(ctx, userMounts, time.Now().Unix(), addr)
	if err != nil {
		return fmt.Errorf("failed publishing %w", err)
	}

	return nil
}
