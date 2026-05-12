package evm

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Instruction is a decoded EVM instruction.
type Instruction struct {
	PC      int
	Op      Opcode
	Operand []byte // only set for PUSH*
}

func (i Instruction) String() string {
	if len(i.Operand) > 0 {
		return fmt.Sprintf("%04x  %-16s 0x%s", i.PC, i.Op, hex.EncodeToString(i.Operand))
	}
	return fmt.Sprintf("%04x  %s", i.PC, i.Op)
}

// Disassemble decodes raw bytecode into a list of instructions.
func Disassemble(code []byte) []Instruction {
	var out []Instruction
	for i := 0; i < len(code); {
		op := Opcode(code[i])
		ins := Instruction{PC: i, Op: op}
		i++
		if n := pushSize(op); n > 0 {
			end := i + n
			if end > len(code) {
				end = len(code)
			}
			ins.Operand = code[i:end]
			i = end
		}
		out = append(out, ins)
	}
	return out
}

// OpcodeCount holds frequency of each opcode.
type OpcodeCount struct {
	Op    Opcode
	Count int
}

// Stats holds analysis results for a bytecode.
type Stats struct {
	Size         int
	UniqueOps    int
	Counts       []OpcodeCount // sorted by count desc
	FunctionSigs []string      // 4-byte selectors found via PUSH4
}

// Analyze returns opcode statistics for the given bytecode.
func Analyze(code []byte) Stats {
	freq := make(map[Opcode]int)
	var sigs []string
	seen := make(map[string]bool)

	insns := Disassemble(code)
	for _, ins := range insns {
		freq[ins.Op]++
		// collect 4-byte function selectors (PUSH4 in dispatcher)
		if ins.Op == PUSH4 && len(ins.Operand) == 4 {
			sig := "0x" + hex.EncodeToString(ins.Operand)
			if !seen[sig] {
				seen[sig] = true
				sigs = append(sigs, sig)
			}
		}
	}

	counts := make([]OpcodeCount, 0, len(freq))
	for op, n := range freq {
		counts = append(counts, OpcodeCount{op, n})
	}
	sort.Slice(counts, func(i, j int) bool {
		return counts[i].Count > counts[j].Count
	})

	return Stats{
		Size:         len(code),
		UniqueOps:    len(freq),
		Counts:       counts,
		FunctionSigs: sigs,
	}
}

// DisassembleHex decodes a hex string (with or without 0x prefix) and disassembles it.
func DisassembleHex(raw string) ([]Instruction, error) {
	code, err := hexDecode(raw)
	if err != nil {
		return nil, err
	}
	return Disassemble(code), nil
}

// AnalyzeHex decodes a hex string and returns stats.
func AnalyzeHex(raw string) (Stats, error) {
	code, err := hexDecode(raw)
	if err != nil {
		return Stats{}, err
	}
	return Analyze(code), nil
}

func hexDecode(raw string) ([]byte, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "0x"))
	return hex.DecodeString(raw)
}
