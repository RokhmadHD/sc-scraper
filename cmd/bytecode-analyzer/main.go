package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"scrape-smart-contract/internal/evm"
)

func main() {
	disasm := flag.Bool("disasm", false, "print disassembly")
	stats := flag.Bool("stats", false, "print opcode stats")
	patterns := flag.Bool("patterns", false, "detect contract patterns")
	vuln := flag.Bool("vuln", false, "run vulnerability audit")
	similar := flag.Bool("similar", false, "compute pairwise similarity")
	minScore := flag.Float64("min-score", 0.8, "minimum similarity score (0.0-1.0)")
	topN := flag.Int("top", 10, "top N opcodes to show in stats")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: bytecode-analyzer [flags] <file.evm> [file2.evm ...]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// load all files
	bytecodes := make(map[string][]byte, len(files))
	for _, f := range files {
		code, err := loadEVM(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading %s: %v\n", f, err)
			os.Exit(1)
		}
		bytecodes[f] = code
	}

	for _, f := range files {
		code := bytecodes[f]
		name := filepath.Base(f)

		fmt.Printf("=== %s (%d bytes) ===\n", name, len(code))

		if *patterns {
			found := evm.DetectPatterns(code)
			if len(found) == 0 {
				fmt.Println("Patterns: none")
			} else {
				fmt.Printf("Patterns: %s\n", strings.Join(found, ", "))
			}
		}

		if *vuln {
			findings := evm.Audit(code)
			if len(findings) == 0 {
				fmt.Println("Vulnerabilities: none")
			} else {
				fmt.Printf("Vulnerabilities (%d):\n", len(findings))
				for _, v := range findings {
					fmt.Printf("  %s\n", v)
				}
			}
		}

		if *stats {
			s := evm.Analyze(code)
			fmt.Printf("Unique opcodes: %d\n", s.UniqueOps)
			if len(s.FunctionSigs) > 0 {
				fmt.Printf("Function selectors: %s\n", strings.Join(s.FunctionSigs, " "))
			}
			fmt.Printf("Top %d opcodes:\n", *topN)
			for i, c := range s.Counts {
				if i >= *topN {
					break
				}
				fmt.Printf("  %-16s %d\n", c.Op, c.Count)
			}
		}

		if *disasm {
			insns, _ := evm.DisassembleHex(hex.EncodeToString(code))
			fmt.Println("Disassembly:")
			for _, ins := range insns {
				fmt.Printf("  %s\n", ins)
			}
		}

		fmt.Println()
	}

	if *similar && len(files) > 1 {
		fmt.Printf("=== Similarity (min=%.2f) ===\n", *minScore)
		results := evm.CompareAll(bytecodes, *minScore)
		if len(results) == 0 {
			fmt.Println("No pairs above threshold.")
		}
		for _, r := range results {
			fmt.Printf("  %.4f  %s  <->  %s\n", r.Score, filepath.Base(r.A), filepath.Base(r.B))
		}
	}
}

func loadEVM(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	clean := strings.TrimSpace(strings.TrimPrefix(string(raw), "0x"))
	return hex.DecodeString(clean)
}
