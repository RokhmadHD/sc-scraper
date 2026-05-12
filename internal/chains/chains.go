package chains

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type Chain struct {
	ID           int
	Name         string
	ShortName    string
	Category     string
	NativeSymbol string
	RPCURLs      []string
}

type fileChain struct {
	Name           string `json:"name"`
	Chain          string `json:"chain"`
	ShortName      string `json:"shortName"`
	Category       string `json:"category"`
	ChainID        int    `json:"chainId"`
	NativeCurrency struct {
		Symbol string `json:"symbol"`
	} `json:"nativeCurrency"`
	RPC []string `json:"rpc"`
}

var Ethereum = Chain{
	ID:           1,
	Name:         "Ethereum Mainnet",
	ShortName:    "eth",
	Category:     "mainnet",
	NativeSymbol: "ETH",
	RPCURLs: []string{
		"https://ethereum.publicnode.com",
		"https://rpc.ankr.com/eth",
		"https://eth.llamarpc.com",
		"https://1rpc.io/eth",
		"https://eth.drpc.org",
	},
}

func Get(name string) (Chain, error) {
	query := normalize(name)
	if chain, ok, err := loadFromFiles(query); err != nil {
		return Chain{}, err
	} else if ok {
		return chain, nil
	}

	switch query {
	case "ethereum", "eth", "mainnet", "ethereum-mainnet":
		return Ethereum, nil
	default:
		return Chain{}, fmt.Errorf("unsupported chain %q; run `go run ./cmd/chains-sync --input /tmp/chains.json` or pass --rpc-url", name)
	}
}

func loadFromFiles(query string) (Chain, bool, error) {
	var matches []Chain
	chainsRoot := findChainsRoot()
	for _, root := range []string{filepath.Join(chainsRoot, "mainnet"), filepath.Join(chainsRoot, "test")} {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Chain{}, false, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name(), "chain.json")
			chain, err := loadFile(path)
			if err != nil {
				return Chain{}, false, err
			}
			if chainMatches(chain, query, entry.Name()) {
				matches = append(matches, chain)
			}
		}
	}
	if len(matches) == 0 {
		return Chain{}, false, nil
	}
	slices.SortFunc(matches, func(a Chain, b Chain) int {
		if a.Category == b.Category {
			return strings.Compare(a.ShortName, b.ShortName)
		}
		if a.Category == "mainnet" {
			return -1
		}
		return 1
	})
	return matches[0], true, nil
}

func findChainsRoot() string {
	dir, err := os.Getwd()
	if err == nil {
		if root, ok := findChainsRootFrom(dir); ok {
			return root
		}
	}

	_, file, _, ok := runtime.Caller(0)
	if ok {
		if root, ok := findChainsRootFrom(filepath.Dir(file)); ok {
			return root
		}
	}
	return "chains"
}

func findChainsRootFrom(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, "chains")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() && hasChainCategories(candidate) {
			return candidate, true
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", false
		}
		dir = next
	}
}

func hasChainCategories(path string) bool {
	for _, category := range []string{"mainnet", "test"} {
		if info, err := os.Stat(filepath.Join(path, category)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func loadFile(path string) (Chain, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Chain{}, err
	}
	var source fileChain
	if err := json.Unmarshal(data, &source); err != nil {
		return Chain{}, err
	}
	return Chain{
		ID:           source.ChainID,
		Name:         source.Name,
		ShortName:    source.ShortName,
		Category:     source.Category,
		NativeSymbol: source.NativeCurrency.Symbol,
		RPCURLs:      source.RPC,
	}, nil
}

func chainMatches(chain Chain, query string, folder string) bool {
	candidates := []string{
		chain.Name,
		chain.ShortName,
		folder,
		fmt.Sprint(chain.ID),
	}
	if query == "ethereum" && chain.ID == 1 {
		return true
	}
	for _, candidate := range candidates {
		if normalize(candidate) == query {
			return true
		}
	}
	return false
}

func normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.Join(strings.Fields(value), "-")
	return value
}
