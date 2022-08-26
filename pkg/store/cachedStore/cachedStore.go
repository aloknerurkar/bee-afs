package cachedStore

import (
	"context"
	"fmt"
	"sync"

	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
	lru "github.com/hashicorp/golang-lru"
	logger "github.com/ipfs/go-log/v2"
)

var log = logger.Logger("cachedStore")

type cachedStore struct {
	store.PutGetter
	cache *lru.Cache
	mtx   sync.RWMutex
}

func New(st store.PutGetter) (*cachedStore, error) {
	cache, err := lru.New(100000)
	if err != nil {
		return nil, fmt.Errorf("failed creating cache %w", err)
	}
	return &cachedStore{PutGetter: st, cache: cache}, nil
}

func (c *cachedStore) Get(ctx context.Context, md storage.ModeGet, address swarm.Address) (ch swarm.Chunk, err error) {
	c.mtx.RLock()
	chEntry, found := c.cache.Get(address.ByteString())
	c.mtx.RUnlock()
	if !found {
		ch, err = c.PutGetter.Get(ctx, md, address)
		if err == nil {
			c.mtx.Lock()
			_ = c.cache.Add(address.ByteString(), ch)
			c.mtx.Unlock()
			log.Debugf("adding chunk to cache %s", ch.Address().String())
		}
	} else {
		ch = chEntry.(swarm.Chunk)
		log.Debugf("found chunk in cache %s", ch.Address().String())
	}
	return
}

func (c *cachedStore) Put(ctx context.Context, md storage.ModePut, chs ...swarm.Chunk) (exist []bool, err error) {
	_, err = c.PutGetter.Put(ctx, md, chs...)
	if err == nil {
		for _, ch := range chs {
			c.mtx.Lock()
			_ = c.cache.Add(ch.Address().ByteString(), ch)
			c.mtx.Unlock()
			log.Debugf("adding chunk to cache %s", ch.Address().String())
		}
	}
	return
}
