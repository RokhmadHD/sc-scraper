# Scrape Smart Contract

Scraper awal untuk menemukan smart contract baru di EVM chain lewat public RPC Chainlist-style.

Project ini pakai Go supaya lebih siap untuk scraping paralel dan long-running.

## Fitur

- Public RPC fallback dari `chains/mainnet|test/<shortName>/chain.json`.
- Scrape block range tertentu.
- Deteksi transaksi contract creation (`to == null`) dari receipt.
- Simpan hasil sebagai JSON Lines (`.jsonl`) ke folder run timestamp.
- Bisa lanjutkan run berikutnya dengan `--resume`.
- Scrape paralel dengan `--concurrency`.
- Filter minimum balance contract dengan `--min-balance`.
- Download deployed bytecode dengan `--download-bytecode`.
- Progress bar terminal dengan `--progress`.
- Console log contract ditemukan dengan `--print-found`.
- Tanpa dependency eksternal untuk versi awal.

## Cara Pakai

```bash
go run ./cmd/contract-scraper --latest
```

Pilih chain dari folder `chains`:

```bash
go run ./cmd/contract-scraper --chain base --latest
go run ./cmd/contract-scraper --chain sep --latest
```

Scrape 5 block terakhir:

```bash
go run ./cmd/contract-scraper --last 5
```

Default output tidak menimpa data lama. Setiap run masuk ke folder baru:

```text
data/2026-05-13_143012/contracts.jsonl
data/2026-05-13_143012/bytecode/
```

Scrape paralel, filter balance, dan download bytecode:

```bash
go run ./cmd/contract-scraper --last 100 --concurrency 4 --min-balance 0.5 --download-bytecode
```

Tampilkan progress:

```bash
go run ./cmd/contract-scraper --last 100 --concurrency 4 --progress
```

Console akan menampilkan contract yang ditemukan:

```text
4:27PM INF Current block : 14910116
4:27PM INF Contract address : 0x...
4:27PM INF Contract balance : 18.75
4:27PM INF ----------------------------------------------
```

Matikan log ini jika ingin output lebih sepi:

```bash
go run ./cmd/contract-scraper --last 100 --print-found=false
```

Scrape block range tertentu:

```bash
go run ./cmd/contract-scraper --start-block 22000000 --end-block 22000010
```

Pakai RPC sendiri:

```bash
go run ./cmd/contract-scraper --rpc-url https://ethereum.publicnode.com --last 10
```

Lewati beberapa RPC pertama jika endpoint public sedang menolak request:

```bash
go run ./cmd/contract-scraper --chain eth --skip-rpc 2 --last 10
```

## Update RPC Chains

Download `chains.json` dari Chainlist/ethereum-lists lalu split ke folder lokal:

```bash
curl -L https://chainid.network/chains.json -o /tmp/chains.json
go run ./cmd/contract-scraper --sync-chains --chains-input /tmp/chains.json --chains-output chains
```

Command lama `chains-sync` tetap tersedia:

```bash
go run ./cmd/chains-sync --input /tmp/chains.json --output chains
```

Struktur output:

```text
chains/mainnet/eth/chain.json
chains/mainnet/base/chain.json
chains/test/sep/chain.json
```

RPC yang disimpan sudah difilter:

- hanya `http`/`https`
- tanpa placeholder seperti `${API_KEY}`
- tanpa localhost
- tanpa provider URL yang jelas membutuhkan key seperti Infura, Alchemy, QuickNode, GetBlock, dan BlastAPI

Resume dari output yang sudah ada:

```bash
go run ./cmd/contract-scraper --last 100 --output data/2026-05-13_143012/contracts.jsonl --resume
```

Append manual ke file yang sama:

```bash
go run ./cmd/contract-scraper --last 100 --output data/contracts.jsonl --append
```

Build binary:

```bash
go build -o bin/contract-scraper ./cmd/contract-scraper
```

Install dari GitHub Release:

```bash
curl -fsSL https://raw.githubusercontent.com/RokhmadHD/sc-scraper/main/installer.sh | sh
```

Install versi tertentu atau ke folder lokal:

```bash
VERSION=v1.0.1 INSTALL_DIR="$HOME/.local/bin" sh installer.sh
```

## Output

Setiap baris adalah satu contract creation:

```json
{"chain_id":1,"network":"ethereum","block_number":22000000,"transaction_hash":"0x...","contract_address":"0x...","creator":"0x...","status":1,"gas_used":123456,"effective_gas_price":"1000000000","balance_wei":"0","bytecode_size":42,"bytecode_path":"data/bytecode/0x....evm","timestamp":1740000000,"rpc_url":"https://ethereum.publicnode.com"}
```

## Catatan

Public RPC bisa rate limited atau tidak stabil. Scraper akan mencoba endpoint berikutnya bila request gagal.
Naikkan `--concurrency` pelan-pelan karena public RPC sering membatasi request.
