package evm

// opcodeSet returns the set of unique opcodes in a bytecode.
func opcodeSet(code []byte) map[Opcode]bool {
	s := make(map[Opcode]bool)
	for _, ins := range Disassemble(code) {
		s[ins.Op] = true
	}
	return s
}

// Jaccard returns the Jaccard similarity (0.0–1.0) between two bytecodes
// based on their unique opcode sets.
func Jaccard(a, b []byte) float64 {
	sa := opcodeSet(a)
	sb := opcodeSet(b)

	intersection := 0
	for op := range sa {
		if sb[op] {
			intersection++
		}
	}
	union := len(sa) + len(sb) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// SimilarityResult holds a pairwise similarity score.
type SimilarityResult struct {
	A     string
	B     string
	Score float64
}

// CompareAll computes pairwise Jaccard similarity for a map of name→bytecode.
// Only pairs with score >= minScore are returned, sorted by score desc.
func CompareAll(files map[string][]byte, minScore float64) []SimilarityResult {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}

	var results []SimilarityResult
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			score := Jaccard(files[names[i]], files[names[j]])
			if score >= minScore {
				results = append(results, SimilarityResult{
					A:     names[i],
					B:     names[j],
					Score: score,
				})
			}
		}
	}

	// sort desc
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
	return results
}
