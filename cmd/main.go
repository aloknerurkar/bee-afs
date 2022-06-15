package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/aloknerurkar/bee-afs/pkg/store/beestore"
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
		altsrc.NewStringFlag(&cli.StringFlag{Name: "swarm-key", Usage: "path to swarm-key file", Required: true}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "password", Usage: "password for swarm-key file", Required: true}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "api-host", DefaultText: "http://localhost"}),
		altsrc.NewIntFlag(&cli.IntFlag{Name: "api-port", DefaultText: "1633"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "postage-batch", Required: true}),
		altsrc.NewBoolFlag(&cli.BoolFlag{Name: "pin"}),
		altsrc.NewBoolFlag(&cli.BoolFlag{Name: "encrypt"}),
	}

	app := &cli.App{
		Name:   "bee-afs",
		Usage:  "Provides filesystem abstraction for Swarm decentralized storage",
		Before: altsrc.InitInputSourceWithContext(confFlags, altsrc.NewYamlSourceFromFlagFunc("config")),
		Flags:  confFlags,
		Commands: []*cli.Command{
			{
				Name:    "mount",
				Aliases: []string{"m"},
				Usage:   "Mount a FUSE Filesystem on Swarm",
				Subcommands: []*cli.Command{
					{
						Name: "list",
						Action: func(c *cli.Context) error {
							var mnts mounts.UserMounts
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
						Name: "create",
						Action: func(c *cli.Context) error {
							if c.NArg() != 2 {
								return errors.New("incorrect arguments")
							}

							var mnts mounts.UserMounts
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

							return nil
						},
					},
				},
			},
		},
	}
}

func getSigner(c *cli.Context) (crypto.Signer, error) {
	keystore := filekeystore.New(filepath.Dir(c.String("swarm-key")))
	pk, _, err := keystore.Key("swarm", c.String("password"))
	if err != nil {
		return nil, err
	}
	return crypto.NewDefaultSigner(pk), nil
}

func getBeeStore(c *cli.Context) (*beestore.BeeStore, error) {
	return beestore.NewBeeStore(
		c.String("api-host"),
		c.Int("api-port"),
		false,
		c.String("postage-batch"),
	)
}
