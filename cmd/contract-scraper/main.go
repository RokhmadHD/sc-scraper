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
	"scrape-smart-contract/internal/storage"
)

type multiFlag []string

type multiSink []scraper.Sink

func (m *multiFlag) String() string {
	return fmt.Sprint([]string(*m))
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func (m multiSink) Save(ctx context.Context, creation scraper.ContractCreation) error {
	for _, sink := range m {
		if err := sink.Save(ctx, creation); err != nil {
			return err
		}
	}
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
	postgresURL := flag.String("postgres-url", "", "PostgreSQL connection URL; when set, JSONL is only written if --output is provided")
	postgresTable := flag.String("postgres-table", "contract_creations", "PostgreSQL table for --postgres-url")
	resetDB := flag.Bool("reset-db", false, "drop and recreate the PostgreSQL table before writing")
	resume := flag.Bool("resume", false, "continue after the last saved block")
	concurrency := flag.Int("concurrency", 1, "number of blocks to scrape in parallel")
	minBalance := flag.String("min-balance", "0", "minimum contract balance in ETH")
	tokenAddress := flag.String("token-address", "", "ERC-20 token address to check with balanceOf(contract)")
	minTokenBalance := flag.String("min-token-balance", "0", "minimum ERC-20 token balance; requires --token-address")
	tokenDecimals := flag.Int("token-decimals", 18, "ERC-20 token decimals for --min-token-balance and logs")
	downloadBytecode := flag.Bool("download-bytecode", false, "download deployed contract bytecode")
	inlineBytecode := flag.Bool("inline-bytecode", false, "store downloaded bytecode in JSONL instead of .evm files")
	bytecodeOutputDir := flag.String("bytecode-output-dir", "", "bytecode output directory; defaults beside contracts.jsonl")
	bytecodeAddress := flag.String("bytecode-address", "", "download bytecode for one contract address and exit")
	bytecodeOutput := flag.String("bytecode-output", "", "output .evm path for --bytecode-address; defaults to data/bytecode/<address>.evm")
	bytecodeStdout := flag.Bool("bytecode-stdout", false, "print bytecode to stdout for --bytecode-address instead of writing a file")
	showProgress := flag.Bool("progress", false, "show scrape progress")
	printFound := flag.Bool("print-found", true, "print each found contract like ChainWalker")
	continueOnError := flag.Bool("continue-on-error", true, "continue after a block-level RPC error")
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
	normalizedBytecodeAddress, err := normalizeAddress(*bytecodeAddress)
	if err != nil {
		return err
	}
	if normalizedBytecodeAddress != "" {
		return downloadSingleBytecode(ctx, contractScraper, normalizedBytecodeAddress, *bytecodeOutput, *bytecodeStdout)
	}

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
		if *postgresURL != "" && *output == "" {
			return fmt.Errorf("--resume with --postgres-url requires --output for now")
		}
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
	normalizedTokenAddress, err := normalizeAddress(*tokenAddress)
	if err != nil {
		return err
	}
	if *tokenDecimals < 0 {
		return fmt.Errorf("--token-decimals must be greater than or equal to 0")
	}
	tokenMin, err := parseTokenBalance(*minTokenBalance, *tokenDecimals)
	if err != nil {
		return err
	}
	if tokenMin != nil && normalizedTokenAddress == "" {
		return fmt.Errorf("--min-token-balance requires --token-address")
	}
	options := scraper.Options{
		Concurrency:       *concurrency,
		MinBalanceWei:     minBalanceWei,
		TokenAddress:      normalizedTokenAddress,
		TokenDecimals:     *tokenDecimals,
		MinTokenBalance:   tokenMin,
		DownloadBytecode:  *downloadBytecode,
		InlineBytecode:    *inlineBytecode,
		BytecodeOutputDir: resolveBytecodeOutputDir(*bytecodeOutputDir, outputPath),
		ContinueOnError:   *continueOnError,
	}
	if *showProgress {
		options.Progress = printProgress
	}
	if *printFound {
		options.Found = printFoundContract
	}

	sink, closeSink, err := resolveSink(ctx, *postgresURL, *postgresTable, *output, outputPath, shouldAppend, *resetDB)
	if err != nil {
		return err
	}
	defer closeSink()

	fmt.Printf("Scraping %s blocks %d..%d with concurrency=%d\n", chain.Name, start, end, *concurrency)
	count, err := contractScraper.ScrapeRangeWithSink(ctx, start, end, sink, options)
	if err != nil {
		return err
	}
	if *showProgress {
		fmt.Fprintln(os.Stderr)
	}
	if *postgresURL != "" {
		fmt.Printf("Saved %d contract creations to PostgreSQL table %s\n", count, *postgresTable)
		if *output != "" {
			fmt.Printf("Also wrote JSONL to %s\n", outputPath)
		}
	} else {
		fmt.Printf("Saved %d contract creations to %s\n", count, outputPath)
	}
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

func parseTokenBalance(raw string, decimals int) (*big.Int, error) {
	value, err := eth.ParseUnits(raw, decimals)
	if err != nil {
		return nil, err
	}
	if value.Sign() <= 0 {
		return nil, nil
	}
	return value, nil
}

func normalizeAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.HasPrefix(raw, "0x") || len(raw) != 42 {
		return "", fmt.Errorf("invalid address %q", raw)
	}
	for _, r := range raw[2:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return "", fmt.Errorf("invalid address %q", raw)
		}
	}
	return strings.ToLower(raw), nil
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

func resolveSink(ctx context.Context, postgresURL string, postgresTable string, output string, outputPath string, appendOutput bool, resetDB bool) (scraper.Sink, func(), error) {
	var sinks []scraper.Sink
	var closers []func()
	if postgresURL != "" {
		postgresSink, err := storage.OpenPostgres(ctx, postgresURL, postgresTable, resetDB)
		if err != nil {
			return nil, nil, err
		}
		sinks = append(sinks, postgresSink)
		closers = append(closers, func() { _ = postgresSink.Close() })
	}
	if postgresURL == "" || output != "" {
		file, err := scraper.OpenOutput(outputPath, appendOutput)
		if err != nil {
			closeAll(closers)
			return nil, nil, err
		}
		sinks = append(sinks, scraper.NewJSONLSink(file))
		closers = append(closers, func() { _ = file.Close() })
	}
	return multiSink(sinks), func() { closeAll(closers) }, nil
}

func closeAll(closers []func()) {
	for i := len(closers) - 1; i >= 0; i-- {
		closers[i]()
	}
}

func downloadSingleBytecode(ctx context.Context, contractScraper *scraper.Scraper, address string, outputPath string, stdout bool) error {
	if stdout {
		result, err := contractScraper.DownloadBytecode(ctx, address, "")
		if err != nil {
			return err
		}
		fmt.Println(result.Bytecode)
		return nil
	}
	if outputPath == "" {
		outputPath = filepath.Join("data", "bytecode", address+".evm")
	}
	result, err := contractScraper.DownloadBytecode(ctx, address, outputPath)
	if err != nil {
		return err
	}
	fmt.Printf("Saved %d byte bytecode for %s to %s\n", result.Size, result.Address, result.Path)
	return nil
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
	if creation.TokenBalance != "" {
		tokenBalance, ok := new(big.Int).SetString(creation.TokenBalance, 10)
		if !ok {
			tokenBalance = big.NewInt(0)
		}
		logInfo("Token address : %s", creation.TokenAddress)
		logInfo("Token balance : %s", eth.FormatUnits(tokenBalance, creation.TokenDecimals, 4))
	}
	if creation.BytecodePath != "" {
		logInfo("Bytecode path : %s", creation.BytecodePath)
	}
	if creation.Bytecode != "" {
		logInfo("Bytecode size : %d bytes", creation.BytecodeSize)
	}
	logInfo("----------------------------------------------")
}

func logInfo(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s INF %s\n", time.Now().Format("3:04PM"), fmt.Sprintf(format, args...))
}
