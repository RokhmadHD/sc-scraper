package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"scrape-smart-contract/internal/chains"
	"scrape-smart-contract/internal/rpc"
)

type fakeRPC struct{}
type failedCreationRPC struct{}
type emptyBytecodeRPC struct{}

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
