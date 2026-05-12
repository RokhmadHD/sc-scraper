package main

import (
	"flag"
	"fmt"
	"os"

	"scrape-smart-contract/internal/chains"
)

func main() {
	input := flag.String("input", "/tmp/chains.json", "source Chainlist chains.json path")
	output := flag.String("output", "chains", "output chains directory")
	flag.Parse()

	count, err := chains.SyncFromFile(*input, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("Wrote %d filtered chain configs to %s\n", count, *output)
}
