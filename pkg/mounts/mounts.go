package mounts

import "context"

type UserMounts interface {
	Get(ctx context.Context) ([]string, error)
	Put(ctx context.Context, mnts []string) error
}
