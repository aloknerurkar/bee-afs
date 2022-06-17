package beestore_test

import (
	"context"
	"io/ioutil"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
	"github.com/ethersphere/bee/pkg/api"
	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/logging"
	mockpost "github.com/ethersphere/bee/pkg/postage/mock"
	postagetesting "github.com/ethersphere/bee/pkg/postage/testing"
	statestore "github.com/ethersphere/bee/pkg/statestore/mock"
	"github.com/ethersphere/bee/pkg/storage"
	"github.com/ethersphere/bee/pkg/storage/mock"
	testingc "github.com/ethersphere/bee/pkg/storage/testing"
	"github.com/ethersphere/bee/pkg/swarm"
	"github.com/ethersphere/bee/pkg/tags"
)

func TestStoreCorrectness(t *testing.T) {
	srvUrl := newTestServer(t, mock.NewStorer())

	host := srvUrl.Hostname()
	port, err := strconv.Atoi(srvUrl.Port())
	if err != nil {
		t.Fatal(err)
	}
	bId := swarm.NewAddress(postagetesting.MustNewID()).String()
	st, err := beestore.NewBeeStore(host, port, false, false, bId)
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
}

// newTestServer creates an http server to serve the bee http api endpoints.
func newTestServer(t *testing.T, storer storage.Storer) *url.URL {
	t.Helper()
	logger := logging.New(ioutil.Discard, 0)
	store := statestore.NewStateStore()
	pk, _ := crypto.GenerateSecp256k1Key()
	signer := crypto.NewDefaultSigner(pk)
	s, _ := api.New(
		tags.NewTags(store, logger),
		storer,
		nil, nil, nil, nil, nil,
		mockpost.New(mockpost.WithAcceptAll()),
		nil, nil,
		signer,
		nil,
		logger,
		nil,
		api.Options{},
	)
	ts := httptest.NewServer(s)
	srvUrl, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	return srvUrl
}
