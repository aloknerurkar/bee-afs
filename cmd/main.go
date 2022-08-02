package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aloknerurkar/bee-afs/pkg/lookuper"
	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/aloknerurkar/bee-afs/pkg/publisher"
	"github.com/aloknerurkar/bee-afs/pkg/store"
	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/keystore"
	filekeystore "github.com/ethersphere/bee/pkg/keystore/file"
	memkeystore "github.com/ethersphere/bee/pkg/keystore/mem"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

var commonFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "config",
		Aliases: []string{"c"},
		Usage:   "Load configuration from `FILE`",
		EnvVars: []string{"BEEAFS_CONFIG"},
	},
	altsrc.NewStringFlag(&cli.StringFlag{Name: "swarm-key", Usage: "path to swarm-key file"}),
	altsrc.NewStringFlag(&cli.StringFlag{Name: "password", Usage: "password for swarm-key file"}),
	altsrc.NewStringFlag(&cli.StringFlag{Name: "api-host", DefaultText: "http://localhost"}),
	altsrc.NewIntFlag(&cli.IntFlag{Name: "api-port", DefaultText: "1633"}),
	altsrc.NewStringFlag(&cli.StringFlag{Name: "root-batch", Usage: "batch to use for user metadata"}),
}

func main() {
	app := &cli.App{
		Name:  "bee-afs",
		Usage: "Provides filesystem abstraction for Swarm decentralized storage",
		Commands: []*cli.Command{
			{
				Name:    "create",
				Aliases: []string{"c"},
				Usage:   "Create a new FUSE Filesystem mount on Swarm",
				Before:  altsrc.InitInputSourceWithContext(commonFlags, altsrc.NewYamlSourceFromFlagFunc("config")),
				Flags:   append(commonFlags, createFlags...),
				Action:  doCreate,
			},
			{
				Name:    "list",
				Aliases: []string{"l"},
				Usage:   "List mounts configured on Swarm",
				Before:  altsrc.InitInputSourceWithContext(commonFlags, altsrc.NewYamlSourceFromFlagFunc("config")),
				Flags:   commonFlags,
				Action:  doMountList,
			},
			{
				Name:    "mount",
				Aliases: []string{"m"},
				Usage:   "Mount a FUSE Filesystem on Swarm locally",
				Before:  altsrc.InitInputSourceWithContext(commonFlags, altsrc.NewYamlSourceFromFlagFunc("config")),
				Flags:   append(commonFlags, mountFlags...),
				Action:  doMount,
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
	return lookuper.New(b, owner), publisher.New(b, signer), nil
}

func getMetadataBeeStore(c *cli.Context, readOnly bool) (store.PutGetter, error) {
	bStore, err := beestore.NewBeeStore(
		c.String("api-host"),
		c.Int("api-port"),
		false,
		true,
		c.String("root-batch"),
		readOnly,
	)
	if err != nil {
		return nil, err
	}
	return bStore, nil
}

func getMounts(c *cli.Context, readOnly bool) (mounts.UserMounts, func(), error) {
	st, err := getMetadataBeeStore(c, false)
	if err != nil {
		return nil, func() {}, err
	}

	lk, pb, err := getLookuperPublisher(c, st)
	if err != nil {
		return nil, func() {}, err
	}

	if readOnly {
		return mounts.NewReadOnly(lk, st), func() { st.Close() }, nil
	}

	return mounts.New(lk, pb, st), func() { st.Close() }, nil
}
