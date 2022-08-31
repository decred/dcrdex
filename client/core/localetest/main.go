package main

import (
	"fmt"
	"os"

	"decred.org/dcrdex/client/core"
)

func main() {
	missing, stale := core.CheckTopicLangs()
	if len(missing) == 0 && len(stale) == 0 {
		fmt.Println("No missing or stale notification translations!")
		os.Exit(0)
	}
	for lang, topics := range missing {
		fmt.Printf("%d missing notification translations for %v\n", len(topics), lang)
		for i := range topics {
			fmt.Printf("[%v] Translation missing for topic %v\n", lang, topics[i])
		}
	}
	for lang, topics := range stale {
		fmt.Printf("%d stale notification translations for %v\n", len(topics), lang)
		for i := range topics {
			fmt.Printf("[%v] Translation stale for topic %v\n", lang, topics[i])
		}
	}
	// os.Exit(1) // if we want this to be fatal
}
