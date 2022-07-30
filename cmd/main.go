package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aloknerurkar/bee-afs/pkg/cached"
	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
	"github.com/aloknerurkar/bee-afs/pkg/store/cachedStore"
	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/keystore"
	filekeystore "github.com/ethersphere/bee/pkg/keystore/file"
	memkeystore "github.com/ethersphere/bee/pkg/keystore/mem"
	"github.com/ethersphere/bee/pkg/storage/mock"
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
		&cli.BoolFlag{Name: "debug", Usage: "enable all logs"},
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
						Action: doMountList,
					},
					{
						Name:   "create",
						Before: altsrc.InitInputSourceWithContext(confFlags, altsrc.NewYamlSourceFromFlagFunc("config")),
						Flags:  confFlags,
						Action: doMountCreate,
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
	var keystore keystore.Service
	if c.String("swarm-key") == "" {
		keystore = memkeystore.New()
	} else {
		keystore = filekeystore.New(filepath.Dir(c.String("swarm-key")))
	}
	pk, _, err := keystore.Key("swarm", c.String("password"))
	if err != nil {
		return nil, fmt.Errorf("failed reading swarm key %w", err)
	}
	return crypto.NewDefaultSigner(pk), nil
}

func getLookuperPublisher(c *cli.Context, b store.PutGetter) (lookuper.Lookuper, publisher.Publisher, error) {
	signer, err := getSigner(c)
	if err != nil {
		return nil, nil, err
	}
	owner, err := signer.EthereumAddress()
	if err != nil {
		return nil, nil, fmt.Errorf("failed getting owner eth address %w", err)
	}
	cachedLkPb, err := cached.New(lookuper.New(b, owner), publisher.New(b, signer), 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating cached lookup and publisher %w", err)
	}
	return cachedLkPb, cachedLkPb, nil
}

func getBeeStore(c *cli.Context) (store.PutGetter, error) {
	if c.Bool("inmem") {
		return mock.NewStorer(), nil
	}
	bStore, err := beestore.NewBeeStore(
		c.String("api-host"),
		c.Int("api-port"),
		false,
		c.Bool("pin"),
		c.String("postage-batch"),
	)
	if err != nil {
		return nil, err
	}
	return cachedStore.New(bStore)
}
