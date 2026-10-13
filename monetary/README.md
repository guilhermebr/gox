# monetary

Precise monetary arithmetic using integers (`big.Int`) with fiat and crypto assets.

## Features
- Fixed-precision per asset (e.g., USD=2, BTC=8), stored as an integer in the smallest unit
- Signed amounts: credits, refunds and negative balances are negative amounts
- Exact decimal parsing: more decimals than the asset has is `ErrPrecision`, never a silent truncation
- Add/subtract/multiply/divide, comparisons, sign and zero checks
- `Int64()` for integer columns and wire formats that carry minor units
- JSON marshal/unmarshal

## Install
```bash
go get github.com/guilhermebr/gox/monetary
```

## Usage
```go
import (
  "math/big"
  "github.com/guilhermebr/gox/monetary"
)

usd100, _ := monetary.NewMonetaryFromString(monetary.USD, "100.50")
usd50,  _ := monetary.NewMonetaryFromString(monetary.USD, "50.25")

sum, _ := usd100.Add(usd50) // 150.75

btc, _ := monetary.NewMonetaryFromString(monetary.BTC, "0.00123456")

amount := big.NewInt(10050)
usd, _ := monetary.NewMonetary(monetary.USD, amount)

_, err := monetary.NewMonetaryFromString(monetary.USD, "1.005") // err == monetary.ErrPrecision
credit, _ := monetary.NewMonetaryFromString(monetary.USD, "-25.00")
cents, ok := credit.Int64()                                      // -2500, true
```

## Find assets
```go
asset, ok := monetary.FindAssetBySymbol("BTC")
asset, ok = monetary.FindAssetByName("USD")
```


