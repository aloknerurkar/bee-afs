package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	fs "github.com/aloknerurkar/bee-afs/pkg/fuse"
	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/ethersphere/bee/pkg/crypto"
	filekeystore "github.com/ethersphere/bee/pkg/keystore/file"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func main() {

	confFlags := []cli.Flag{
		&cli.StringFlag{
			Name:    "config",
			Aliases: []string{"c"},
			Usage:   "Load configuration from `FILE`",
			EnvVars: []string{"BEEAFS_CONFIG"},
		},
		&cli.BoolFlag{Name: "inmem", Usage: "use inmem storage for testing"},
		altsrc.NewStringFlag(&cli.StringFlag{Name: "swarm-key", Usage: "path to swarm-key file"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "password", Usage: "password for swarm-key file"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "api-host", DefaultText: "http://localhost"}),
		altsrc.NewIntFlag(&cli.IntFlag{Name: "api-port", DefaultText: "1633"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "postage-batch"}),
		altsrc.NewBoolFlag(&cli.BoolFlag{Name: "pin"}),
		altsrc.NewBoolFlag(&cli.BoolFlag{Name: "encrypt"}),
	}

	app := &cli.App{
		Name:  "bee-afs",
		Usage: "Provides filesystem abstraction for Swarm decentralized storage",
		Commands: []*cli.Command{
			{
				Name:    "mount",
				Aliases: []string{"m"},
				Usage:   "Mount a FUSE Filesystem on Swarm",
				Subcommands: []*cli.Command{
					{
						Name:   "list",
						Before: altsrc.InitInputSourceWithContext(confFlags, altsrc.NewYamlSourceFromFlagFunc("config")),
						Flags:  confFlags,
						Action: func(c *cli.Context) error {
							st, err := getBeeStore(c)
							if err != nil {
								return err
							}

							lk, pb, err := getLookuperPublisher(c, st)
							if err != nil {
								return err
							}

							mnts := mounts.New(lk, pb, st)
							mntList, err := mnts.Get(c.Context)
							if err != nil {
								return fmt.Errorf("failed getting mounts for user %w", err)
							}

							if len(mntList) == 0 {
								fmt.Println("No mounts found for user")
								return nil
							}
							fmt.Printf("Found %s mounts for user:\n", len(mntList))
							for _, m := range mntList {
								fmt.Println(m)
							}

							return nil
						},
					},
					{
						Name:   "create",
						Before: altsrc.InitInputSourceWithContext(confFlags, altsrc.NewYamlSourceFromFlagFunc("config")),
						Flags:  confFlags,
						Action: func(c *cli.Context) error {
							if c.NArg() != 2 {
								return errors.New("incorrect arguments")
							}

							st, err := getBeeStore(c)
							if err != nil {
								return err
							}

							lk, pb, err := getLookuperPublisher(c, st)
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
								err := mnts.Put(c.Context, append(mntList, c.Args().Get(0)))
								if err != nil {
									return fmt.Errorf("failed adding new mount %w", err)
								}
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
							}

							return nil
						},
					},
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Printf("failed err: %s\n", err.Error())
	}
}

func getSigner(c *cli.Context) (crypto.Signer, error) {
	keystore := filekeystore.New(filepath.Dir(c.String("swarm-key")))
	pk, _, err := keystore.Key("swarm", c.String("password"))
	if err != nil {
		return nil, fmt.Errorf("failed reading swarm key %w", err)
	}
	return crypto.NewDefaultSigner(pk), nil
}

func getLookuperPublisher(c *cli.Context, b *beestore.BeeStore) (lookuper.Lookuper, publisher.Publisher, error) {
	signer, err := getSigner(c)
	if err != nil {
		return nil, nil, err
	}
	owner, err := signer.EthereumAddress()
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting owner eth address %w", err)
	}
	return lookuper.New(b, owner), publisher.New(b, signer), nil
}

func getBeeStore(c *cli.Context) (*beestore.BeeStore, error) {
	return beestore.NewBeeStore(
		c.String("api-host"),
		c.Int("api-port"),
		false,
		c.String("postage-batch"),
	)
}
