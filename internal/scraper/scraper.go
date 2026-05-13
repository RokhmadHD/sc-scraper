package scraper

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"scrape-smart-contract/internal/chains"
	"scrape-smart-contract/internal/eth"
	"scrape-smart-contract/internal/rpc"
)

type RPC interface {
	ActiveURL() string
	Call(ctx context.Context, method string, params any, result any) error
	BatchCall(ctx context.Context, calls []rpc.Call, results []any) error
}

type Scraper struct {
	chain chains.Chain
	rpc   RPC
}

type Sink interface {
	Save(ctx context.Context, creation ContractCreation) error
}

type JSONLSink struct {
	encoder *json.Encoder
}

func NewJSONLSink(writer io.Writer) *JSONLSink {
	return &JSONLSink{encoder: json.NewEncoder(writer)}
}

func (s *JSONLSink) Save(_ context.Context, creation ContractCreation) error {
	return s.encoder.Encode(creation)
}

type Options struct {
	Concurrency       int
	MinBalanceWei     *big.Int
	TokenAddress      string
	TokenDecimals     int
	MinTokenBalance   *big.Int
	DownloadBytecode  bool
	InlineBytecode    bool
	BytecodeOutputDir string
	ContinueOnError   bool
	Progress          func(Progress)
	Error             func(blockNumber uint64, err error)
	Found             func(ContractCreation)
}

type Progress struct {
	BlockNumber        int64
	Contracts          int
	LastBlockContracts int
	Completed          int64
	Total              int64
}

type ContractCreation struct {
	ChainID           int    `json:"chain_id"`
	Network           string `json:"network"`
	BlockNumber       uint64 `json:"block_number"`
	TransactionHash   string `json:"transaction_hash"`
	ContractAddress   string `json:"contract_address"`
	Creator           string `json:"creator"`
	Status            uint64 `json:"status"`
	GasUsed           uint64 `json:"gas_used"`
	EffectiveGasPrice string `json:"effective_gas_price"`
	BalanceWei        string `json:"balance_wei,omitempty"`
	TokenAddress      string `json:"token_address,omitempty"`
	TokenBalance      string `json:"token_balance,omitempty"`
	TokenDecimals     int    `json:"token_decimals,omitempty"`
	BytecodeSize      int    `json:"bytecode_size,omitempty"`
	Bytecode          string `json:"bytecode,omitempty"`
	BytecodePath      string `json:"bytecode_path,omitempty"`
	Timestamp         uint64 `json:"timestamp"`
	RPCURL            string `json:"rpc_url"`
}

type DownloadedBytecode struct {
	Address  string
	Bytecode string
	Size     int
	Path     string
}

type Block struct {
	Number       eth.HexUint64 `json:"number"`
	Timestamp    eth.HexUint64 `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
}

type Transaction struct {
	Hash string  `json:"hash"`
	From string  `json:"from"`
	To   *string `json:"to"`
}

type Receipt struct {
	ContractAddress   string        `json:"contractAddress"`
	Status            eth.HexUint64 `json:"status"`
	GasUsed           eth.HexUint64 `json:"gasUsed"`
	EffectiveGasPrice string        `json:"effectiveGasPrice"`
}

func New(chain chains.Chain, rpcClient RPC) *Scraper {
	return &Scraper{chain: chain, rpc: rpcClient}
}

func (s *Scraper) LatestBlock(ctx context.Context) (uint64, error) {
	var raw string
	if err := s.rpc.Call(ctx, "eth_blockNumber", []any{}, &raw); err != nil {
		return 0, err
	}
	return eth.ParseHexUint64(raw)
}

func (s *Scraper) DownloadBytecode(ctx context.Context, address string, outputPath string) (DownloadedBytecode, error) {
	bytecode, err := s.bytecode(ctx, address)
	if err != nil {
		return DownloadedBytecode{}, err
	}
	cleanBytecode, err := cleanBytecode(bytecode)
	if err != nil {
		return DownloadedBytecode{}, fmt.Errorf("invalid bytecode for %s: %w", address, err)
	}
	if cleanBytecode == "" {
		return DownloadedBytecode{}, fmt.Errorf("no bytecode found for %s", address)
	}
	result := DownloadedBytecode{
		Address:  address,
		Bytecode: cleanBytecode,
		Size:     len(cleanBytecode) / 2,
	}
	if outputPath != "" {
		if err := writeBytecodeFile(outputPath, cleanBytecode); err != nil {
			return DownloadedBytecode{}, err
		}
		result.Path = outputPath
	}
	return result, nil
}

func (s *Scraper) ScrapeRange(ctx context.Context, startBlock uint64, endBlock uint64, writer io.Writer) (int, error) {
	return s.ScrapeRangeWithOptions(ctx, startBlock, endBlock, writer, Options{})
}

func (s *Scraper) ScrapeRangeWithOptions(ctx context.Context, startBlock uint64, endBlock uint64, writer io.Writer, options Options) (int, error) {
	return s.ScrapeRangeWithSink(ctx, startBlock, endBlock, NewJSONLSink(writer), options)
}

func (s *Scraper) ScrapeRangeWithSink(ctx context.Context, startBlock uint64, endBlock uint64, sink Sink, options Options) (int, error) {
	if startBlock > endBlock {
		return 0, fmt.Errorf("start block must be less than or equal to end block")
	}
	options = normalizeOptions(options)
	if options.Concurrency > 1 {
		return s.scrapeRangeConcurrent(ctx, startBlock, endBlock, sink, options)
	}
	return s.scrapeRangeSequential(ctx, startBlock, endBlock, sink, options)
}

func (s *Scraper) scrapeRangeSequential(ctx context.Context, startBlock uint64, endBlock uint64, sink Sink, options Options) (int, error) {
	count := 0
	total := int64(endBlock - startBlock + 1)
	completed := int64(0)
	for blockNumber := startBlock; blockNumber <= endBlock; blockNumber++ {
		creations, err := s.ScrapeBlockWithOptions(ctx, blockNumber, options)
		if err != nil {
			if options.ContinueOnError {
				reportError(options, blockNumber, err)
				completed++
				reportProgress(options, Progress{
					BlockNumber:        int64(blockNumber),
					Contracts:          count,
					LastBlockContracts: 0,
					Completed:          completed,
					Total:              total,
				})
				continue
			}
			return count, fmt.Errorf("scrape block %d: %w", blockNumber, err)
		}
		for _, creation := range creations {
			if err := sink.Save(ctx, creation); err != nil {
				return count, err
			}
			count++
			reportFound(options, creation)
		}
		completed++
		reportProgress(options, Progress{
			BlockNumber:        int64(blockNumber),
			Contracts:          count,
			LastBlockContracts: len(creations),
			Completed:          completed,
			Total:              total,
		})
	}
	return count, nil
}

func (s *Scraper) scrapeRangeConcurrent(ctx context.Context, startBlock uint64, endBlock uint64, sink Sink, options Options) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan uint64)
	results := make(chan blockResult, options.Concurrency)
	var wg sync.WaitGroup

	for range options.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for blockNumber := range jobs {
				creations, err := s.ScrapeBlockWithOptions(ctx, blockNumber, options)
				result := blockResult{blockNumber: blockNumber, creations: creations, err: err}
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
				if err != nil && !options.ContinueOnError {
					cancel()
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for blockNumber := startBlock; blockNumber <= endBlock; blockNumber++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- blockNumber:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	count := 0
	total := int64(endBlock - startBlock + 1)
	completed := int64(0)
	for result := range results {
		if result.err != nil {
			if options.ContinueOnError {
				reportError(options, result.blockNumber, result.err)
				completed++
				reportProgress(options, Progress{
					BlockNumber:        int64(result.blockNumber),
					Contracts:          count,
					LastBlockContracts: 0,
					Completed:          completed,
					Total:              total,
				})
				continue
			}
			return count, fmt.Errorf("scrape block %d: %w", result.blockNumber, result.err)
		}
		for _, creation := range result.creations {
			if err := sink.Save(ctx, creation); err != nil {
				cancel()
				return count, err
			}
			count++
			reportFound(options, creation)
		}
		completed++
		reportProgress(options, Progress{
			BlockNumber:        int64(result.blockNumber),
			Contracts:          count,
			LastBlockContracts: len(result.creations),
			Completed:          completed,
			Total:              total,
		})
	}
	return count, nil
}

func (s *Scraper) ScrapeBlock(ctx context.Context, blockNumber uint64) ([]ContractCreation, error) {
	return s.ScrapeBlockWithOptions(ctx, blockNumber, Options{})
}

func (s *Scraper) ScrapeBlockWithOptions(ctx context.Context, blockNumber uint64, options Options) ([]ContractCreation, error) {
	options = normalizeOptions(options)
	var block *Block
	if err := s.rpc.Call(ctx, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", blockNumber), true}, &block); err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}

	creationTXs := make([]Transaction, 0)
	for _, tx := range block.Transactions {
		if tx.To == nil {
			creationTXs = append(creationTXs, tx)
		}
	}
	if len(creationTXs) == 0 {
		return nil, nil
	}

	calls := make([]rpc.Call, 0, len(creationTXs))
	results := make([]any, 0, len(creationTXs))
	receipts := make([]*Receipt, len(creationTXs))
	for index, tx := range creationTXs {
		calls = append(calls, rpc.Call{Method: "eth_getTransactionReceipt", Params: []any{tx.Hash}})
		results = append(results, &receipts[index])
	}
	if err := s.rpc.BatchCall(ctx, calls, results); err != nil {
		return nil, err
	}

	creations := make([]ContractCreation, 0, len(receipts))
	for index, receipt := range receipts {
		if receipt == nil || receipt.ContractAddress == "" {
			continue
		}
		if receipt.Status.Uint64() != 1 {
			continue
		}
		price, err := eth.ParseHexBigInt(receipt.EffectiveGasPrice)
		if err != nil {
			return nil, err
		}
		balanceWei, err := s.balanceWei(ctx, receipt.ContractAddress)
		if err != nil {
			return nil, err
		}
		if options.MinBalanceWei != nil && balanceWei.Cmp(options.MinBalanceWei) < 0 {
			continue
		}
		tokenBalance := ""
		if options.TokenAddress != "" {
			balance, err := s.tokenBalance(ctx, options.TokenAddress, receipt.ContractAddress)
			if err != nil {
				return nil, err
			}
			if options.MinTokenBalance != nil && balance.Cmp(options.MinTokenBalance) < 0 {
				continue
			}
			tokenBalance = balance.String()
		}
		bytecodePath := ""
		bytecodeValue := ""
		bytecodeSize := 0
		if options.DownloadBytecode {
			bytecode, err := s.bytecode(ctx, receipt.ContractAddress)
			if err != nil {
				return nil, err
			}
			cleanBytecode, err := cleanBytecode(bytecode)
			if err != nil {
				return nil, fmt.Errorf("invalid bytecode for %s: %w", receipt.ContractAddress, err)
			}
			bytecodeSize = len(cleanBytecode) / 2
			if bytecodeSize == 0 {
				continue
			}
			if options.InlineBytecode {
				bytecodeValue = cleanBytecode
			} else {
				bytecodePath, err = writeBytecode(options.BytecodeOutputDir, receipt.ContractAddress, cleanBytecode)
				if err != nil {
					return nil, err
				}
			}
		}
		creations = append(creations, ContractCreation{
			ChainID:           s.chain.ID,
			Network:           s.chain.Name,
			BlockNumber:       blockNumber,
			TransactionHash:   creationTXs[index].Hash,
			ContractAddress:   receipt.ContractAddress,
			Creator:           creationTXs[index].From,
			Status:            receipt.Status.Uint64(),
			GasUsed:           receipt.GasUsed.Uint64(),
			EffectiveGasPrice: price.String(),
			BalanceWei:        balanceWei.String(),
			TokenAddress:      options.TokenAddress,
			TokenBalance:      tokenBalance,
			TokenDecimals:     options.TokenDecimals,
			BytecodeSize:      bytecodeSize,
			Bytecode:          bytecodeValue,
			BytecodePath:      bytecodePath,
			Timestamp:         block.Timestamp.Uint64(),
			RPCURL:            s.rpc.ActiveURL(),
		})
	}
	return creations, nil
}

type blockResult struct {
	blockNumber uint64
	creations   []ContractCreation
	err         error
}

func (s *Scraper) balanceWei(ctx context.Context, address string) (*big.Int, error) {
	var raw string
	if err := s.rpc.Call(ctx, "eth_getBalance", []any{address, "latest"}, &raw); err != nil {
		return nil, err
	}
	return eth.ParseHexBigInt(raw)
}

func (s *Scraper) bytecode(ctx context.Context, address string) (string, error) {
	var raw string
	if err := s.rpc.Call(ctx, "eth_getCode", []any{address, "latest"}, &raw); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Scraper) tokenBalance(ctx context.Context, tokenAddress string, holderAddress string) (*big.Int, error) {
	data, err := balanceOfCallData(holderAddress)
	if err != nil {
		return nil, err
	}
	var raw string
	call := map[string]string{
		"to":   tokenAddress,
		"data": data,
	}
	if err := s.rpc.Call(ctx, "eth_call", []any{call, "latest"}, &raw); err != nil {
		return nil, err
	}
	return eth.ParseHexBigInt(raw)
}

func normalizeOptions(options Options) Options {
	if options.Concurrency < 1 {
		options.Concurrency = 1
	}
	if options.TokenDecimals < 0 {
		options.TokenDecimals = 0
	}
	if options.BytecodeOutputDir == "" {
		options.BytecodeOutputDir = "data/bytecode"
	}
	return options
}

func reportProgress(options Options, progress Progress) {
	if options.Progress != nil {
		options.Progress(progress)
	}
}

func reportError(options Options, blockNumber uint64, err error) {
	if options.Error != nil {
		options.Error(blockNumber, err)
	}
}

func reportFound(options Options, creation ContractCreation) {
	if options.Found != nil {
		options.Found(creation)
	}
}

func cleanBytecode(bytecode string) (string, error) {
	clean := strings.TrimPrefix(bytecode, "0x")
	if clean == "" {
		return "", nil
	}
	if _, err := hex.DecodeString(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func writeBytecode(outputDir string, address string, cleanBytecode string) (string, error) {
	path := filepath.Join(outputDir, address+".evm")
	return path, writeBytecodeFile(path, cleanBytecode)
}

func writeBytecodeFile(path string, cleanBytecode string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(cleanBytecode), 0o644)
}

func balanceOfCallData(holderAddress string) (string, error) {
	clean := strings.TrimPrefix(strings.ToLower(holderAddress), "0x")
	if len(clean) != 40 {
		return "", fmt.Errorf("invalid holder address %q", holderAddress)
	}
	if _, err := hex.DecodeString(clean); err != nil {
		return "", fmt.Errorf("invalid holder address %q: %w", holderAddress, err)
	}
	return "0x70a08231" + strings.Repeat("0", 24) + clean, nil
}

func OpenOutput(path string, append bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	flag := os.O_CREATE | os.O_WRONLY
	if append {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	return os.OpenFile(path, flag, 0o644)
}

func ReadLastBlock(path string) (uint64, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var last uint64
	found := false
	for decoder.More() {
		var item ContractCreation
		if err := decoder.Decode(&item); err != nil {
			return 0, false, err
		}
		if !found || item.BlockNumber > last {
			last = item.BlockNumber
			found = true
		}
	}
	return last, found, nil
}
