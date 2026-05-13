package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"scrape-smart-contract/internal/chains"
	"scrape-smart-contract/internal/rpc"
)

type fakeRPC struct{}
type failedCreationRPC struct{}
type emptyBytecodeRPC struct{}
type tokenBalanceRPC struct{}
type flakyBlockRPC struct{}

func (f fakeRPC) ActiveURL() string {
	return "https://example.test"
}

func (f fakeRPC) Call(_ context.Context, method string, _ any, result any) error {
	switch method {
	case "eth_blockNumber":
		*(result.(*string)) = "0xa"
	case "eth_getBlockByNumber":
		block := result.(**Block)
		*block = &Block{
			Timestamp: 0x65,
			Transactions: []Transaction{
				{Hash: "0xcreate", From: "0xcreator", To: nil},
				{Hash: "0xtransfer", From: "0xsender", To: stringPtr("0xreceiver")},
			},
		}
	case "eth_getBalance":
		*(result.(*string)) = "0xde0b6b3a7640000"
	case "eth_getCode":
		*(result.(*string)) = "0x60016002"
	default:
		t := testing.T{}
		t.Fatalf("unexpected method %s", method)
	}
	return nil
}

func (f fakeRPC) BatchCall(_ context.Context, calls []rpc.Call, results []any) error {
	if len(calls) != 1 || calls[0].Method != "eth_getTransactionReceipt" {
		panic("unexpected batch calls")
	}
	receipt := results[0].(**Receipt)
	*receipt = &Receipt{
		ContractAddress:   "0xcontract",
		Status:            1,
		GasUsed:           0x5208,
		EffectiveGasPrice: "0x3b9aca00",
	}
	return nil
}

func (f failedCreationRPC) ActiveURL() string {
	return (fakeRPC{}).ActiveURL()
}

func (f failedCreationRPC) Call(ctx context.Context, method string, params any, result any) error {
	return (fakeRPC{}).Call(ctx, method, params, result)
}

func (f failedCreationRPC) BatchCall(ctx context.Context, calls []rpc.Call, results []any) error {
	if err := (fakeRPC{}).BatchCall(ctx, calls, results); err != nil {
		return err
	}
	receipt := results[0].(**Receipt)
	(*receipt).Status = 0
	return nil
}

func (f emptyBytecodeRPC) ActiveURL() string {
	return (fakeRPC{}).ActiveURL()
}

func (f emptyBytecodeRPC) Call(ctx context.Context, method string, params any, result any) error {
	if method == "eth_getCode" {
		*(result.(*string)) = "0x"
		return nil
	}
	return (fakeRPC{}).Call(ctx, method, params, result)
}

func (f emptyBytecodeRPC) BatchCall(ctx context.Context, calls []rpc.Call, results []any) error {
	return (fakeRPC{}).BatchCall(ctx, calls, results)
}

func (f tokenBalanceRPC) ActiveURL() string {
	return (fakeRPC{}).ActiveURL()
}

func (f tokenBalanceRPC) Call(ctx context.Context, method string, params any, result any) error {
	if method == "eth_call" {
		calls := params.([]any)
		call := calls[0].(map[string]string)
		wantData := "0x70a082310000000000000000000000001111111111111111111111111111111111111111"
		if call["to"] != "0x2222222222222222222222222222222222222222" {
			panic("unexpected token address")
		}
		if call["data"] != wantData {
			panic("unexpected balanceOf calldata")
		}
		*(result.(*string)) = "0x8ac7230489e80000"
		return nil
	}
	return (fakeRPC{}).Call(ctx, method, params, result)
}

func (f tokenBalanceRPC) BatchCall(_ context.Context, calls []rpc.Call, results []any) error {
	if len(calls) != 1 || calls[0].Method != "eth_getTransactionReceipt" {
		panic("unexpected batch calls")
	}
	receipt := results[0].(**Receipt)
	*receipt = &Receipt{
		ContractAddress:   "0x1111111111111111111111111111111111111111",
		Status:            1,
		GasUsed:           0x5208,
		EffectiveGasPrice: "0x3b9aca00",
	}
	return nil
}

func (f flakyBlockRPC) ActiveURL() string {
	return (fakeRPC{}).ActiveURL()
}

func (f flakyBlockRPC) Call(ctx context.Context, method string, params any, result any) error {
	if method == "eth_getBlockByNumber" {
		raw := params.([]any)[0].(string)
		if raw == "0xb" {
			return fmt.Errorf("rpc error: Internal error")
		}
	}
	return (fakeRPC{}).Call(ctx, method, params, result)
}

func (f flakyBlockRPC) BatchCall(ctx context.Context, calls []rpc.Call, results []any) error {
	return (fakeRPC{}).BatchCall(ctx, calls, results)
}

func TestLatestBlock(t *testing.T) {
	s := New(chains.Ethereum, fakeRPC{})

	latest, err := s.LatestBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest != 10 {
		t.Fatalf("latest block = %d, want 10", latest)
	}
}

func TestScrapeBlockContractCreation(t *testing.T) {
	s := New(chains.Ethereum, fakeRPC{})

	creations, err := s.ScrapeBlock(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(creations) != 1 {
		t.Fatalf("creations = %d, want 1", len(creations))
	}
	if creations[0].ContractAddress != "0xcontract" {
		t.Fatalf("contract address = %s", creations[0].ContractAddress)
	}
	if creations[0].EffectiveGasPrice != "1000000000" {
		t.Fatalf("effective gas price = %s", creations[0].EffectiveGasPrice)
	}
	if creations[0].BalanceWei != "1000000000000000000" {
		t.Fatalf("balance wei = %s", creations[0].BalanceWei)
	}
}

func TestScrapeRangeWritesJSONL(t *testing.T) {
	s := New(chains.Ethereum, fakeRPC{})

	var out bytes.Buffer
	count, err := s.ScrapeRange(context.Background(), 10, 10, &out)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var creation ContractCreation
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &creation); err != nil {
		t.Fatal(err)
	}
	if creation.BlockNumber != 10 {
		t.Fatalf("block number = %d, want 10", creation.BlockNumber)
	}
}

func TestScrapeRangeProgressReportsCumulativeContracts(t *testing.T) {
	s := New(chains.Ethereum, fakeRPC{})
	var progress []Progress

	var out bytes.Buffer
	count, err := s.ScrapeRangeWithOptions(context.Background(), 10, 11, &out, Options{
		Progress: func(item Progress) {
			progress = append(progress, item)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	last := progress[len(progress)-1]
	if last.Contracts != 2 {
		t.Fatalf("progress contracts = %d, want 2", last.Contracts)
	}
	if last.LastBlockContracts != 1 {
		t.Fatalf("progress last block contracts = %d, want 1", last.LastBlockContracts)
	}
}

func TestScrapeRangeContinuesOnBlockError(t *testing.T) {
	s := New(chains.Ethereum, flakyBlockRPC{})
	var progress []Progress
	var errors []uint64

	var out bytes.Buffer
	count, err := s.ScrapeRangeWithOptions(context.Background(), 10, 12, &out, Options{
		ContinueOnError: true,
		Progress: func(item Progress) {
			progress = append(progress, item)
		},
		Error: func(blockNumber uint64, err error) {
			errors = append(errors, blockNumber)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if len(errors) != 1 || errors[0] != 11 {
		t.Fatalf("errors = %#v, want block 11", errors)
	}
	last := progress[len(progress)-1]
	if last.Completed != 3 {
		t.Fatalf("completed = %d, want 3", last.Completed)
	}
}

func TestScrapeRangeContinuesOnBlockErrorConcurrently(t *testing.T) {
	s := New(chains.Ethereum, flakyBlockRPC{})
	var progress []Progress
	var errors []uint64

	var out bytes.Buffer
	count, err := s.ScrapeRangeWithOptions(context.Background(), 10, 12, &out, Options{
		Concurrency:     2,
		ContinueOnError: true,
		Progress: func(item Progress) {
			progress = append(progress, item)
		},
		Error: func(blockNumber uint64, err error) {
			errors = append(errors, blockNumber)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if len(errors) != 1 || errors[0] != 11 {
		t.Fatalf("errors = %#v, want block 11", errors)
	}
	if len(progress) == 0 {
		t.Fatal("expected progress updates")
	}
}

func TestScrapeBlockDownloadsBytecode(t *testing.T) {
	s := New(chains.Ethereum, fakeRPC{})
	outputDir := t.TempDir()

	creations, err := s.ScrapeBlockWithOptions(context.Background(), 10, Options{
		DownloadBytecode:  true,
		BytecodeOutputDir: outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if creations[0].BytecodeSize != 4 {
		t.Fatalf("bytecode size = %d, want 4", creations[0].BytecodeSize)
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "0xcontract.evm"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "60016002" {
		t.Fatalf("bytecode file = %s", data)
	}
}

func TestScrapeBlockInlinesBytecode(t *testing.T) {
	s := New(chains.Ethereum, fakeRPC{})

	creations, err := s.ScrapeBlockWithOptions(context.Background(), 10, Options{
		DownloadBytecode: true,
		InlineBytecode:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if creations[0].Bytecode != "60016002" {
		t.Fatalf("bytecode = %s", creations[0].Bytecode)
	}
	if creations[0].BytecodePath != "" {
		t.Fatalf("bytecode path = %s, want empty", creations[0].BytecodePath)
	}
}

func TestDownloadBytecodeForAddress(t *testing.T) {
	s := New(chains.Ethereum, fakeRPC{})
	outputPath := filepath.Join(t.TempDir(), "contract.evm")

	result, err := s.DownloadBytecode(context.Background(), "0xcontract", outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != 4 {
		t.Fatalf("size = %d, want 4", result.Size)
	}
	if result.Bytecode != "60016002" {
		t.Fatalf("bytecode = %s", result.Bytecode)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "60016002" {
		t.Fatalf("bytecode file = %s", data)
	}
}

func TestScrapeBlockChecksTokenBalance(t *testing.T) {
	s := New(chains.Ethereum, tokenBalanceRPC{})

	creations, err := s.ScrapeBlockWithOptions(context.Background(), 10, Options{
		TokenAddress:    "0x2222222222222222222222222222222222222222",
		TokenDecimals:   18,
		MinTokenBalance: mustBigInt("10000000000000000000"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(creations) != 1 {
		t.Fatalf("creations = %d, want 1", len(creations))
	}
	if creations[0].TokenBalance != "10000000000000000000" {
		t.Fatalf("token balance = %s", creations[0].TokenBalance)
	}
	if creations[0].TokenAddress != "0x2222222222222222222222222222222222222222" {
		t.Fatalf("token address = %s", creations[0].TokenAddress)
	}
}

func TestScrapeBlockFiltersTokenBalance(t *testing.T) {
	s := New(chains.Ethereum, tokenBalanceRPC{})

	creations, err := s.ScrapeBlockWithOptions(context.Background(), 10, Options{
		TokenAddress:    "0x2222222222222222222222222222222222222222",
		TokenDecimals:   18,
		MinTokenBalance: mustBigInt("10000000000000000001"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(creations) != 0 {
		t.Fatalf("creations = %d, want 0", len(creations))
	}
}

func TestScrapeBlockSkipsFailedCreation(t *testing.T) {
	s := New(chains.Ethereum, failedCreationRPC{})

	creations, err := s.ScrapeBlock(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(creations) != 0 {
		t.Fatalf("creations = %d, want 0", len(creations))
	}
}

func TestScrapeBlockSkipsEmptyBytecodeWhenDownloading(t *testing.T) {
	s := New(chains.Ethereum, emptyBytecodeRPC{})

	creations, err := s.ScrapeBlockWithOptions(context.Background(), 10, Options{
		DownloadBytecode:  true,
		BytecodeOutputDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(creations) != 0 {
		t.Fatalf("creations = %d, want 0", len(creations))
	}
}

func stringPtr(value string) *string {
	return &value
}

func mustBigInt(raw string) *big.Int {
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		panic("invalid big int")
	}
	return value
}
