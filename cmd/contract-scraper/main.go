package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"scrape-smart-contract/internal/chains"
	"scrape-smart-contract/internal/eth"
	"scrape-smart-contract/internal/rpc"
	"scrape-smart-contract/internal/scraper"
)

type multiFlag []string

func (m *multiFlag) String() string {
	return fmt.Sprint([]string(*m))
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var rpcURLs multiFlag
	chainName := flag.String("chain", "ethereum", "chain name")
	startBlock := flag.Uint64("start-block", 0, "first block to scrape")
	endBlock := flag.Uint64("end-block", 0, "last block to scrape")
	lastBlocks := flag.Uint64("last", 0, "scrape the last N blocks")
	latestOnly := flag.Bool("latest", false, "print latest block and exit")
	output := flag.String("output", "", "output JSONL path; defaults to data/<timestamp>/contracts.jsonl")
	appendOutput := flag.Bool("append", false, "append to output instead of creating a new file")
	resume := flag.Bool("resume", false, "continue after the last saved block")
	concurrency := flag.Int("concurrency", 1, "number of blocks to scrape in parallel")
	minBalance := flag.String("min-balance", "0", "minimum contract balance in ETH")
	downloadBytecode := flag.Bool("download-bytecode", false, "download deployed contract bytecode")
	bytecodeOutputDir := flag.String("bytecode-output-dir", "", "bytecode output directory; defaults beside contracts.jsonl")
	showProgress := flag.Bool("progress", false, "show scrape progress")
	printFound := flag.Bool("print-found", true, "print each found contract like ChainWalker")
	timeout := flag.Duration("timeout", 20*time.Second, "RPC timeout")
	retries := flag.Int("retries", 2, "retry rounds across RPC URLs")
	skipRPC := flag.Int("skip-rpc", 0, "skip the first N RPC URLs from the selected chain")
	syncChains := flag.Bool("sync-chains", false, "sync chain configs from Chainlist JSON and exit")
	chainsInput := flag.String("chains-input", "/tmp/chains.json", "source Chainlist chains.json path for --sync-chains")
	chainsOutput := flag.String("chains-output", "chains", "output chains directory for --sync-chains")
	flag.Var(&rpcURLs, "rpc-url", "override RPC URL; repeatable")
	flag.Parse()

	if *syncChains {
		count, err := chains.SyncFromFile(*chainsInput, *chainsOutput)
		if err != nil {
			return err
		}
		fmt.Printf("Wrote %d filtered chain configs to %s\n", count, *chainsOutput)
		return nil
	}

	chain, err := chains.Get(*chainName)
	if err != nil {
		return err
	}
	urls := chain.RPCURLs
	if len(rpcURLs) > 0 {
		urls = rpcURLs
	}
	if *skipRPC > 0 {
		if *skipRPC >= len(urls) {
			return fmt.Errorf("--skip-rpc=%d leaves no RPC URLs", *skipRPC)
		}
		urls = urls[*skipRPC:]
	}

	rpcClient, err := rpc.NewClient(urls, *timeout, rpc.WithRetries(*retries))
	if err != nil {
		return err
	}
	contractScraper := scraper.New(chain, rpcClient)
	ctx := context.Background()

	latest, err := contractScraper.LatestBlock(ctx)
	if err != nil {
		return err
	}
	if *latestOnly {
		fmt.Printf("%s latest block: %d\n", chain.Name, latest)
		return nil
	}

	start, end, err := resolveRange(latest, *lastBlocks, *startBlock, *endBlock)
	if err != nil {
		return err
	}

	outputPath := resolveOutputPath(*output, time.Now())
	shouldAppend := *appendOutput
	if *resume {
		lastSaved, ok, err := scraper.ReadLastBlock(outputPath)
		if err != nil {
			return err
		}
		if ok && lastSaved >= start {
			start = lastSaved + 1
			shouldAppend = true
		}
	}
	if start > end {
		fmt.Printf("No blocks to scrape. start=%d end=%d\n", start, end)
		return nil
	}
	minBalanceWei, err := parseMinBalance(*minBalance)
	if err != nil {
		return err
	}
	options := scraper.Options{
		Concurrency:       *concurrency,
		MinBalanceWei:     minBalanceWei,
		DownloadBytecode:  *downloadBytecode,
		BytecodeOutputDir: resolveBytecodeOutputDir(*bytecodeOutputDir, outputPath),
	}
	if *showProgress {
		options.Progress = printProgress
	}
	if *printFound {
		options.Found = printFoundContract
	}

	file, err := scraper.OpenOutput(outputPath, shouldAppend)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Printf("Scraping %s blocks %d..%d with concurrency=%d\n", chain.Name, start, end, *concurrency)
	count, err := contractScraper.ScrapeRangeWithOptions(ctx, start, end, file, options)
	if err != nil {
		return err
	}
	if *showProgress {
		fmt.Fprintln(os.Stderr)
	}
	fmt.Printf("Saved %d contract creations to %s\n", count, outputPath)
	return nil
}

func resolveRange(latest uint64, last uint64, start uint64, end uint64) (uint64, uint64, error) {
	if last > 0 {
		if last > latest+1 {
			return 0, 0, fmt.Errorf("--last is bigger than latest block range")
		}
		return latest - last + 1, latest, nil
	}
	if start == 0 && end == 0 {
		return 0, 0, fmt.Errorf("provide --last or both --start-block and --end-block")
	}
	if start > end {
		return 0, 0, fmt.Errorf("--start-block must be less than or equal to --end-block")
	}
	return start, end, nil
}

func parseMinBalance(raw string) (*big.Int, error) {
	wei, err := eth.ParseEther(raw)
	if err != nil {
		return nil, err
	}
	if wei.Sign() <= 0 {
		return nil, nil
	}
	return wei, nil
}

func resolveOutputPath(output string, now time.Time) string {
	if output != "" {
		return output
	}
	return filepath.Join("data", now.Format("2006-01-02_150405"), "contracts.jsonl")
}

func resolveBytecodeOutputDir(bytecodeOutputDir string, outputPath string) string {
	if bytecodeOutputDir != "" {
		return bytecodeOutputDir
	}
	return filepath.Join(filepath.Dir(outputPath), "bytecode")
}

func printProgress(progress scraper.Progress) {
	percent := float64(progress.Completed) / float64(progress.Total) * 100
	width := 30
	filled := int(float64(width) * float64(progress.Completed) / float64(progress.Total))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("=", filled) + strings.Repeat("-", width-filled)
	fmt.Fprintf(
		os.Stderr,
		"\r[%s] %6.2f%% %d/%d block=%d contracts=%d last_block_contracts=%d",
		bar,
		percent,
		progress.Completed,
		progress.Total,
		progress.BlockNumber,
		progress.Contracts,
		progress.LastBlockContracts,
	)
}

func printFoundContract(creation scraper.ContractCreation) {
	balanceWei, ok := new(big.Int).SetString(creation.BalanceWei, 10)
	if !ok {
		balanceWei = big.NewInt(0)
	}
	logInfo("Current block : %d", creation.BlockNumber)
	logInfo("Contract address : %s", creation.ContractAddress)
	logInfo("Contract balance : %s", eth.FormatEther(balanceWei, 2))
	if creation.BytecodePath != "" {
		logInfo("Bytecode path : %s", creation.BytecodePath)
	}
	logInfo("----------------------------------------------")
}

func logInfo(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s INF %s\n", time.Now().Format("3:04PM"), fmt.Sprintf(format, args...))
}
