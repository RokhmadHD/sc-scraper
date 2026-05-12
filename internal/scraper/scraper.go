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

type Options struct {
	Concurrency       int
	MinBalanceWei     *big.Int
	DownloadBytecode  bool
	BytecodeOutputDir string
	Progress          func(Progress)
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
	BytecodeSize      int    `json:"bytecode_size,omitempty"`
	BytecodePath      string `json:"bytecode_path,omitempty"`
	Timestamp         uint64 `json:"timestamp"`
	RPCURL            string `json:"rpc_url"`
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

func (s *Scraper) ScrapeRange(ctx context.Context, startBlock uint64, endBlock uint64, writer io.Writer) (int, error) {
	return s.ScrapeRangeWithOptions(ctx, startBlock, endBlock, writer, Options{})
}

func (s *Scraper) ScrapeRangeWithOptions(ctx context.Context, startBlock uint64, endBlock uint64, writer io.Writer, options Options) (int, error) {
	if startBlock > endBlock {
		return 0, fmt.Errorf("start block must be less than or equal to end block")
	}
	options = normalizeOptions(options)
	if options.Concurrency > 1 {
		return s.scrapeRangeConcurrent(ctx, startBlock, endBlock, writer, options)
	}
	return s.scrapeRangeSequential(ctx, startBlock, endBlock, writer, options)
}

func (s *Scraper) scrapeRangeSequential(ctx context.Context, startBlock uint64, endBlock uint64, writer io.Writer, options Options) (int, error) {
	encoder := json.NewEncoder(writer)
	count := 0
	total := int64(endBlock - startBlock + 1)
	completed := int64(0)
	for blockNumber := startBlock; blockNumber <= endBlock; blockNumber++ {
		creations, err := s.ScrapeBlockWithOptions(ctx, blockNumber, options)
		if err != nil {
			return count, fmt.Errorf("scrape block %d: %w", blockNumber, err)
		}
		for _, creation := range creations {
			if err := encoder.Encode(creation); err != nil {
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

func (s *Scraper) scrapeRangeConcurrent(ctx context.Context, startBlock uint64, endBlock uint64, writer io.Writer, options Options) (int, error) {
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
				if err != nil {
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

	encoder := json.NewEncoder(writer)
	count := 0
	total := int64(endBlock - startBlock + 1)
	completed := int64(0)
	for result := range results {
		if result.err != nil {
			return count, fmt.Errorf("scrape block %d: %w", result.blockNumber, result.err)
		}
		for _, creation := range result.creations {
			if err := encoder.Encode(creation); err != nil {
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
		bytecodePath := ""
		bytecodeSize := 0
		if options.DownloadBytecode {
			bytecode, err := s.bytecode(ctx, receipt.ContractAddress)
			if err != nil {
				return nil, err
			}
			bytecodeSize = bytecodeLength(bytecode)
			if bytecodeSize == 0 {
				continue
			}
			bytecodePath, err = writeBytecode(options.BytecodeOutputDir, receipt.ContractAddress, bytecode)
			if err != nil {
				return nil, err
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
			BytecodeSize:      bytecodeSize,
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

func normalizeOptions(options Options) Options {
	if options.Concurrency < 1 {
		options.Concurrency = 1
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

func reportFound(options Options, creation ContractCreation) {
	if options.Found != nil {
		options.Found(creation)
	}
}

func bytecodeLength(bytecode string) int {
	clean := strings.TrimPrefix(bytecode, "0x")
	return len(clean) / 2
}

func writeBytecode(outputDir string, address string, bytecode string) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	clean := strings.TrimPrefix(bytecode, "0x")
	if _, err := hex.DecodeString(clean); err != nil {
		return "", fmt.Errorf("invalid bytecode for %s: %w", address, err)
	}
	path := filepath.Join(outputDir, address+".evm")
	return path, os.WriteFile(path, []byte(clean), 0o644)
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
