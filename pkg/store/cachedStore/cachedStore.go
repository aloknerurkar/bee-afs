package cachedStore

import (
	"context"
	"fmt"

	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
	lru "github.com/hashicorp/golang-lru"
)

type cachedStore struct {
	store.PutGetter
	cache *lru.Cache
}

func New(st store.PutGetter) (*cachedStore, error) {
	cache, err := lru.New(10000)
	if err != nil {
		return nil, fmt.Errorf("failed creating cache %w", err)
	}
	return &cachedStore{PutGetter: st, cache: cache}, nil
}

func (c *cachedStore) Get(ctx context.Context, md storage.ModeGet, address swarm.Address) (ch swarm.Chunk, err error) {
	chEntry, found := c.cache.Get(address.ByteString())
	if !found {
		ch, err = c.PutGetter.Get(ctx, md, address)
		if err == nil {
			_ = c.cache.Add(address.ByteString(), ch)
		}
	} else {
		ch = chEntry.(swarm.Chunk)
	}
	return
}

func (c *cachedStore) Put(ctx context.Context, md storage.ModePut, chs ...swarm.Chunk) (exist []bool, err error) {
	_, err = c.PutGetter.Put(ctx, md, chs...)
	if err == nil {
		for _, ch := range chs {
			_ = c.cache.Add(ch.Address().ByteString(), ch)
		}
	}
	return
}
