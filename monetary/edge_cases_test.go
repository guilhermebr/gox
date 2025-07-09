package monetary

import (
	"math/big"
	"testing"
)

func TestVeryLargeAmounts(t *testing.T) {
	// Test with very large amounts
	largeAmount := new(big.Int)
	largeAmount.SetString("999999999999999999999999999999999999999999", 10)

	m, err := NewMonetary(USD, largeAmount)
	if err != nil {
		t.Errorf("unexpected error with large amount: %v", err)
	}

	if m.Amount.Cmp(largeAmount) != 0 {
		t.Errorf("large amount not preserved correctly")
	}
}

func TestZeroPrecisionAssets(t *testing.T) {
	// Test with zero precision assets like JPY
	jpy, err := NewMonetaryFromString(JPY, "1000")
	if err != nil {
		t.Errorf("unexpected error creating JPY: %v", err)
	}

	formatted := jpy.FormatAmount()
	if formatted != "1000" {
		t.Errorf("expected 1000, got %s", formatted)
	}

	// Test decimal conversion
	decimal := jpy.ToDecimal()
	if decimal.String() != "1000" {
		t.Errorf("expected 1000, got %s", decimal.String())
	}
}

func TestHighPrecisionAssets(t *testing.T) {
	// Test with high precision assets like ETH (18 decimals)
	eth, err := NewMonetaryFromString(ETH, "1.123456789012345678")
	if err != nil {
		t.Errorf("unexpected error creating ETH: %v", err)
	}

	formatted := eth.FormatAmount()
	if formatted != "1.123456789012345678" {
		t.Errorf("expected 1.123456789012345678, got %s", formatted)
	}
}

func TestVerySmallAmounts(t *testing.T) {
	// Test with very small BTC amounts
	btc, err := NewMonetaryFromString(BTC, "0.00000001")
	if err != nil {
		t.Errorf("unexpected error creating small BTC: %v", err)
	}

	formatted := btc.FormatAmount()
	if formatted != "0.00000001" {
		t.Errorf("expected 0.00000001, got %s", formatted)
	}

	// Test that it's equal to 1 satoshi
	oneSatoshi, _ := NewMonetary(BTC, big.NewInt(1))
	if !btc.Equal(oneSatoshi) {
		t.Errorf("expected 0.00000001 BTC to equal 1 satoshi")
	}
}

func TestDecimalPrecisionLoss(t *testing.T) {
	// Test that precision is maintained through conversion
	original := "123.456789"
	usd, err := NewMonetaryFromString(USD, original)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	decimal := usd.ToDecimal()
	reconstructed := NewMonetaryFromDecimal(USD, decimal)

	if !usd.Equal(reconstructed) {
		t.Errorf("precision lost during decimal conversion")
	}
}

func TestArithmeticOverflow(t *testing.T) {
	// Test arithmetic operations with very large numbers
	largeAmount := new(big.Int)
	largeAmount.SetString("999999999999999999999999999999999999999999", 10)

	m1, _ := NewMonetary(USD, largeAmount)
	m2, _ := NewMonetary(USD, big.NewInt(1))

	// This should not overflow since we're using big.Int
	result, err := m1.Add(m2)
	if err != nil {
		t.Errorf("unexpected error with large addition: %v", err)
	}

	expected := new(big.Int)
	expected.SetString("1000000000000000000000000000000000000000000", 10)

	if result.Amount.Cmp(expected) != 0 {
		t.Errorf("large addition result incorrect")
	}
}

func TestRoundingBehavior(t *testing.T) {
	// Test how the package handles rounding during decimal conversion
	// This is especially important for division operations

	// Create $1.00 and divide by 3
	one, _ := NewMonetaryFromString(USD, "1.00")
	three := big.NewInt(3)

	result, err := one.Divide(three)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should be 33 cents (truncated)
	expected := big.NewInt(33)
	if result.Amount.Cmp(expected) != 0 {
		t.Errorf("expected 33 cents, got %v", result.Amount)
	}
}

func TestAssetLookupEdgeCases(t *testing.T) {
	// Test case insensitive lookup by symbol
	asset, found := FindAssetBySymbol("$")
	if !found || asset.Asset != "USD" {
		t.Errorf("symbol lookup for $ failed")
	}

	// Test case insensitive lookup by name
	asset, found = FindAssetByName("usd")
	if !found || asset.Asset != "USD" {
		t.Errorf("case insensitive name lookup failed")
	}

	// Test with empty string
	_, found = FindAssetBySymbol("")
	if found {
		t.Errorf("empty string should not find asset")
	}

	// Test with whitespace
	_, found = FindAssetBySymbol(" USD ")
	if found {
		t.Errorf("whitespace should not match")
	}
}

func TestJSONEdgeCases(t *testing.T) {
	// Test JSON marshaling/unmarshaling with edge cases

	// Test with zero amount
	zero := Zero(USD)
	data, err := zero.MarshalJSON()
	if err != nil {
		t.Errorf("error marshaling zero: %v", err)
	}

	var unmarshaled Monetary
	err = unmarshaled.UnmarshalJSON(data)
	if err != nil {
		t.Errorf("error unmarshaling zero: %v", err)
	}

	if !zero.Equal(&unmarshaled) {
		t.Errorf("zero value not preserved through JSON")
	}

	// Test with nil amount
	nilMonetary := &Monetary{Asset: USD, Amount: nil}
	data, err = nilMonetary.MarshalJSON()
	if err != nil {
		t.Errorf("error marshaling nil: %v", err)
	}

	var nilUnmarshaled Monetary
	err = nilUnmarshaled.UnmarshalJSON(data)
	if err != nil {
		t.Errorf("error unmarshaling nil: %v", err)
	}

	if nilUnmarshaled.Amount != nil {
		t.Errorf("nil amount not preserved through JSON")
	}
}

func TestStringFormattingEdgeCases(t *testing.T) {
	// Test string formatting with different assets and amounts

	// Test with JPY (no decimal places)
	jpy, _ := NewMonetaryFromString(JPY, "1000")
	jpyStr := jpy.String()
	expected := "[JPY (¥) 1000]"
	if jpyStr != expected {
		t.Errorf("expected %s, got %s", expected, jpyStr)
	}

	// Test with BTC (8 decimal places)
	btc, _ := NewMonetaryFromString(BTC, "0.12345678")
	btcStr := btc.String()
	expected = "[BTC (BTC) 0.12345678]"
	if btcStr != expected {
		t.Errorf("expected %s, got %s", expected, btcStr)
	}

	// Test with very small amount
	smallBtc, _ := NewMonetary(BTC, big.NewInt(1))
	smallStr := smallBtc.String()
	expected = "[BTC (BTC) 0.00000001]"
	if smallStr != expected {
		t.Errorf("expected %s, got %s", expected, smallStr)
	}
}

func TestCopyIndependence(t *testing.T) {
	// Test that copies are truly independent
	original, _ := NewMonetaryFromString(USD, "100.00")
	copy1 := original.Copy()
	copy2 := original.Copy()

	// Modify original
	original.Amount.Add(original.Amount, big.NewInt(5000)) // Add $50

	// Copies should be unchanged
	if !copy1.Equal(copy2) {
		t.Errorf("copies should be equal to each other")
	}

	expected, _ := NewMonetaryFromString(USD, "100.00")
	if !copy1.Equal(expected) {
		t.Errorf("copy should equal original value")
	}
}

func TestNegativeSubtractionEdgeCases(t *testing.T) {
	// Test edge cases around negative subtraction
	small, _ := NewMonetaryFromString(USD, "0.01")
	large, _ := NewMonetaryFromString(USD, "100.00")

	// This should fail
	_, err := small.Subtract(large)
	if err == nil {
		t.Errorf("expected error when subtracting larger from smaller")
	}

	// Test subtracting exactly equal amounts
	same, _ := NewMonetaryFromString(USD, "100.00")
	result, err := large.Subtract(same)
	if err != nil {
		t.Errorf("unexpected error subtracting equal amounts: %v", err)
	}

	if !result.IsZero() {
		t.Errorf("expected zero result when subtracting equal amounts")
	}
}
