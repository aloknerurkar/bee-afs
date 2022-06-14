package beestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/swarm"
	"github.com/gorilla/websocket"
)

var successWsMsg = []byte{}

// BeeStore provies a storage.PutGetter that adds/retrieves chunks to/from swarm through the HTTP chunk API.
type BeeStore struct {
	Client    *http.Client
	baseUrl   string
	tagUrl    string
	streamUrl string
	batch     string
	cancel    context.CancelFunc
	opChan    chan putOp
	wg        sync.WaitGroup
}

// NewBeeStore creates a new APIStore.
func NewBeeStore(host string, port int, tls bool, batch string) (*BeeStore, error) {
	scheme := "http"
	if tls {
		scheme += "s"
	}
	u := &url.URL{
		Host:   fmt.Sprintf("%s:%d", host, port),
		Scheme: scheme,
		Path:   "chunks",
	}
	t := &url.URL{
		Host:   fmt.Sprintf("%s:%d", host, port),
		Scheme: scheme,
		Path:   "tags",
	}
	st := &url.URL{
		Host:   fmt.Sprintf("%s:%d", host, port),
		Scheme: "ws",
		Path:   "chunks/stream",
	}

	ctx, cancel := context.WithCancel(context.Background())
	b := &BeeStore{
		Client:    http.DefaultClient,
		baseUrl:   u.String(),
		tagUrl:    t.String(),
		streamUrl: st.String(),
		batch:     batch,
		cancel:    cancel,
		opChan:    make(chan putOp),
	}

	err := b.putWorker(ctx)
	if err != nil {
		return nil, err
	}

	return b, nil
}

type putOp struct {
	ch   swarm.Chunk
	errc chan<- error
}

func (b *BeeStore) putWorker(ctx context.Context) error {
	reqHeader := http.Header{}
	reqHeader.Set("Content-Type", "application/octet-stream")
	reqHeader.Set("Swarm-Postage-Batch-Id", b.batch)

	dialer := websocket.Dialer{
		ReadBufferSize:  swarm.ChunkSize,
		WriteBufferSize: swarm.ChunkSize,
	}

	conn, _, err := dialer.DialContext(ctx, b.streamUrl, reqHeader)
	if err != nil {
		return fmt.Errorf("failed creating connection %s: %w", b.streamUrl, err)
	}

	var wsErr string
	serverClosed := make(chan struct{})
	conn.SetCloseHandler(func(code int, text string) error {
		wsErr = fmt.Sprintf("websocket connection closed code:%d msg:%s", code, text)
		close(serverClosed)
		return nil
	})

	b.wg.Add(1)
	go func() {
		defer conn.Close()
		defer b.wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case op, more := <-b.opChan:
				if !more {
					return
				}
				select {
				case <-serverClosed:
					return
				case <-ctx.Done():
					return
				default:
				}
				err := conn.SetWriteDeadline(time.Now().Add(time.Second * 4))
				if err != nil {
					op.errc <- fmt.Errorf("failed setting write deadline %w", err)
					continue
				}
				err = conn.WriteMessage(websocket.BinaryMessage, op.ch.Data())
				if err != nil {
					op.errc <- fmt.Errorf("failed writing message %w", err)
					continue
				}
				err = conn.SetReadDeadline(time.Now().Add(time.Second * 4))
				if err != nil {
					// server sent close message with error
					if wsErr != "" {
						op.errc <- errors.New(wsErr)
						continue
					}
					op.errc <- fmt.Errorf("failed setting read deadline %w", err)
					continue
				}
				mt, msg, err := conn.ReadMessage()
				if err != nil {
					op.errc <- fmt.Errorf("failed reading message %w", err)
					continue
				}
				if mt != websocket.BinaryMessage || !bytes.Equal(successWsMsg, msg) {
					op.errc <- errors.New("invalid msg returned on success")
					continue
				}
				op.errc <- nil
			}
		}
	}()
	return nil
}

func (b *BeeStore) Put(ctx context.Context, mode storage.ModePut, chs ...swarm.Chunk) (exist []bool, err error) {
	for _, ch := range chs {
		errc := make(chan error, 1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case b.opChan <- putOp{ch: ch, errc: errc}:
		}

		select {
		case err := <-errc:
			if err != nil {
				return nil, fmt.Errorf("failed putting chunk %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return make([]bool, len(chs)), nil
}

func (b *BeeStore) Get(ctx context.Context, mode storage.ModeGet, address swarm.Address) (ch swarm.Chunk, err error) {
	addressHex := address.String()
	url := strings.Join([]string{b.baseUrl, addressHex}, "/")
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed creating http req %w", err)
	}
	res, err := b.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed executing http req %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chunk %s not found", addressHex)
	}
	defer res.Body.Close()
	chunkData, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading chunk body %w", err)
	}
	ch = swarm.NewChunk(address, chunkData)
	return ch, nil
}

func (b *BeeStore) Close() error {
	b.cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("closing beestore with ongoing routines")
	}
}
