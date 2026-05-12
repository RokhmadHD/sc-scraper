package chains

import "testing"

func TestGetLoadsEthereumFromGeneratedChains(t *testing.T) {
	chain, err := Get("eth")
	if err != nil {
		t.Fatal(err)
	}
	if chain.ID != 1 {
		t.Fatalf("chain ID = %d, want 1", chain.ID)
	}
	if len(chain.RPCURLs) == 0 {
		t.Fatal("expected filtered RPC URLs")
	}
}

func TestGetLoadsBaseFromGeneratedChains(t *testing.T) {
	chain, err := Get("base")
	if err != nil {
		t.Fatal(err)
	}
	if chain.ID != 8453 {
		t.Fatalf("chain ID = %d, want 8453", chain.ID)
	}
	if chain.Category != "mainnet" {
		t.Fatalf("category = %s, want mainnet", chain.Category)
	}
}

func TestGetLoadsSepoliaFromGeneratedChains(t *testing.T) {
	chain, err := Get("sep")
	if err != nil {
		t.Fatal(err)
	}
	if chain.ID != 11155111 {
		t.Fatalf("chain ID = %d, want 11155111", chain.ID)
	}
	if chain.Category != "test" {
		t.Fatalf("category = %s, want test", chain.Category)
	}
}
