package monetary

import (
	"math/big"
	"testing"
)

func TestNewMonetaryFromStringIsExact(t *testing.T) {
	tests := []struct {
		name   string
		asset  Asset
		in     string
		want   string // minor units, or "" when err is expected
		errIs  error
		anyErr bool
	}{
		{name: "two decimals", asset: USD, in: "100.50", want: "10050"},
		{name: "trailing zeros past precision", asset: USD, in: "12.500", want: "1250"},
		{name: "sub-cent is refused", asset: USD, in: "1.005", errIs: ErrPrecision},
		{name: "negative", asset: USD, in: "-25.00", want: "-2500"},
		{name: "explicit plus", asset: USD, in: "+3", want: "300"},
		{name: "leading dot", asset: USD, in: ".5", want: "50"},
		{name: "trailing dot", asset: USD, in: "1.", want: "100"},
		{name: "spaces around", asset: USD, in: " 7.25 ", want: "725"},
		{name: "beyond float precision", asset: USD, in: "123456789012345678901234567.89", want: "12345678901234567890123456789"},
		{name: "zero precision asset", asset: JPY, in: "1000", want: "1000"},
		{name: "zero precision asset refuses decimals", asset: JPY, in: "1000.5", errIs: ErrPrecision},
		{name: "eighteen decimals", asset: ETH, in: "1.123456789012345678", want: "1123456789012345678"},
		{name: "scientific notation is not a decimal", asset: USD, in: "1e3", anyErr: true},
		{name: "lone sign", asset: USD, in: "-", anyErr: true},
		{name: "lone dot", asset: USD, in: ".", anyErr: true},
		{name: "letters", asset: USD, in: "abc", anyErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewMonetaryFromString(tt.asset, tt.in)
			switch {
			case tt.errIs != nil:
				if err != tt.errIs {
					t.Fatalf("want %v, got %v", tt.errIs, err)
				}
			case tt.anyErr:
				if err == nil {
					t.Fatalf("want an error, got %v", got.Amount)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.Amount.String() != tt.want {
					t.Fatalf("want %s minor units, got %s", tt.want, got.Amount)
				}
			}
		})
	}
}

func TestFormatAmountIsSignedAndPadded(t *testing.T) {
	tests := []struct {
		asset Asset
		minor int64
		want  string
	}{
		{USD, 10050, "100.50"},
		{USD, 5, "0.05"},
		{USD, -5, "-0.05"},
		{USD, -10050, "-100.50"},
		{USD, 0, "0.00"},
		{JPY, -1000, "-1000"},
		{BTC, 1, "0.00000001"},
	}
	for _, tt := range tests {
		m, err := NewMonetary(tt.asset, big.NewInt(tt.minor))
		if err != nil {
			t.Fatal(err)
		}
		if got := m.FormatAmount(); got != tt.want {
			t.Errorf("%d %s: want %s, got %s", tt.minor, tt.asset.Asset, tt.want, got)
		}
	}
}

func TestSignedArithmetic(t *testing.T) {
	credit, _ := NewMonetaryFromString(USD, "-25.00")
	charge, _ := NewMonetaryFromString(USD, "80.00")

	sum, err := charge.Add(credit)
	if err != nil || sum.FormatAmount() != "55.00" {
		t.Fatalf("add: %v %v", sum, err)
	}
	if credit.Sign() != -1 || charge.Sign() != 1 || Zero(USD).Sign() != 0 {
		t.Fatal("Sign")
	}
	if credit.Neg().FormatAmount() != "25.00" || credit.Neg().Neg().Equal(credit) != true {
		t.Fatal("Neg")
	}
	// Division truncates toward zero on both sides of it.
	q, _ := credit.Divide(big.NewInt(-7))
	if q.Amount.Int64() != 357 {
		t.Fatalf("-2500 / -7 should be 357, got %v", q.Amount)
	}
	q, _ = credit.Divide(big.NewInt(7))
	if q.Amount.Int64() != -357 {
		t.Fatalf("-2500 / 7 should be -357, got %v", q.Amount)
	}
	if err := ValidateMonetary(*credit); err != nil {
		t.Fatalf("a negative amount is valid: %v", err)
	}
}

func TestInt64(t *testing.T) {
	m, _ := NewMonetaryFromString(USD, "-0.01")
	if v, ok := m.Int64(); !ok || v != -1 {
		t.Fatalf("want -1 true, got %d %v", v, ok)
	}
	big1, _ := NewMonetaryFromString(USD, "123456789012345678901234567.89")
	if _, ok := big1.Int64(); ok {
		t.Fatal("an amount wider than int64 must report false")
	}
	if _, ok := (&Monetary{Asset: USD}).Int64(); ok {
		t.Fatal("a nil amount must report false")
	}
}
