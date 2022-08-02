package beestore_test

import (
	"context"
	"crypto/ecdsa"
	"io/ioutil"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethersphere/bee/pkg/api"
	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/logging"
	mockbatchstore "github.com/ethersphere/bee/pkg/postage/batchstore/mock"
	mockpost "github.com/ethersphere/bee/pkg/postage/mock"
	postagetesting "github.com/ethersphere/bee/pkg/postage/testing"
	statestore "github.com/ethersphere/bee/pkg/statestore/mock"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/storage/mock"
	testingc "github.com/ethersphere/bee/pkg/storage/testing"
	"github.com/ethersphere/bee/pkg/swarm"
	"github.com/ethersphere/bee/pkg/tags"
	"github.com/ethersphere/bee/pkg/tracing"
)

func TestStoreCorrectness(t *testing.T) {
	srvUrl := newTestServer(t, mock.NewStorer())

	host := srvUrl.Hostname()
	port, err := strconv.Atoi(srvUrl.Port())
	if err != nil {
		t.Fatal(err)
	}
	bId := swarm.NewAddress(postagetesting.MustNewID()).String()

	t.Run("read-write", func(t *testing.T) {
		st, err := beestore.NewBeeStore(host, port, false, false, bId, false)
		if err != nil {
			t.Fatal("failed creating new beestore")
		}

		t.Cleanup(func() {
			err := st.Close()
			if err != nil {
				t.Fatal(err)
			}
		})

		ctx := context.Background()

		for i := 0; i < 50; i++ {
			ch := testingc.GenerateTestRandomChunk()
			_, err := st.Put(ctx, storage.ModePutUpload, ch)
			if err != nil {
				t.Fatal(err)
			}
			chResult, err := st.Get(ctx, storage.ModeGetRequest, ch.Address())
			if err != nil {
				t.Fatal(err)
			}
			if !ch.Equal(chResult) {
				t.Fatal("chunk mismatch")
			}
		}
	})

	t.Run("read-only", func(t *testing.T) {
		st, err := beestore.NewBeeStore(host, port, false, false, bId, true)
		if err != nil {
			t.Fatal("failed creating new beestore")
		}

		t.Cleanup(func() {
			err := st.Close()
			if err != nil {
				t.Fatal(err)
			}
		})

		ch := testingc.GenerateTestRandomChunk()
		_, err = st.Put(context.TODO(), storage.ModePutUpload, ch)
		if err == nil {
			t.Fatal("expected error while putting")
		}
	})
}

// newTestServer creates an http server to serve the bee http api endpoints.
func newTestServer(t *testing.T, storer storage.Storer) *url.URL {
	t.Helper()
	logger := logging.New(ioutil.Discard, 0)
	store := statestore.NewStateStore()
	pk, _ := crypto.GenerateSecp256k1Key()
	signer := crypto.NewDefaultSigner(pk)
	batchStore := mockbatchstore.New(mockbatchstore.WithAcceptAllExistsFunc())

	var (
		dummyKey   ecdsa.PublicKey
		dummyOwner common.Address
	)

	s := api.New(dummyKey, dummyKey, dummyOwner, logger, nil, batchStore, false, api.FullMode, true, true, nil)

	var extraOpts = api.ExtraOptions{
		Tags:   tags.NewTags(store, logger),
		Storer: storer,
		Post:   mockpost.New(mockpost.WithAcceptAll()),
	}

	noOpTracer, tracerCloser, _ := tracing.NewTracer(&tracing.Options{
		Enabled: false,
	})

	t.Cleanup(func() { _ = tracerCloser.Close() })

	_ = s.Configure(signer, nil, noOpTracer, api.Options{}, extraOpts, 10, nil, nil)

	s.MountAPI()

	ts := httptest.NewServer(s)
	srvUrl, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	return srvUrl
}
