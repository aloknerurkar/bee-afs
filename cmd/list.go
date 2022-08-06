package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cheynewallace/tabby"
	"github.com/urfave/cli/v2"
)

func doMountList(c *cli.Context) error {
	mnts, done, err := getMounts(c, true)
	if err != nil {
		return err
	}
	defer done()

	mntList, err := mnts.Get(c.Context)
	if err != nil {
		return fmt.Errorf("failed getting mounts for user %w", err)
	}

	if len(mntList.Mnts) == 0 {
		fmt.Println("No mounts found for user")
		return nil
	}
	fmt.Printf("Found %d mounts for user:\n", len(mntList.Mnts))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	t := tabby.NewCustom(w)
	t.AddHeader("NAME", "CREATED", "POSTAGE BATCH", "ENCRYPTED", "PINNED")

	for _, m := range mntList.Mnts {
		t.AddLine(m.Name, time.Unix(m.Created, 0).String(), m.Batch, m.Encrypt, m.Pin)
	}
	t.Print()

	return nil
}
