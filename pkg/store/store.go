package store

import "github.com/ethersphere/bee/pkg/storage"

type PutGetter interface {
	storage.Putter
	storage.Getter
}
