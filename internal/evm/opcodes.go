package evm

type Opcode byte

const (
	STOP           Opcode = 0x00
	ADD            Opcode = 0x01
	MUL            Opcode = 0x02
	SUB            Opcode = 0x03
	DIV            Opcode = 0x04
	SDIV           Opcode = 0x05
	MOD            Opcode = 0x06
	SMOD           Opcode = 0x07
	ADDMOD         Opcode = 0x08
	MULMOD         Opcode = 0x09
	EXP            Opcode = 0x0a
	SIGNEXTEND     Opcode = 0x0b
	LT             Opcode = 0x10
	GT             Opcode = 0x11
	SLT            Opcode = 0x12
	SGT            Opcode = 0x13
	EQ             Opcode = 0x14
	ISZERO         Opcode = 0x15
	AND            Opcode = 0x16
	OR             Opcode = 0x17
	XOR            Opcode = 0x18
	NOT            Opcode = 0x19
	BYTE           Opcode = 0x1a
	SHL            Opcode = 0x1b
	SHR            Opcode = 0x1c
	SAR            Opcode = 0x1d
	SHA3           Opcode = 0x20
	ADDRESS        Opcode = 0x30
	BALANCE        Opcode = 0x31
	ORIGIN         Opcode = 0x32
	CALLER         Opcode = 0x33
	CALLVALUE      Opcode = 0x34
	CALLDATALOAD   Opcode = 0x35
	CALLDATASIZE   Opcode = 0x36
	CALLDATACOPY   Opcode = 0x37
	CODESIZE       Opcode = 0x38
	CODECOPY       Opcode = 0x39
	GASPRICE       Opcode = 0x3a
	EXTCODESIZE    Opcode = 0x3b
	EXTCODECOPY    Opcode = 0x3c
	RETURNDATASIZE Opcode = 0x3d
	RETURNDATACOPY Opcode = 0x3e
	EXTCODEHASH    Opcode = 0x3f
	BLOCKHASH      Opcode = 0x40
	COINBASE       Opcode = 0x41
	TIMESTAMP      Opcode = 0x42
	NUMBER         Opcode = 0x43
	DIFFICULTY     Opcode = 0x44
	GASLIMIT       Opcode = 0x45
	CHAINID        Opcode = 0x46
	SELFBALANCE    Opcode = 0x47
	BASEFEE        Opcode = 0x48
	POP            Opcode = 0x50
	MLOAD          Opcode = 0x51
	MSTORE         Opcode = 0x52
	MSTORE8        Opcode = 0x53
	SLOAD          Opcode = 0x54
	SSTORE         Opcode = 0x55
	JUMP           Opcode = 0x56
	JUMPI          Opcode = 0x57
	PC             Opcode = 0x58
	MSIZE          Opcode = 0x59
	GAS            Opcode = 0x5a
	JUMPDEST       Opcode = 0x5b
	PUSH0          Opcode = 0x5f
	PUSH1          Opcode = 0x60
	PUSH2          Opcode = 0x61
	PUSH3          Opcode = 0x62
	PUSH4          Opcode = 0x63
	PUSH5          Opcode = 0x64
	PUSH6          Opcode = 0x65
	PUSH7          Opcode = 0x66
	PUSH8          Opcode = 0x67
	PUSH9          Opcode = 0x68
	PUSH10         Opcode = 0x69
	PUSH11         Opcode = 0x6a
	PUSH12         Opcode = 0x6b
	PUSH13         Opcode = 0x6c
	PUSH14         Opcode = 0x6d
	PUSH15         Opcode = 0x6e
	PUSH16         Opcode = 0x6f
	PUSH17         Opcode = 0x70
	PUSH18         Opcode = 0x71
	PUSH19         Opcode = 0x72
	PUSH20         Opcode = 0x73
	PUSH21         Opcode = 0x74
	PUSH22         Opcode = 0x75
	PUSH23         Opcode = 0x76
	PUSH24         Opcode = 0x77
	PUSH25         Opcode = 0x78
	PUSH26         Opcode = 0x79
	PUSH27         Opcode = 0x7a
	PUSH28         Opcode = 0x7b
	PUSH29         Opcode = 0x7c
	PUSH30         Opcode = 0x7d
	PUSH31         Opcode = 0x7e
	PUSH32         Opcode = 0x7f
	DUP1           Opcode = 0x80
	DUP2           Opcode = 0x81
	DUP3           Opcode = 0x82
	DUP4           Opcode = 0x83
	DUP5           Opcode = 0x84
	DUP6           Opcode = 0x85
	DUP7           Opcode = 0x86
	DUP8           Opcode = 0x87
	DUP9           Opcode = 0x88
	DUP10          Opcode = 0x89
	DUP11          Opcode = 0x8a
	DUP12          Opcode = 0x8b
	DUP13          Opcode = 0x8c
	DUP14          Opcode = 0x8d
	DUP15          Opcode = 0x8e
	DUP16          Opcode = 0x8f
	SWAP1          Opcode = 0x90
	SWAP2          Opcode = 0x91
	SWAP3          Opcode = 0x92
	SWAP4          Opcode = 0x93
	SWAP5          Opcode = 0x94
	SWAP6          Opcode = 0x95
	SWAP7          Opcode = 0x96
	SWAP8          Opcode = 0x97
	SWAP9          Opcode = 0x98
	SWAP10         Opcode = 0x99
	SWAP11         Opcode = 0x9a
	SWAP12         Opcode = 0x9b
	SWAP13         Opcode = 0x9c
	SWAP14         Opcode = 0x9d
	SWAP15         Opcode = 0x9e
	SWAP16         Opcode = 0x9f
	LOG0           Opcode = 0xa0
	LOG1           Opcode = 0xa1
	LOG2           Opcode = 0xa2
	LOG3           Opcode = 0xa3
	LOG4           Opcode = 0xa4
	CREATE         Opcode = 0xf0
	CALL           Opcode = 0xf1
	CALLCODE       Opcode = 0xf2
	RETURN         Opcode = 0xf3
	DELEGATECALL   Opcode = 0xf4
	CREATE2        Opcode = 0xf5
	STATICCALL     Opcode = 0xfa
	REVERT         Opcode = 0xfd
	INVALID        Opcode = 0xfe
	SELFDESTRUCT   Opcode = 0xff
)

var opcodeNames = [256]string{
	0x00: "STOP", 0x01: "ADD", 0x02: "MUL", 0x03: "SUB", 0x04: "DIV",
	0x05: "SDIV", 0x06: "MOD", 0x07: "SMOD", 0x08: "ADDMOD", 0x09: "MULMOD",
	0x0a: "EXP", 0x0b: "SIGNEXTEND",
	0x10: "LT", 0x11: "GT", 0x12: "SLT", 0x13: "SGT", 0x14: "EQ",
	0x15: "ISZERO", 0x16: "AND", 0x17: "OR", 0x18: "XOR", 0x19: "NOT",
	0x1a: "BYTE", 0x1b: "SHL", 0x1c: "SHR", 0x1d: "SAR",
	0x20: "SHA3",
	0x30: "ADDRESS", 0x31: "BALANCE", 0x32: "ORIGIN", 0x33: "CALLER",
	0x34: "CALLVALUE", 0x35: "CALLDATALOAD", 0x36: "CALLDATASIZE", 0x37: "CALLDATACOPY",
	0x38: "CODESIZE", 0x39: "CODECOPY", 0x3a: "GASPRICE", 0x3b: "EXTCODESIZE",
	0x3c: "EXTCODECOPY", 0x3d: "RETURNDATASIZE", 0x3e: "RETURNDATACOPY", 0x3f: "EXTCODEHASH",
	0x40: "BLOCKHASH", 0x41: "COINBASE", 0x42: "TIMESTAMP", 0x43: "NUMBER",
	0x44: "DIFFICULTY", 0x45: "GASLIMIT", 0x46: "CHAINID", 0x47: "SELFBALANCE", 0x48: "BASEFEE",
	0x50: "POP", 0x51: "MLOAD", 0x52: "MSTORE", 0x53: "MSTORE8",
	0x54: "SLOAD", 0x55: "SSTORE", 0x56: "JUMP", 0x57: "JUMPI",
	0x58: "PC", 0x59: "MSIZE", 0x5a: "GAS", 0x5b: "JUMPDEST", 0x5f: "PUSH0",
	0x60: "PUSH1", 0x61: "PUSH2", 0x62: "PUSH3", 0x63: "PUSH4", 0x64: "PUSH5",
	0x65: "PUSH6", 0x66: "PUSH7", 0x67: "PUSH8", 0x68: "PUSH9", 0x69: "PUSH10",
	0x6a: "PUSH11", 0x6b: "PUSH12", 0x6c: "PUSH13", 0x6d: "PUSH14", 0x6e: "PUSH15",
	0x6f: "PUSH16", 0x70: "PUSH17", 0x71: "PUSH18", 0x72: "PUSH19", 0x73: "PUSH20",
	0x74: "PUSH21", 0x75: "PUSH22", 0x76: "PUSH23", 0x77: "PUSH24", 0x78: "PUSH25",
	0x79: "PUSH26", 0x7a: "PUSH27", 0x7b: "PUSH28", 0x7c: "PUSH29", 0x7d: "PUSH30",
	0x7e: "PUSH31", 0x7f: "PUSH32",
	0x80: "DUP1", 0x81: "DUP2", 0x82: "DUP3", 0x83: "DUP4", 0x84: "DUP5",
	0x85: "DUP6", 0x86: "DUP7", 0x87: "DUP8", 0x88: "DUP9", 0x89: "DUP10",
	0x8a: "DUP11", 0x8b: "DUP12", 0x8c: "DUP13", 0x8d: "DUP14", 0x8e: "DUP15", 0x8f: "DUP16",
	0x90: "SWAP1", 0x91: "SWAP2", 0x92: "SWAP3", 0x93: "SWAP4", 0x94: "SWAP5",
	0x95: "SWAP6", 0x96: "SWAP7", 0x97: "SWAP8", 0x98: "SWAP9", 0x99: "SWAP10",
	0x9a: "SWAP11", 0x9b: "SWAP12", 0x9c: "SWAP13", 0x9d: "SWAP14", 0x9e: "SWAP15", 0x9f: "SWAP16",
	0xa0: "LOG0", 0xa1: "LOG1", 0xa2: "LOG2", 0xa3: "LOG3", 0xa4: "LOG4",
	0xf0: "CREATE", 0xf1: "CALL", 0xf2: "CALLCODE", 0xf3: "RETURN",
	0xf4: "DELEGATECALL", 0xf5: "CREATE2", 0xfa: "STATICCALL",
	0xfd: "REVERT", 0xfe: "INVALID", 0xff: "SELFDESTRUCT",
}

func (o Opcode) String() string {
	if s := opcodeNames[o]; s != "" {
		return s
	}
	return "UNKNOWN"
}

// pushSize returns how many immediate bytes follow a PUSH opcode (0 if not PUSH).
func pushSize(o Opcode) int {
	if o >= PUSH1 && o <= PUSH32 {
		return int(o-PUSH1) + 1
	}
	if o == PUSH0 {
		return 0
	}
	return 0
}
