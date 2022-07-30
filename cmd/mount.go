package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	fs "github.com/aloknerurkar/bee-afs/pkg/fuse"
	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/billziss-gh/cgofuse/fuse"
	logger "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
)

func doMount(c *cli.Context) error {
	if c.NArg() != 2 {
		fmt.Println(c.Args().Get(0))
		return errors.New("incorrect arguments")
	}

	if c.Bool("debug") {
		logger.SetLogLevel("*", "debug")
	}

	st, err := getCachedBeeStore(c)
	if err != nil {
		return err
	}
	defer st.Close()

	lk, pb, err := getCachedLookuperPublisher(c, st)
	if err != nil {
		return err
	}

	mnts := mounts.New(lk, pb, st)
	mntList, err := mnts.Get(c.Context)
	if err != nil {
		return fmt.Errorf("failed getting mounts for user %w", err)
	}

	found := false
	for _, m := range mntList {
		if m == c.Args().Get(0) {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("mount %s not found", c.Args().Get(0))
	}

	opts := []fs.Option{fs.WithNamespace(c.Args().Get(0))}
	if c.Bool("encrypt") {
		opts = append(opts, fs.WithEncryption(true))
	}

	fsImpl, err := fs.New(st, lk, pb, opts...)
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
