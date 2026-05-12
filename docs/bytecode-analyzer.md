# Bytecode Analyzer

Tool untuk menganalisa file `.evm` hasil download dari scraper.

## Cara Jalankan

```bash
go run ./cmd/bytecode-analyzer [flags] <file.evm> [file2.evm ...]
```

Build binary:

```bash
go build -o bin/bytecode-analyzer ./cmd/bytecode-analyzer
```

---

## Flags

| Flag | Default | Keterangan |
|------|---------|------------|
| `-patterns` | false | Deteksi jenis contract (ERC20, Proxy, dll) |
| `-stats` | false | Statistik opcode + function selectors |
| `-disasm` | false | Cetak disassembly lengkap |
| `-similar` | false | Hitung kemiripan antar file (butuh 2+ file) |
| `-min-score` | 0.8 | Threshold similarity (0.0–1.0) |
| `-top` | 10 | Jumlah opcode teratas yang ditampilkan di stats |

Flag bisa dikombinasikan bebas.

---

## Fitur 1 — Pattern Detection (`-patterns`)

Mendeteksi karakteristik contract berdasarkan opcode dan 4-byte function selector yang ada di bytecode.

```bash
go run ./cmd/bytecode-analyzer -patterns 0xABC123.evm
```

Output:
```
=== 0xABC123.evm (2048 bytes) ===
Patterns: ERC20, Payable
```

### Daftar Pattern

| Pattern | Cara Deteksi | Artinya |
|---------|-------------|---------|
| `ERC20` | Minimal 4 dari 6 selector ERC20 ditemukan | Token fungible standar |
| `ERC721` | Minimal 4 dari 6 selector ERC721 ditemukan | NFT standar |
| `Proxy` | Ada opcode `DELEGATECALL` | Contract proxy/upgradeable |
| `Delegatecall` | Ada opcode `DELEGATECALL` | Bisa forward call ke contract lain |
| `Selfdestruct` | Ada opcode `SELFDESTRUCT` | Bisa menghancurkan diri sendiri |
| `Create2` | Ada opcode `CREATE2` | Bisa deploy contract dengan address deterministik |
| `Payable` | Ada opcode `CALLVALUE` | Menerima ETH |

### Selector ERC20 yang Dicek

| Selector | Fungsi |
|----------|--------|
| `0xa9059cbb` | `transfer(address,uint256)` |
| `0x23b872dd` | `transferFrom(address,address,uint256)` |
| `0x095ea7b3` | `approve(address,uint256)` |
| `0x70a08231` | `balanceOf(address)` |
| `0x18160ddd` | `totalSupply()` |
| `0xdd62ed3e` | `allowance(address,address)` |

### Selector ERC721 yang Dicek

| Selector | Fungsi |
|----------|--------|
| `0x6352211e` | `ownerOf(uint256)` |
| `0x42842e0e` | `safeTransferFrom(address,address,uint256)` |
| `0xb88d4fde` | `safeTransferFrom(address,address,uint256,bytes)` |
| `0xa22cb465` | `setApprovalForAll(address,bool)` |
| `0x081812fc` | `getApproved(uint256)` |
| `0xe985e9c5` | `isApprovedForAll(address,address)` |

> **Catatan:** Threshold 4 dari 6 dipakai supaya tidak false positive. Contract yang hanya implement sebagian interface tidak akan terdeteksi.

---

## Fitur 2 — Opcode Stats (`-stats`)

Menampilkan frekuensi opcode dan function selector yang ditemukan.

```bash
go run ./cmd/bytecode-analyzer -stats -top 15 0xABC123.evm
```

Output:
```
=== 0xABC123.evm (2048 bytes) ===
Unique opcodes: 34
Function selectors: 0xa9059cbb 0x70a08231 0x18160ddd 0x095ea7b3
Top 15 opcodes:
  PUSH1            142
  JUMPDEST          98
  PUSH2             87
  DUP1              61
  ...
```

### Penjelasan Field

- **Unique opcodes** — berapa banyak jenis opcode berbeda yang dipakai. Contract sederhana biasanya 20–30, contract kompleks bisa 40+.
- **Function selectors** — semua `PUSH4` yang ditemukan. Di Solidity, dispatcher function menggunakan `PUSH4` + `EQ` untuk routing. Ini bisa dipakai untuk identifikasi fungsi tanpa source code.
- **Top N opcodes** — opcode paling sering muncul. `PUSH1`, `JUMPDEST`, `DUP*` biasanya dominan di semua contract.

---

## Fitur 3 — Disassembly (`-disasm`)

Decode bytecode menjadi instruksi EVM satu per satu.

```bash
go run ./cmd/bytecode-analyzer -disasm 0xABC123.evm
```

Output:
```
=== 0xABC123.evm (2048 bytes) ===
Disassembly:
  0000  PUSH1            0x80
  0002  PUSH1            0x40
  0004  MSTORE
  0005  PUSH1            0x04
  0007  CALLDATASIZE
  0008  LT
  0009  PUSH2            0x0058
  000c  JUMPI
  000d  PUSH1            0x00
  000f  CALLDATALOAD
  0010  PUSH1            0xe0
  0012  SHR
  0013  DUP1
  0014  PUSH4            0xa9059cbb
  0019  EQ
  001a  PUSH2            0x0063
  001d  JUMPI
  ...
```

### Cara Baca Output

```
<PC>  <OPCODE>  [OPERAND]
```

- **PC** (Program Counter) — posisi byte instruksi dalam bytecode, dalam hex.
- **OPCODE** — nama instruksi EVM.
- **OPERAND** — hanya ada untuk `PUSH1`–`PUSH32`, yaitu data yang di-push ke stack.

### Pola Umum yang Sering Muncul

**Free memory pointer (awal hampir semua Solidity contract):**
```
0000  PUSH1   0x80
0002  PUSH1   0x40
0004  MSTORE
```

**Function dispatcher:**
```
CALLDATALOAD
PUSH1   0xe0
SHR                  ← ambil 4 byte pertama calldata (function selector)
DUP1
PUSH4   0xa9059cbb   ← bandingkan dengan selector
EQ
PUSH2   0x0063       ← jump ke handler jika cocok
JUMPI
```

**Revert dengan pesan:**
```
PUSH1   0x00
DUP1
REVERT
```

---

## Fitur 4 — Similarity (`-similar`)

Menghitung kemiripan antar bytecode menggunakan **Jaccard similarity** berbasis set opcode unik.

```bash
go run ./cmd/bytecode-analyzer -similar -min-score 0.7 data/bytecode/*.evm
```

Output:
```
=== Similarity (min=0.70) ===
  0.9412  0xAAA.evm  <->  0xBBB.evm
  0.8750  0xCCC.evm  <->  0xDDD.evm
  0.7143  0xEEE.evm  <->  0xFFF.evm
```

### Cara Kerja Jaccard Similarity

Jaccard mengukur seberapa banyak opcode yang sama dipakai oleh dua contract:

```
score = |opcode_set_A ∩ opcode_set_B| / |opcode_set_A ∪ opcode_set_B|
```

Contoh:
- Contract A pakai opcode: `{PUSH1, ADD, SSTORE, RETURN}` → 4 opcode
- Contract B pakai opcode: `{PUSH1, MUL, SSTORE, RETURN}` → 4 opcode
- Irisan: `{PUSH1, SSTORE, RETURN}` → 3
- Gabungan: `{PUSH1, ADD, MUL, SSTORE, RETURN}` → 5
- Score: `3/5 = 0.60`

### Interpretasi Score

| Score | Artinya |
|-------|---------|
| `1.00` | Bytecode identik (atau compile dari template yang sama) |
| `0.85–0.99` | Sangat mirip — kemungkinan fork atau versi berbeda dari contract yang sama |
| `0.70–0.84` | Mirip — mungkin pakai library/framework yang sama (OpenZeppelin, dll) |
| `< 0.70` | Berbeda signifikan |

> **Keterbatasan:** Jaccard berbasis set opcode, bukan urutan. Dua contract dengan logika berbeda tapi pakai opcode yang sama akan tetap terlihat mirip. Untuk similarity yang lebih akurat, bisa pakai n-gram opcode (belum diimplementasi).

### Gunakan untuk Apa?

- Temukan contract yang di-deploy berkali-kali (copy-paste deploy)
- Identifikasi contract dari factory yang sama
- Deteksi scam contract yang mirip satu sama lain

---

## Contoh Workflow Lengkap

### 1. Scrape + download bytecode

```bash
go run ./cmd/contract-scraper --last 100 --download-bytecode --concurrency 4
```

Hasil bytecode tersimpan di `data/bytecode/*.evm`.

### 2. Analisa semua contract

```bash
go run ./cmd/bytecode-analyzer -patterns -stats data/bytecode/*.evm
```

### 3. Cari contract yang mirip

```bash
go run ./cmd/bytecode-analyzer -similar -min-score 0.85 data/bytecode/*.evm
```

### 4. Inspect contract tertentu lebih dalam

```bash
go run ./cmd/bytecode-analyzer -patterns -stats -disasm data/bytecode/0xABC123.evm | less
```

---

## Format File `.evm`

File `.evm` berisi hex string dari deployed bytecode, tanpa prefix `0x`, satu baris:

```
6080604052600436106100...
```

Ini adalah **deployed bytecode** (runtime code), bukan creation bytecode. Artinya kode constructor sudah tidak ada — yang tersimpan adalah kode yang berjalan saat contract dipanggil.
