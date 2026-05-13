package eth

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

type HexUint64 uint64

func (h HexUint64) Uint64() uint64 {
	return uint64(h)
}

func (h HexUint64) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatUint(uint64(h), 10)), nil
}

func (h *HexUint64) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	value, err := ParseHexUint64(raw)
	if err != nil {
		return err
	}
	*h = HexUint64(value)
	return nil
}

func ParseHexUint64(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseUint(strings.TrimPrefix(raw, "0x"), 16, 64)
}

func ParseHexBigInt(raw string) (*big.Int, error) {
	value := new(big.Int)
	if raw == "" {
		return value, nil
	}
	if _, ok := value.SetString(strings.TrimPrefix(raw, "0x"), 16); !ok {
		return nil, fmt.Errorf("invalid hex integer %q", raw)
	}
	return value, nil
}

var Ether = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

func ParseEther(raw string) (*big.Int, error) {
	return ParseUnits(raw, 18)
}

func ParseUnits(raw string, decimals int) (*big.Int, error) {
	if raw == "" {
		return big.NewInt(0), nil
	}
	if decimals < 0 {
		return nil, fmt.Errorf("decimals must be greater than or equal to 0")
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("invalid decimal value %q", raw)
	}
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt(unit))
	if !scaled.IsInt() {
		return nil, fmt.Errorf("decimal value %q is too precise for %d decimals", raw, decimals)
	}
	return new(big.Int).Div(scaled.Num(), scaled.Denom()), nil
}

func FormatEther(wei *big.Int, precision int) string {
	return FormatUnits(wei, 18, precision)
}

func FormatUnits(value *big.Int, decimals int, precision int) string {
	if value == nil {
		value = big.NewInt(0)
	}
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	amount := new(big.Rat).SetFrac(value, unit)
	return amount.FloatString(precision)
}
