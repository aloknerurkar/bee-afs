package feedstore_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"io/ioutil"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/aloknerurkar/bee-afs/pkg/store/feedstore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethersphere/bee/pkg/api"
	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/logging"
	mockbatchstore "github.com/ethersphere/bee/pkg/postage/batchstore/mock"
	mockpost "github.com/ethersphere/bee/pkg/postage/mock"
	postagetesting "github.com/ethersphere/bee/pkg/postage/testing"
	soctesting "github.com/ethersphere/bee/pkg/soc/testing"
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
	sch := soctesting.GenerateMockSOC(t, []byte("dummy"))

	st, err := feedstore.NewFeedStore(host, port, false, false, bId, hex.EncodeToString(sch.Owner))
	if err != nil {
		t.Fatal("failed creating new beestore")
	}

	t.Run("soc chunk", func(t *testing.T) {
		_, err = st.Put(context.TODO(), storage.ModePutUpload, sch.Chunk())
		if err != nil {
			t.Fatal(err)
		}
		chResult, err := st.Get(context.TODO(), storage.ModeGetRequest, sch.Address())
		if err != nil {
			t.Fatal(err)
		}
		if !sch.Chunk().Equal(chResult) {
			t.Fatal("chunk mismatch")
		}
	})

	t.Run("cac chunk fails", func(t *testing.T) {
		ch := testingc.GenerateTestRandomChunk()
		_, err := st.Put(context.TODO(), storage.ModePutUpload, ch)
		if err == nil {
			t.Fatal("expected failure for cac")
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
