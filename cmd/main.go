package main

import (
	"fmt"

	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "bee-afs",
		Usage: "Provides filesystem abstraction for Swarm decentralized storage",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Load configuration from `FILE`",
				EnvVars: []string{"BEEAFS_CONFIG"},
			},
		},
		Commands: []*cli.Command{
			{
				Name:    "mount",
				Aliases: []string{"m"},
				Usage:   "Mount a FUSE Filesystem on Swarm",
				Action: func(c *cli.Context) error {
					var mnts mounts.UserMounts
					mntList, err := mnts.Get(c.Context)
					if err != nil {
						return fmt.Errorf("failed getting mounts for user %w", err)
					}

					found := false
					for _, m := range mntList {
						if m == name {
							found = true
						}
					}
					if !found {
						err := mnts.Put(c.Context, append(mntList, name))
						if err != nil {
							fmt.Errorf("failed adding new mount %w", err)
						}
					}

					return nil
				},
			},
			{
				Name:    "show-mounts",
				Aliases: []string{"sm"},
				Usage:   "Show existing mounts",
				Action: func(c *cli.Context) error {
					return nil
				},
			},
		},
	}
}
