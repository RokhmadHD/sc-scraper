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
	if raw == "" {
		return big.NewInt(0), nil
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("invalid ether value %q", raw)
	}
	wei := new(big.Rat).Mul(value, new(big.Rat).SetInt(Ether))
	if !wei.IsInt() {
		return nil, fmt.Errorf("ether value %q is too precise for wei", raw)
	}
	return new(big.Int).Div(wei.Num(), wei.Denom()), nil
}

func FormatEther(wei *big.Int, precision int) string {
	if wei == nil {
		wei = big.NewInt(0)
	}
	value := new(big.Rat).SetFrac(wei, Ether)
	return value.FloatString(precision)
}
