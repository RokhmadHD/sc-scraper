package evm

import "encoding/hex"

// Pattern names
const (
	PatternERC20        = "ERC20"
	PatternERC721       = "ERC721"
	PatternProxy        = "Proxy"
	PatternSelfdestruct = "Selfdestruct"
	PatternDelegatecall = "Delegatecall"
	PatternCreate2      = "Create2"
	PatternPayable      = "Payable"
)

// Well-known 4-byte function selectors.
var (
	// ERC20
	erc20Sigs = set(
		"a9059cbb", // transfer(address,uint256)
		"23b872dd", // transferFrom(address,address,uint256)
		"095ea7b3", // approve(address,uint256)
		"70a08231", // balanceOf(address)
		"18160ddd", // totalSupply()
		"dd62ed3e", // allowance(address,address)
	)
	// ERC721
	erc721Sigs = set(
		"6352211e", // ownerOf(uint256)
		"42842e0e", // safeTransferFrom(address,address,uint256)
		"b88d4fde", // safeTransferFrom(address,address,uint256,bytes)
		"a22cb465", // setApprovalForAll(address,bool)
		"081812fc", // getApproved(uint256)
		"e985e9c5", // isApprovedForAll(address,address)
	)
)

func set(sigs ...string) map[string]bool {
	m := make(map[string]bool, len(sigs))
	for _, s := range sigs {
		m[s] = true
	}
	return m
}

// DetectPatterns returns a list of pattern names found in the bytecode.
func DetectPatterns(code []byte) []string {
	insns := Disassemble(code)

	// collect PUSH4 selectors
	push4 := make(map[string]bool)
	hasDelegatecall := false
	hasSelfdestruct := false
	hasCreate2 := false
	hasCallvalue := false

	for _, ins := range insns {
		switch ins.Op {
		case DELEGATECALL:
			hasDelegatecall = true
		case SELFDESTRUCT:
			hasSelfdestruct = true
		case CREATE2:
			hasCreate2 = true
		case CALLVALUE:
			hasCallvalue = true
		case PUSH4:
			if len(ins.Operand) == 4 {
				push4[hex.EncodeToString(ins.Operand)] = true
			}
		}
	}

	var patterns []string

	if matchSigs(push4, erc20Sigs, 4) {
		patterns = append(patterns, PatternERC20)
	}
	if matchSigs(push4, erc721Sigs, 4) {
		patterns = append(patterns, PatternERC721)
	}
	// Proxy: has DELEGATECALL and very few function selectors (dispatcher forwards everything)
	if hasDelegatecall {
		patterns = append(patterns, PatternProxy)
		patterns = append(patterns, PatternDelegatecall)
	}
	if hasSelfdestruct {
		patterns = append(patterns, PatternSelfdestruct)
	}
	if hasCreate2 {
		patterns = append(patterns, PatternCreate2)
	}
	if hasCallvalue {
		patterns = append(patterns, PatternPayable)
	}
	return patterns
}

// matchSigs returns true if at least `threshold` sigs from `want` appear in `found`.
func matchSigs(found map[string]bool, want map[string]bool, threshold int) bool {
	n := 0
	for sig := range want {
		if found[sig] {
			n++
			if n >= threshold {
				return true
			}
		}
	}
	return false
}
