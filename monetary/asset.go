package monetary

import "fmt"

// Asset describes a unit of value: its name, decimal precision, display symbol and class (currency or cryptocurrency).
type Asset struct {
	Asset     string `json:"asset"`
	Precision int    `json:"precision"`
	Symbol    string `json:"symbol"`
	Class     string `json:"class"`
}

// String returns the asset name and symbol, e.g. "USD ($)".
func (a Asset) String() string {
	return fmt.Sprintf("%s (%s)", a.Asset, a.Symbol)
}

// NewAsset builds an Asset from its name, decimal precision, display symbol and class.
func NewAsset(asset string, precision int, symbol string, class string) Asset {
	return Asset{Asset: asset, Precision: precision, Symbol: symbol, Class: class}
}

// GetPrecision returns the number of decimal places the asset is denominated in.
func (a Asset) GetPrecision() int {
	return a.Precision
}

// GetClass returns the asset class, e.g. "currency" or "cryptocurrency".
func (a Asset) GetClass() string {
	return a.Class
}
