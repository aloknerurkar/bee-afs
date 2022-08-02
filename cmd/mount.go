package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aloknerurkar/bee-afs/pkg/cached"
	fs "github.com/aloknerurkar/bee-afs/pkg/fuse"
	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
	"github.com/aloknerurkar/bee-afs/pkg/store/cachedStore"
	"github.com/billziss-gh/cgofuse/fuse"
	logger "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
)

var mountFlags = []cli.Flag{
	&cli.BoolFlag{Name: "debug", Usage: "enable all logs"},
}

func doMount(c *cli.Context) error {
	if c.NArg() != 2 {
		return errors.New("incorrect arguments")
	}

	if c.Bool("debug") {
		logger.SetLogLevel("*", "debug")
	}

	mnts, done, err := getMounts(c, true)
	if err != nil {
		return err
	}
	defer done()

	mntList, err := mnts.Get(c.Context)
	if err != nil {
		return fmt.Errorf("failed getting mounts for user %w", err)
	}

	found := false
	foundIdx := 0
	for idx, m := range mntList.Mnts {
		if m.Name == c.Args().Get(0) {
			found = true
			foundIdx = idx
			break
		}
	}
	if !found {
		return fmt.Errorf("mount %s not found", c.Args().Get(0))
	}

	st, err := beestore.NewBeeStore(
		c.String("api-host"),
		c.Int("api-port"),
		false,
		mntList.Mnts[foundIdx].Pin,
		mntList.Mnts[foundIdx].Batch,
		false,
	)
	if err != nil {
		return err
	}
	defer st.Close()

	cStore, err := cachedStore.New(st)
	if err != nil {
		return err
	}

	lk, pb, err := getCachedLookuperPublisher(c, cStore)
	if err != nil {
		return err
	}

	opts := []fs.Option{fs.WithNamespace(c.Args().Get(0))}
	if c.Bool("encrypt") {
		opts = append(opts, fs.WithEncryption(mntList.Mnts[foundIdx].Encrypt))
	}

	fsImpl, err := fs.New(cStore, lk, pb, opts...)
	if err != nil {
		return fmt.Errorf("failed creating new fs %w", err)
	}

	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, syscall.SIGINT, syscall.SIGTERM)

	srv := fuse.NewFileSystemHost(fsImpl)
	srv.SetCapReaddirPlus(true)

	var fuseArgs []string
	if c.Bool("debug") {
		fuseArgs = []string{"-d"}
	}
	stopped := make(chan struct{})
	go func() {
		if !srv.Mount(c.Args().Get(1), fuseArgs) {
			close(stopped)
		}
	}()

	select {
	case <-stopped:
		return errors.New("fuse mount stopped")
	case <-interruptChannel:
		fmt.Println("Received stop signal...")
		srv.Unmount()
	}

	return nil
}

func getCachedLookuperPublisher(c *cli.Context, b store.PutGetter) (lookuper.Lookuper, publisher.Publisher, error) {
	lk, pb, err := getLookuperPublisher(c, b)
	if err != nil {
		return nil, nil, err
	}
	cachedLkPb, err := cached.New(lk, pb, 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating cached lookup and publisher %w", err)
	}
	return cachedLkPb, cachedLkPb, nil
}
