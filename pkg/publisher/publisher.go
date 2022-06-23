package publisher

import (
	"context"
	"fmt"
	"sync"

	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/feeds"
	"github.com/ethersphere/bee/pkg/feeds/sequence"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
	logger "github.com/ipfs/go-log/v2"
)

var log = logger.Logger("lookuper")

type Publisher interface {
	Put(ctx context.Context, id string, version int64, ref swarm.Address) error
}

type pubImpl struct {
	putter     storage.Putter
	signer     crypto.Signer
	updaterMap sync.Map
}

func New(putter storage.Putter, signer crypto.Signer) Publisher {
	return &pubImpl{putter: putter, signer: signer}
}

func (p *pubImpl) Put(ctx context.Context, id string, version int64, ref swarm.Address) error {
	upd, err := p.updater(id)
	if err != nil {
		return err
	}

	log.Debugf("updating id %s version %d ref %s", id, version, ref.String())

	return upd.Update(ctx, version, ref.Bytes())
}

func (p *pubImpl) updater(id string) (feeds.Updater, error) {
	upd, found := p.updaterMap.Load(id)
	if !found {
		var err error
		upd, err = sequence.NewUpdater(p.putter, p.signer, []byte(id))
		if err != nil {
			return nil, fmt.Errorf("failed creating new updater %w", err)
		}
		p.updaterMap.Store(id, upd)
	}
	return upd.(feeds.Updater), nil
}
