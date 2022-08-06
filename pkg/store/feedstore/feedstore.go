package feedstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
	"github.com/ethersphere/bee/pkg/soc"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
)

type FeedStore struct {
	Client  *http.Client
	getter  storage.Getter
	baseUrl string
	owner   string
	batch   string
	pin     bool
}

func NewFeedStore(host string, port int, tls, pin bool, batch, owner string) (*FeedStore, error) {
	chunkGetter, err := beestore.NewBeeStore(host, port, tls, false, "", true)
	if err != nil {
		return nil, fmt.Errorf("failed creating chunk getter %w", err)
	}
	scheme := "http"
	if tls {
		scheme += "s"
	}
	u := &url.URL{
		Host:   fmt.Sprintf("%s:%d", host, port),
		Scheme: scheme,
		Path:   "soc",
	}

	return &FeedStore{
		Client:  http.DefaultClient,
		getter:  chunkGetter,
		baseUrl: u.String(),
		owner:   owner,
		batch:   batch,
		pin:     pin,
	}, nil
}

func (f *FeedStore) Get(ctx context.Context, md storage.ModeGet, address swarm.Address) (swarm.Chunk, error) {
	return f.getter.Get(ctx, md, address)
}

func (f *FeedStore) Put(ctx context.Context, md storage.ModePut, chs ...swarm.Chunk) (exists []bool, err error) {
	for _, ch := range chs {
		if !soc.Valid(ch) {
			return exists, errors.New("chunk not a single owner chunk")
		}

		err = f.putSOCChunk(ctx, ch)
		if err != nil {
			return exists, err
		}
	}
	return make([]bool, len(chs)), nil
}

func (f *FeedStore) Close() error {
	return nil
}

func (f *FeedStore) putSOCChunk(ctx context.Context, ch swarm.Chunk) error {
	chunkData := ch.Data()
	cursor := 0

	id := hex.EncodeToString(chunkData[cursor:swarm.HashSize])
	cursor += swarm.HashSize

	signature := hex.EncodeToString(chunkData[cursor : cursor+swarm.SocSignatureSize])
	cursor += swarm.SocSignatureSize

	chData := chunkData[cursor:]

	qURL, err := url.Parse(strings.Join([]string{f.baseUrl, f.owner, id}, "/"))
	if err != nil {
		return fmt.Errorf("failed parsing URL %w", err)
	}

	q := qURL.Query()
	q.Set("sig", signature)
	qURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", qURL.String(), bytes.NewBuffer(chData))
	if err != nil {
		return fmt.Errorf("failed creating HTTP req %w", err)
	}
	req.Header.Set("Swarm-Postage-Batch-Id", f.batch)
	if f.pin {
		req.Header.Set("Swarm-Pin", "true")
	}

	resp, err := f.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed executing HTTP req %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("invalid status code from response %d", resp.StatusCode)
	}

	return nil
}
