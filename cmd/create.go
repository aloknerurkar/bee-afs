package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/urfave/cli/v2"
)

var createFlags = []cli.Flag{
	&cli.StringFlag{Name: "batch", Usage: "postage batch used for this mount", Required: true},
	&cli.BoolFlag{Name: "encrypt", Usage: "encrypt the mount"},
	&cli.BoolFlag{Name: "pin", Usage: "pin all the content locally for this mount"},
}

func doCreate(c *cli.Context) error {
	if c.NArg() != 1 {
		return errors.New("incorrect arguments")
	}

	mnts, done, err := getMounts(c, false)
	if err != nil {
		return err
	}
	defer done()

	mntList, err := mnts.Get(c.Context)
	if err != nil {
		return fmt.Errorf("failed getting mounts for user %w", err)
	}

	found := false
	for _, m := range mntList.Mnts {
		if m.Name == c.Args().Get(0) {
			found = true
		}
	}
	if found {
		return fmt.Errorf("mount %s already exists", c.Args().Get(0))
	}

	mntList.Mnts = append(mntList.Mnts, mounts.MountInfo{
		Name:    c.Args().Get(0),
		Batch:   c.String("batch"),
		Encrypt: c.Bool("encrypt"),
		Pin:     c.Bool("pin"),
		Created: time.Now().Unix(),
	})

	err = mnts.Put(c.Context, mntList)
	if err != nil {
		return fmt.Errorf("failed updating mount list %w", err)
	}

	fmt.Println("Successfully created new mount %s", c.Args().Get(0))

	return nil
}
