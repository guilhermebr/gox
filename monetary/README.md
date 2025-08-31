# monetary

Precise monetary arithmetic using integers (`big.Int`) with fiat and crypto assets.

## Features
- Fixed-precision per asset (e.g., USD=2, BTC=8)
- Safe add/subtract/multiply/divide
- Comparisons and zero checks
- JSON marshal/unmarshal
- Parse/format decimal strings

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
```

## Find assets
```go
asset, ok := monetary.FindAssetBySymbol("BTC")
asset, ok = monetary.FindAssetByName("USD")
```


