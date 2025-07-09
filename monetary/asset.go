package monetary

import "fmt"

type Asset struct {
	Asset     string `json:"asset"`
	Precision int    `json:"precision"`
	Symbol    string `json:"symbol"`
	Class     string `json:"class"`
}

func (a Asset) String() string {
	return fmt.Sprintf("%s (%s)", a.Asset, a.Symbol)
}

func NewAsset(asset string, precision int, symbol string, class string) Asset {
	return Asset{Asset: asset, Precision: precision, Symbol: symbol, Class: class}
}

func (a Asset) GetPrecision() int {
	return a.Precision
}

func (a Asset) GetClass() string {
	return a.Class
}
