package main

import (
	"errors"
	"fmt"

	"github.com/aloknerurkar/bee-afs/pkg/mounts"
	"github.com/urfave/cli/v2"
)

func doCreate(c *cli.Context) error {
	if c.NArg() != 1 {
		return errors.New("incorrect arguments")
	}

	st, err := getBeeStore(c)
	if err != nil {
		return err
	}
	defer st.Close()

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
	if found {
		return fmt.Errorf("mount %s already exists", c.Args().Get(0))
	}

	mntList = append(mntList, c.Args().Get(0))

	err = mnts.Put(c.Context, mntList)
	if err != nil {
		return fmt.Errorf("failed updating mount list %w", err)
	}

	return nil
}
