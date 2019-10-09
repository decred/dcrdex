package driver

import (
	"context"

	"github.com/decred/dcrdex/server/db"
)

type Driver interface {
	Open(ctx context.Context, cfg interface{}) (db.DEXArchivist, error)
}
