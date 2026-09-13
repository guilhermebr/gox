// Package monetary provides types and functions for handling monetary values
// with precise arithmetic using big.Int for amounts and support for various
// fiat currencies and cryptocurrencies.
//
// The package ensures precision by storing amounts as integers in the smallest
// unit of the currency (e.g., cents for USD, satoshis for BTC).
//
// Example usage:
//
//	amount := big.NewInt(10050) // 100.50 in cents
//	brl, _ := NewMonetary(BRL, amount)
//	fmt.Println(brl.String()) // [BRL (R$) 100.50]
package monetary

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// Monetary is an exact amount of an Asset, stored as an integer in the asset's
// smallest unit (e.g. cents for USD, satoshi for BTC). All arithmetic is exact.
type Monetary struct {
	Asset  Asset    `json:"asset"`
	Amount *big.Int `json:"amount"`
}

// String renders the asset and the amount with the asset's decimal precision.
func (m Monetary) String() string {
	if m.Amount == nil {
		return fmt.Sprintf("[%s nil]", m.Asset.String())
	}
	// Format with decimal places based on precision
	return fmt.Sprintf("[%s %s]", m.Asset.String(), m.FormatAmount())
}

// GetAsset returns the asset this amount is denominated in.
func (m Monetary) GetAsset() Asset { return m.Asset }

// NewMonetary creates an amount in the asset's smallest unit. It rejects nil
// and negative amounts.
func NewMonetary(asset Asset, amount *big.Int) (*Monetary, error) {
	if amount == nil {
		return nil, ErrNilAmount
	}
	if amount.Sign() < 0 {
		return nil, ErrNegativeAmount
	}
	return &Monetary{Asset: asset, Amount: new(big.Int).Set(amount)}, nil
}

// NewMonetaryFromString parses a decimal string such as "100.50" using the
// asset's precision.
func NewMonetaryFromString(asset Asset, amountStr string) (*Monetary, error) {
	if amountStr == "" {
		return nil, fmt.Errorf("amount string cannot be empty")
	}

	// Parse the decimal value
	decimal, ok := new(big.Float).SetString(amountStr)
	if !ok {
		return nil, fmt.Errorf("invalid decimal format: %s", amountStr)
	}

	return NewMonetaryFromDecimal(asset, decimal), nil
}

// ValidateMonetary reports whether mon has a non-nil, non-negative amount.
func ValidateMonetary(mon Monetary) error {
	if mon.Amount == nil {
		return ErrNilAmount
	}
	if mon.Amount.Sign() < 0 {
		return ErrNegativeAmount
	}
	return nil
}

// Add returns m + other. Both must share the same asset.
func (m *Monetary) Add(other *Monetary) (*Monetary, error) {
	if m.Asset.Asset != other.Asset.Asset {
		return nil, fmt.Errorf("cannot add different assets: %s and %s", m.Asset.Asset, other.Asset.Asset)
	}
	result := new(big.Int).Add(m.Amount, other.Amount)
	return &Monetary{Asset: m.Asset, Amount: result}, nil
}

// Subtract returns m - other. Both must share the same asset and the result
// must not be negative.
func (m *Monetary) Subtract(other *Monetary) (*Monetary, error) {
	if m.Asset.Asset != other.Asset.Asset {
		return nil, fmt.Errorf("cannot subtract different assets: %s and %s", m.Asset.Asset, other.Asset.Asset)
	}
	result := new(big.Int).Sub(m.Amount, other.Amount)
	if result.Sign() < 0 {
		return nil, ErrNegativeAmount
	}
	return &Monetary{Asset: m.Asset, Amount: result}, nil
}

// Multiply returns m scaled by an integer factor.
func (m *Monetary) Multiply(factor *big.Int) (*Monetary, error) {
	if factor == nil {
		return nil, fmt.Errorf("factor cannot be nil")
	}
	if factor.Sign() < 0 {
		return nil, fmt.Errorf("factor cannot be negative")
	}
	result := new(big.Int).Mul(m.Amount, factor)
	return &Monetary{Asset: m.Asset, Amount: result}, nil
}

// Divide returns m divided by an integer divisor, truncating toward zero.
// It returns ErrDivisionByZero for a zero divisor.
func (m *Monetary) Divide(divisor *big.Int) (*Monetary, error) {
	if divisor == nil {
		return nil, fmt.Errorf("divisor cannot be nil")
	}
	if divisor.Sign() == 0 {
		return nil, ErrDivisionByZero
	}
	if divisor.Sign() < 0 {
		return nil, fmt.Errorf("divisor cannot be negative")
	}
	result := new(big.Int).Div(m.Amount, divisor)
	return &Monetary{Asset: m.Asset, Amount: result}, nil
}

// IsZero reports whether the amount is exactly zero.
func (m *Monetary) IsZero() bool {
	return m.Amount != nil && m.Amount.Sign() == 0
}

// FormatAmount renders the amount as a decimal string using the asset's precision.
func (m *Monetary) FormatAmount() string {
	if m.Amount == nil {
		return "nil"
	}

	if m.Asset.Precision == 0 {
		return m.Amount.String()
	}

	// Convert to decimal representation
	decimal := m.ToDecimal()
	return decimal.Text('f', m.Asset.Precision)
}

// FindAssetBySymbol looks up a well-known asset by its display symbol.
func FindAssetBySymbol(symbol string) (Asset, bool) {
	// Create a registry of all known assets
	allAssets := []Asset{
		// Currencies
		BRL, USD, GBP, CHF, JPY, ARS, CLP, CAD, MXN, COP,
		// Cryptocurrencies
		BTC, ETH, USDT, USDC, DAI, SOL, TRX, BNB, MATIC, AVAX, LINK, ATOM, DOGE, SHIB,
	}

	for _, asset := range allAssets {
		if strings.EqualFold(asset.Symbol, symbol) {
			return asset, true
		}
	}
	return Asset{}, false
}

// FindAssetByName looks up a well-known asset by its name, e.g. "USD".
func FindAssetByName(name string) (Asset, bool) {
	// Create a registry of all known assets
	allAssets := []Asset{
		// Currencies
		BRL, USD, GBP, CHF, JPY, ARS, CLP, CAD, MXN, COP,
		// Cryptocurrencies
		BTC, ETH, USDT, USDC, DAI, SOL, TRX, BNB, MATIC, AVAX, LINK, ATOM, DOGE, SHIB,
	}

	for _, asset := range allAssets {
		if strings.EqualFold(asset.Asset, name) {
			return asset, true
		}
	}
	return Asset{}, false
}

// Equal reports whether both amounts have the same asset and value.
func (m *Monetary) Equal(other *Monetary) bool {
	return m.Asset.Asset == other.Asset.Asset && m.Amount.Cmp(other.Amount) == 0
}

// GreaterThan reports whether m > other. Both must share the same asset.
func (m *Monetary) GreaterThan(other *Monetary) (bool, error) {
	if m.Asset.Asset != other.Asset.Asset {
		return false, fmt.Errorf("cannot compare different assets: %s and %s", m.Asset.Asset, other.Asset.Asset)
	}
	return m.Amount.Cmp(other.Amount) > 0, nil
}

// LessThan reports whether m < other. Both must share the same asset.
func (m *Monetary) LessThan(other *Monetary) (bool, error) {
	if m.Asset.Asset != other.Asset.Asset {
		return false, fmt.Errorf("cannot compare different assets: %s and %s", m.Asset.Asset, other.Asset.Asset)
	}
	return m.Amount.Cmp(other.Amount) < 0, nil
}

// Zero returns a zero amount of the given asset.
func Zero(asset Asset) *Monetary {
	return &Monetary{Asset: asset, Amount: big.NewInt(0)}
}

// MarshalJSON encodes the amount as {"asset": ..., "amount": "<decimal string>"}.
func (m *Monetary) MarshalJSON() ([]byte, error) {
	type monetaryJSON struct {
		Asset  Asset  `json:"asset"`
		Amount string `json:"amount"`
	}

	amountStr := ""
	if m.Amount != nil {
		amountStr = m.Amount.String()
	}

	return json.Marshal(monetaryJSON{
		Asset:  m.Asset,
		Amount: amountStr,
	})
}

// UnmarshalJSON decodes the representation produced by MarshalJSON.
func (m *Monetary) UnmarshalJSON(data []byte) error {
	type monetaryJSON struct {
		Asset  Asset  `json:"asset"`
		Amount string `json:"amount"`
	}

	var temp monetaryJSON
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	m.Asset = temp.Asset

	if temp.Amount == "" {
		m.Amount = nil
	} else {
		amount := new(big.Int)
		if _, ok := amount.SetString(temp.Amount, 10); !ok {
			return fmt.Errorf("invalid amount format: %s", temp.Amount)
		}
		m.Amount = amount
	}

	return nil
}

// ToDecimal converts the amount to a big.Float in whole units. Prefer the
// exact integer API for arithmetic; use this for display and interop only.
func (m *Monetary) ToDecimal() *big.Float {
	if m.Amount == nil {
		return nil
	}

	// Convert big.Int to big.Float
	amountFloat := new(big.Float).SetInt(m.Amount)

	// Divide by 10^precision to get the decimal representation
	if m.Asset.Precision > 0 {
		divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(m.Asset.Precision)), nil))
		amountFloat.Quo(amountFloat, divisor)
	}

	return amountFloat
}

// NewMonetaryFromDecimal converts a whole-unit big.Float into an amount,
// truncating any precision beyond the asset's.
func NewMonetaryFromDecimal(asset Asset, decimal *big.Float) *Monetary {
	if decimal == nil {
		return &Monetary{Asset: asset, Amount: nil}
	}

	// Multiply by 10^precision to get the integer representation
	multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(asset.Precision)), nil))
	result := new(big.Float).Mul(decimal, multiplier)

	// Convert to big.Int
	amount, _ := result.Int(nil)

	return &Monetary{Asset: asset, Amount: amount}
}

// Copy returns an independent copy of m.
func (m *Monetary) Copy() *Monetary {
	return &Monetary{
		Asset:  m.Asset,
		Amount: new(big.Int).Set(m.Amount),
	}
}

// Error is the error type returned by this package; values are comparable
// with errors.Is.
type Error string

// Error implements the error interface.
func (e Error) Error() string { return string(e) }

// Errors returned by the monetary operations.
const (
	ErrNilAmount      Error = "amount cannot be nil"
	ErrNegativeAmount Error = "amount cannot be negative"
	ErrAssetMismatch  Error = "assets do not match"
	ErrDivisionByZero Error = "division by zero"
)
