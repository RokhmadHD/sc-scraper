package chains

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SourceChain struct {
	Name           string         `json:"name"`
	Chain          string         `json:"chain"`
	Status         string         `json:"status,omitempty"`
	RPC            []string       `json:"rpc"`
	Faucets        []string       `json:"faucets,omitempty"`
	NativeCurrency NativeCurrency `json:"nativeCurrency"`
	InfoURL        string         `json:"infoURL,omitempty"`
	ShortName      string         `json:"shortName"`
	ChainID        int            `json:"chainId"`
	NetworkID      int            `json:"networkId"`
	Explorers      []Explorer     `json:"explorers,omitempty"`
}

type OutputChain struct {
	Name           string         `json:"name"`
	Chain          string         `json:"chain"`
	ShortName      string         `json:"shortName"`
	Category       string         `json:"category"`
	ChainID        int            `json:"chainId"`
	NetworkID      int            `json:"networkId"`
	NativeCurrency NativeCurrency `json:"nativeCurrency"`
	RPC            []string       `json:"rpc"`
	Explorers      []Explorer     `json:"explorers,omitempty"`
	InfoURL        string         `json:"infoURL,omitempty"`
}

type NativeCurrency struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}

type Explorer struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Standard string `json:"standard,omitempty"`
}

func SyncFromFile(input string, outputDir string) (int, error) {
	data, err := os.ReadFile(input)
	if err != nil {
		return 0, err
	}

	var chains []SourceChain
	if err := json.Unmarshal(data, &chains); err != nil {
		return 0, err
	}

	count := 0
	seen := make(map[string]int)
	for _, chain := range chains {
		if strings.EqualFold(chain.Status, "deprecated") {
			continue
		}
		rpcs := filterRPCs(chain.RPC)
		if len(rpcs) == 0 || chain.ShortName == "" || chain.ChainID == 0 {
			continue
		}

		category := "mainnet"
		if isTestnet(chain) {
			category = "test"
		}
		slug := slugify(chain.ShortName)
		key := filepath.Join(category, slug)
		if seen[key] > 0 {
			slug = fmt.Sprintf("%s-%d", slug, chain.ChainID)
			key = filepath.Join(category, slug)
		}
		seen[key]++

		out := OutputChain{
			Name:           chain.Name,
			Chain:          chain.Chain,
			ShortName:      chain.ShortName,
			Category:       category,
			ChainID:        chain.ChainID,
			NetworkID:      chain.NetworkID,
			NativeCurrency: chain.NativeCurrency,
			RPC:            rpcs,
			Explorers:      chain.Explorers,
			InfoURL:        chain.InfoURL,
		}
		if err := writeChain(filepath.Join(outputDir, category, slug, "chain.json"), out); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func filterRPCs(rpcs []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, rpc := range rpcs {
		rpc = strings.TrimSpace(rpc)
		if !isPublicHTTPRPC(rpc) || seen[rpc] {
			continue
		}
		seen[rpc] = true
		out = append(out, rpc)
	}
	sort.Strings(out)
	return out
}

func isPublicHTTPRPC(raw string) bool {
	if raw == "" {
		return false
	}
	lower := strings.ToLower(raw)
	blocked := []string{
		"${", "}", "<", ">", "api_key", "apikey", "api-key", "key=", "token=", "projectid",
		"your-", "your_", "yourkey", "localhost", "127.0.0.1", "0.0.0.0",
		"alchemy.com/v2/", "alchemy.com/v3/", "infura.io/v3/", "quiknode.pro/",
		"getblock.io/", "blastapi.io/",
	}
	for _, marker := range blocked {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" || parsed.Scheme == "http"
}

func isTestnet(chain SourceChain) bool {
	text := strings.ToLower(strings.Join([]string{
		chain.Name,
		chain.Chain,
		chain.ShortName,
	}, " "))
	markers := []string{
		"test", "testnet", "sepolia", "goerli", "holesky", "hoodi", "ropsten", "rinkeby", "kovan",
		"mumbai", "amoy", "fuji", "chapel", "alfajores", "nile", "shasta", "devnet", "sandbox",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return len(chain.Faucets) > 0 && !strings.Contains(text, "mainnet")
}

func slugify(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if keep {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeChain(path string, chain OutputChain) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
