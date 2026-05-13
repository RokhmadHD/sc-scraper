package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"

	"scrape-smart-contract/internal/scraper"
)

type PostgresSink struct {
	db    *sql.DB
	table string
}

func OpenPostgres(ctx context.Context, databaseURL string, table string, reset bool) (*PostgresSink, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("postgres URL is required")
	}
	table, err := normalizeTableName(table)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	sink := &PostgresSink{db: db, table: table}
	if err := sink.db.PingContext(ctx); err != nil {
		_ = sink.Close()
		return nil, err
	}
	if reset {
		if err := sink.resetSchema(ctx); err != nil {
			_ = sink.Close()
			return nil, err
		}
	}
	if err := sink.ensureSchema(ctx); err != nil {
		_ = sink.Close()
		return nil, err
	}
	return sink, nil
}

func (s *PostgresSink) Close() error {
	return s.db.Close()
}

func (s *PostgresSink) Save(ctx context.Context, creation scraper.ContractCreation) error {
	query := fmt.Sprintf(`
INSERT INTO %s (
	chain_id, network, block_number, transaction_hash, contract_address, creator,
	status, gas_used, effective_gas_price, balance_wei, token_address, token_balance,
	token_decimals, bytecode_size, bytecode, bytecode_path, block_timestamp, rpc_url
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9::numeric, $10::numeric, NULLIF($11, ''), NULLIF($12, '')::numeric,
	NULLIF($13, 0), NULLIF($14, 0), NULLIF($15, ''), NULLIF($16, ''), $17, $18
)
ON CONFLICT (chain_id, contract_address) DO UPDATE SET
	network = EXCLUDED.network,
	block_number = EXCLUDED.block_number,
	creator = EXCLUDED.creator,
	status = EXCLUDED.status,
	gas_used = EXCLUDED.gas_used,
	effective_gas_price = EXCLUDED.effective_gas_price,
	balance_wei = EXCLUDED.balance_wei,
	token_address = EXCLUDED.token_address,
	token_balance = EXCLUDED.token_balance,
	token_decimals = EXCLUDED.token_decimals,
	bytecode_size = EXCLUDED.bytecode_size,
	bytecode = EXCLUDED.bytecode,
	bytecode_path = EXCLUDED.bytecode_path,
	block_timestamp = EXCLUDED.block_timestamp,
	rpc_url = EXCLUDED.rpc_url,
	updated_at = now()
`, s.table)
	_, err := s.db.ExecContext(
		ctx,
		query,
		creation.ChainID,
		creation.Network,
		creation.BlockNumber,
		creation.TransactionHash,
		creation.ContractAddress,
		creation.Creator,
		creation.Status,
		creation.GasUsed,
		creation.EffectiveGasPrice,
		creation.BalanceWei,
		creation.TokenAddress,
		creation.TokenBalance,
		creation.TokenDecimals,
		creation.BytecodeSize,
		creation.Bytecode,
		creation.BytecodePath,
		creation.Timestamp,
		creation.RPCURL,
	)
	return err
}

func (s *PostgresSink) ensureSchema(ctx context.Context) error {
	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	id bigserial PRIMARY KEY,
	chain_id bigint NOT NULL,
	network text NOT NULL,
	block_number bigint NOT NULL,
	transaction_hash text NOT NULL,
	contract_address text NOT NULL,
	creator text NOT NULL,
	status bigint NOT NULL,
	gas_used bigint NOT NULL,
	effective_gas_price numeric NOT NULL,
	balance_wei numeric NOT NULL,
	token_address text,
	token_balance numeric,
	token_decimals integer,
	bytecode_size integer,
	bytecode text,
	bytecode_path text,
	block_timestamp bigint NOT NULL,
	rpc_url text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (chain_id, contract_address)
);
CREATE INDEX IF NOT EXISTS %[1]s_chain_block_idx ON %[1]s (chain_id, block_number);
CREATE INDEX IF NOT EXISTS %[1]s_contract_idx ON %[1]s (contract_address);
`, s.table)
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *PostgresSink) resetSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE;`, s.table))
	return err
}

func normalizeTableName(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		table = "contract_creations"
	}
	for _, r := range table {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return "", fmt.Errorf("invalid postgres table name %q", table)
		}
	}
	return table, nil
}
