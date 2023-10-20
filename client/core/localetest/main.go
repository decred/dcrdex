package main

import (
	"fmt"

	"decred.org/dcrdex/client/core"
)

func main() {
	fmt.Printf("Missing %d translations \n", core.CheckTopicLangs())
}
