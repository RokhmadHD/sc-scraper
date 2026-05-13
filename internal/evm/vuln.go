package evm

import "fmt"

type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"
	SeverityInfo     Severity = "INFO"
)

type Vulnerability struct {
	ID       string
	Severity Severity
	Title    string
	Detail   string
	PC       int // -1 if not tied to a specific instruction
}

func (v Vulnerability) String() string {
	if v.PC >= 0 {
		return fmt.Sprintf("[%s] %s (pc=0x%04x): %s", v.Severity, v.Title, v.PC, v.Detail)
	}
	return fmt.Sprintf("[%s] %s: %s", v.Severity, v.Title, v.Detail)
}

// Audit runs all vulnerability checks and returns findings sorted by severity.
func Audit(code []byte) []Vulnerability {
	insns := Disassemble(code)
	var vulns []Vulnerability

	vulns = append(vulns, checkCallcode(insns)...)
	vulns = append(vulns, checkSelfdestruct(insns)...)
	vulns = append(vulns, checkSilentFailure(insns)...)
	vulns = append(vulns, checkUncheckedSend(insns)...)
	vulns = append(vulns, checkTxOrigin(insns)...)
	vulns = append(vulns, checkTimestamp(insns)...)
	vulns = append(vulns, checkHardcodedGas(insns)...)
	vulns = append(vulns, checkArbitraryDelegatecall(insns)...)
	vulns = append(vulns, checkReentrancy(insns)...)

	return sortBySeverity(vulns)
}

// checkCallcode detects use of deprecated CALLCODE opcode.
func checkCallcode(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	for _, ins := range insns {
		if ins.Op == CALLCODE {
			out = append(out, Vulnerability{
				ID:       "CALLCODE",
				Severity: SeverityHigh,
				Title:    "Deprecated CALLCODE",
				Detail:   "CALLCODE is deprecated since EIP-7. msg.sender inside callee is this contract, not the original caller. Use DELEGATECALL instead.",
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkSelfdestruct detects SELFDESTRUCT usage.
func checkSelfdestruct(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	for _, ins := range insns {
		if ins.Op == SELFDESTRUCT {
			out = append(out, Vulnerability{
				ID:       "SELFDESTRUCT",
				Severity: SeverityHigh,
				Title:    "SELFDESTRUCT present",
				Detail:   "Contract can be destroyed. If unprotected, anyone can trigger it and drain funds.",
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkSilentFailure detects CALL/CALLCODE/DELEGATECALL/STATICCALL whose return
// value is immediately popped (not checked).
func checkSilentFailure(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	callOps := map[Opcode]bool{CALL: true, CALLCODE: true, DELEGATECALL: true, STATICCALL: true}
	for i, ins := range insns {
		if !callOps[ins.Op] {
			continue
		}
		// check if next meaningful instruction is POP (return value discarded)
		if i+1 < len(insns) && insns[i+1].Op == POP {
			out = append(out, Vulnerability{
				ID:       "UNCHECKED_RETURN",
				Severity: SeverityCritical,
				Title:    "Unchecked call return value",
				Detail:   fmt.Sprintf("%s return value is immediately POP'd — failure is silently ignored.", ins.Op),
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkUncheckedSend detects CALL not followed by a success check.
// Skips over POP/SWAP stack-cleanup instructions (common in Solidity ABI encoding)
// and looks for ISZERO or JUMPI within 10 non-trivial instructions.
func checkUncheckedSend(insns []Instruction) []Vulnerability {
	// opcodes that are pure stack shuffling — skip when scanning for check
	stackNoise := map[Opcode]bool{
		POP: true, SWAP1: true, SWAP2: true, SWAP3: true, SWAP4: true,
		DUP1: true, DUP2: true, DUP3: true, DUP4: true,
	}
	var out []Vulnerability
	for i, ins := range insns {
		if ins.Op != CALL {
			continue
		}
		checked := false
		meaningful := 0
		for j := i + 1; j < len(insns) && meaningful < 10; j++ {
			op := insns[j].Op
			if op == ISZERO || op == JUMPI {
				checked = true
				break
			}
			// stop scanning if we hit a hard boundary
			if op == JUMP || op == RETURN || op == REVERT || op == STOP {
				break
			}
			if !stackNoise[op] {
				meaningful++
			}
		}
		if !checked {
			out = append(out, Vulnerability{
				ID:       "UNCHECKED_SEND",
				Severity: SeverityHigh,
				Title:    "Unchecked ETH send",
				Detail:   "CALL return value not checked. Failed ETH transfer may go unnoticed.",
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkTxOrigin detects use of ORIGIN (tx.origin) which is phishing-prone.
func checkTxOrigin(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	for _, ins := range insns {
		if ins.Op == ORIGIN {
			out = append(out, Vulnerability{
				ID:       "TX_ORIGIN",
				Severity: SeverityMedium,
				Title:    "tx.origin used (ORIGIN opcode)",
				Detail:   "Using tx.origin for auth is vulnerable to phishing attacks. Use msg.sender instead.",
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkTimestamp detects TIMESTAMP usage (block.timestamp manipulation).
func checkTimestamp(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	for _, ins := range insns {
		if ins.Op == TIMESTAMP {
			out = append(out, Vulnerability{
				ID:       "TIMESTAMP",
				Severity: SeverityLow,
				Title:    "block.timestamp dependency",
				Detail:   "Miners can manipulate block.timestamp by ~15 seconds. Avoid using it for critical logic.",
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkHardcodedGas detects GAS+SUB pattern (hardcoded gas reservation).
func checkHardcodedGas(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	for i, ins := range insns {
		if ins.Op == GAS && i+1 < len(insns) && insns[i+1].Op == SUB {
			out = append(out, Vulnerability{
				ID:       "HARDCODED_GAS",
				Severity: SeverityMedium,
				Title:    "Hardcoded gas reservation (GAS-SUB pattern)",
				Detail:   "Gas cost is hardcoded via GAS+SUB. EIP-150/EIP-2929 changed opcode costs — this may cause out-of-gas on modern chains.",
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkArbitraryDelegatecall detects DELEGATECALL where the target address
// may come from calldata (CALLDATALOAD before DELEGATECALL without hardcoded PUSH20).
func checkArbitraryDelegatecall(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	for i, ins := range insns {
		if ins.Op != DELEGATECALL {
			continue
		}
		// look back up to 10 instructions for CALLDATALOAD without an intervening PUSH20
		hasCalldataLoad := false
		hasPush20 := false
		for j := i - 1; j >= 0 && j >= i-10; j-- {
			if insns[j].Op == CALLDATALOAD {
				hasCalldataLoad = true
			}
			if insns[j].Op == PUSH20 {
				hasPush20 = true
			}
		}
		if hasCalldataLoad && !hasPush20 {
			out = append(out, Vulnerability{
				ID:       "ARBITRARY_DELEGATECALL",
				Severity: SeverityCritical,
				Title:    "Arbitrary DELEGATECALL target",
				Detail:   "DELEGATECALL target may be controlled by calldata. Attacker can execute arbitrary code in this contract's storage context.",
				PC:       ins.PC,
			})
		}
	}
	return out
}

// checkReentrancy detects SSTORE after CALL within the same function boundary.
// Resets tracking on JUMP/RETURN/REVERT/STOP to avoid cross-function false positives.
func checkReentrancy(insns []Instruction) []Vulnerability {
	var out []Vulnerability
	callOps := map[Opcode]bool{CALL: true, CALLCODE: true, DELEGATECALL: true}
	// opcodes that mark a function boundary (control flow leaves current scope)
	boundary := map[Opcode]bool{JUMP: true, RETURN: true, REVERT: true, STOP: true}
	lastCallPC := -1
	for _, ins := range insns {
		if boundary[ins.Op] {
			lastCallPC = -1
			continue
		}
		if callOps[ins.Op] {
			lastCallPC = ins.PC
			continue
		}
		if ins.Op == SSTORE && lastCallPC >= 0 {
			out = append(out, Vulnerability{
				ID:       "REENTRANCY",
				Severity: SeverityHigh,
				Title:    "Potential reentrancy",
				Detail:   fmt.Sprintf("SSTORE at pc=0x%04x follows external call at pc=0x%04x. State written after call — check-effects-interactions pattern may be violated.", ins.PC, lastCallPC),
				PC:       ins.PC,
			})
			lastCallPC = -1
		}
	}
	return out
}

var severityOrder = map[Severity]int{
	SeverityCritical: 0,
	SeverityHigh:     1,
	SeverityMedium:   2,
	SeverityLow:      3,
	SeverityInfo:     4,
}

func sortBySeverity(vulns []Vulnerability) []Vulnerability {
	for i := 1; i < len(vulns); i++ {
		for j := i; j > 0 && severityOrder[vulns[j].Severity] < severityOrder[vulns[j-1].Severity]; j-- {
			vulns[j], vulns[j-1] = vulns[j-1], vulns[j]
		}
	}
	return vulns
}
