package cached

import (
	"context"
	"time"

	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/ethersphere/bee/pkg/swarm"
	lru "github.com/hashicorp/golang-lru"
)

type cachedLookuperPublisher struct {
	lookuper.Lookuper
	publisher.Publisher

	timeout time.Duration
	cached  *lru.Cache
}

type cachedResult struct {
	ref swarm.Address
	err error
	ts  int64
}

func New(lk lookuper.Lookuper, pb publisher.Publisher, timeout time.Duration) (*cachedLookuperPublisher, error) {
	cache, err := lru.New(10000)
	if err != nil {
		return nil, err
	}
	return &cachedLookuperPublisher{
		Lookuper:  lk,
		Publisher: pb,
		timeout:   timeout,
		cached:    cache,
	}, nil
}

func (c *cachedLookuperPublisher) Get(ctx context.Context, id string, version int64) (swarm.Address, error) {
	cRef, found := c.cached.Get(id)
	if found {
		if time.Since(time.Unix(cRef.(cachedResult).ts, 0)) > 3*time.Second {
			go func() {
				ref, err := c.get(context.Background(), id, version)
				_ = c.cached.Add(id, cachedResult{ref: ref, err: err, ts: time.Now().Unix()})
			}()
		}
		res := cRef.(cachedResult)
		return res.ref, res.err
	}
	return c.get(ctx, id, version)
}

func (c *cachedLookuperPublisher) get(ctx context.Context, id string, version int64) (swarm.Address, error) {
	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	return c.Lookuper.Get(cctx, id, version)
}

func (c *cachedLookuperPublisher) Put(ctx context.Context, id string, version int64, ref swarm.Address) error {
	err := c.Publisher.Put(ctx, id, version, ref)
	if err == nil {
		_ = c.cached.Add(id, cachedResult{ref: ref, ts: time.Now().Unix()})
	}
	return err
}
