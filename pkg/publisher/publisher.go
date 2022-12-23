package publisher

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"

	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/feeds"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
	logger "github.com/ipfs/go-log/v2"
)

var log = logger.Logger("publisher")

type Publisher interface {
	Put(ctx context.Context, id string, version int64, ref swarm.Address) error
}

type Loader func(ctx context.Context, id string) (feeds.Index, int64, error)

type pubImpl struct {
	putter     storage.Putter
	signer     crypto.Signer
	loader     Loader
	updaterMap sync.Map
}

type feedState struct {
	currIndex feeds.Index
	ts        int64
}

func New(putter storage.Putter, signer crypto.Signer, loader Loader) Publisher {
	return &pubImpl{putter: putter, signer: signer, loader: loader}
}

func (p *pubImpl) Put(ctx context.Context, id string, version int64, ref swarm.Address) error {
	var nxtIndex feeds.Index

	state, found := p.updaterMap.Load(id)
	if !found {
		currIndex, at, err := p.loader(ctx, id)
		if err == nil {
			log.Infof("publisher: loaded initial version %s timestamp %d", currIndex, at)
			nxtIndex = currIndex.Next(at, uint64(version))
		} else {
			nxtIndex = new(index)
		}
	} else {
		fstate := state.(feedState)
		nxtIndex = fstate.currIndex.Next(fstate.ts, uint64(version))
	}

	err := p.update(ctx, id, version, nxtIndex, ref.Bytes())
	if err != nil {
		return fmt.Errorf("publisher: failed to update next index: %w", err)
	}

	p.updaterMap.Store(id, feedState{currIndex: nxtIndex, ts: version})

	log.Debugf("updated id %s version %d ref %s", id, version, ref.String())

	return nil
}

func (p *pubImpl) update(
	ctx context.Context,
	id string,
	version int64,
	idx feeds.Index,
	payload []byte,
) error {
	putter, err := feeds.NewPutter(p.putter, p.signer, []byte(id))
	if err != nil {
		return err
	}

	err = putter.Put(ctx, idx, version, payload)
	if err != nil {
		return err
	}

	return nil
}

// index replicates the feeds.sequence.Index. This index creation is not exported from
// the package as a result loader doesn't know how to return the first feeds.Index.
// This index will be used to return which will provide a compatible interface to
// feeds.sequence.Index.
type index struct {
	index uint64
}

func (i *index) String() string {
	return strconv.FormatUint(i.index, 10)
}

func (i *index) MarshalBinary() ([]byte, error) {
	indexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBytes, i.index)
	return indexBytes, nil
}

func (i *index) Next(last int64, at uint64) feeds.Index {
	return &index{i.index + 1}
}
