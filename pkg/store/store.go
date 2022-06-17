package store

import (
	"io"

	"github.com/ethersphere/bee/pkg/storage"
)

type PutGetter interface {
	storage.Putter
	storage.Getter

	io.Closer
}
