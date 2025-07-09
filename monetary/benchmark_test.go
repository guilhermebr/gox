package monetary

import (
	"math/big"
	"testing"
)

func BenchmarkNewMonetary(b *testing.B) {
	amount := big.NewInt(10050)

	for i := 0; i < b.N; i++ {
		_, _ = NewMonetary(USD, amount)
	}
}

func BenchmarkNewMonetaryFromString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = NewMonetaryFromString(USD, "100.50")
	}
}

func BenchmarkMonetaryAdd(b *testing.B) {
	m1, _ := NewMonetaryFromString(USD, "100.50")
	m2, _ := NewMonetaryFromString(USD, "50.25")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m1.Add(m2)
	}
}

func BenchmarkMonetarySubtract(b *testing.B) {
	m1, _ := NewMonetaryFromString(USD, "100.50")
	m2, _ := NewMonetaryFromString(USD, "50.25")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m1.Subtract(m2)
	}
}

func BenchmarkMonetaryMultiply(b *testing.B) {
	m, _ := NewMonetaryFromString(USD, "100.50")
	factor := big.NewInt(2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Multiply(factor)
	}
}

func BenchmarkMonetaryDivide(b *testing.B) {
	m, _ := NewMonetaryFromString(USD, "100.50")
	divisor := big.NewInt(2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Divide(divisor)
	}
}

func BenchmarkMonetaryEqual(b *testing.B) {
	m1, _ := NewMonetaryFromString(USD, "100.50")
	m2, _ := NewMonetaryFromString(USD, "100.50")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m1.Equal(m2)
	}
}

func BenchmarkMonetaryString(b *testing.B) {
	m, _ := NewMonetaryFromString(USD, "100.50")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.String()
	}
}

func BenchmarkMonetaryFormatAmount(b *testing.B) {
	m, _ := NewMonetaryFromString(USD, "100.50")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.FormatAmount()
	}
}

func BenchmarkMonetaryToDecimal(b *testing.B) {
	m, _ := NewMonetaryFromString(USD, "100.50")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.ToDecimal()
	}
}

func BenchmarkFindAssetBySymbol(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FindAssetBySymbol("USD")
	}
}

func BenchmarkFindAssetByName(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FindAssetByName("USD")
	}
}

func BenchmarkMonetaryJSONMarshal(b *testing.B) {
	m, _ := NewMonetaryFromString(USD, "100.50")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.MarshalJSON()
	}
}

func BenchmarkMonetaryCopy(b *testing.B) {
	m, _ := NewMonetaryFromString(USD, "100.50")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Copy()
	}
}
