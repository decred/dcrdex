// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// This file should compiled from the commit the file was introduced, otherwise
// it may not compile due to API changes, or may not create the database with
// the correct old version.  This file should not be updated for API changes.

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"decred.org/dcrdex/client/db/bolt"
)

const dbname = "v0.db"

func main() {
	err := setup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}
	err = compress()
	if err != nil {
		fmt.Fprintf(os.Stderr, "compress: %v\n", err)
		os.Exit(1)
	}
}

func setup() error {
	db, err := bolt.NewDB(dbname)
	if err != nil {
		return err
	}
	return db.Close()
}

func compress() error {
	db, err := os.Open(dbname)
	if err != nil {
		return err
	}
	defer os.Remove(dbname)
	defer db.Close()
	dbgz, err := os.Create(dbname + ".gz")
	if err != nil {
		return err
	}
	defer dbgz.Close()
	gz := gzip.NewWriter(dbgz)
	_, err = io.Copy(gz, db)
	if err != nil {
		return err
	}
	return gz.Close()
}
