package main

import (
	"fmt"

	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/urfave/cli/v2"
)

func doMountList(c *cli.Context) error {
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
	fmt.Printf("Found %d mounts for user:\n", len(mntList))
	for _, m := range mntList {
		fmt.Println(m)
	}

	return nil
}
