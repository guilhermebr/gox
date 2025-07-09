package monetary

import (
	"encoding/json"
	"math/big"
	"testing"
)

func TestNewMonetary(t *testing.T) {
	tests := []struct {
		name        string
		asset       Asset
		amount      *big.Int
		expectError bool
		errorType   Error
	}{
		{
			name:        "valid positive amount",
			asset:       USD,
			amount:      big.NewInt(10050),
			expectError: false,
		},
		{
			name:        "valid zero amount",
			asset:       USD,
			amount:      big.NewInt(0),
			expectError: false,
		},
		{
			name:        "nil amount",
			asset:       USD,
			amount:      nil,
			expectError: true,
			errorType:   ErrNilAmount,
		},
		{
			name:        "negative amount",
			asset:       USD,
			amount:      big.NewInt(-100),
			expectError: true,
			errorType:   ErrNegativeAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewMonetary(tt.asset, tt.amount)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if err != tt.errorType {
					t.Errorf("expected error %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Asset != tt.asset {
				t.Errorf("expected asset %v, got %v", tt.asset, result.Asset)
			}

			if result.Amount.Cmp(tt.amount) != 0 {
				t.Errorf("expected amount %v, got %v", tt.amount, result.Amount)
			}
		})
	}
}

func TestNewMonetaryFromString(t *testing.T) {
	tests := []struct {
		name        string
		asset       Asset
		amountStr   string
		expectError bool
		expectedInt *big.Int
	}{
		{
			name:        "valid decimal USD",
			asset:       USD,
			amountStr:   "100.50",
			expectError: false,
			expectedInt: big.NewInt(10050),
		},
		{
			name:        "valid integer USD",
			asset:       USD,
			amountStr:   "100",
			expectError: false,
			expectedInt: big.NewInt(10000),
		},
		{
			name:        "valid BTC amount",
			asset:       BTC,
			amountStr:   "0.00123456",
			expectError: false,
			expectedInt: big.NewInt(123456),
		},
		{
			name:        "empty string",
			asset:       USD,
			amountStr:   "",
			expectError: true,
		},
		{
			name:        "invalid format",
			asset:       USD,
			amountStr:   "abc",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewMonetaryFromString(tt.asset, tt.amountStr)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Amount.Cmp(tt.expectedInt) != 0 {
				t.Errorf("expected amount %v, got %v", tt.expectedInt, result.Amount)
			}
		})
	}
}

func TestMonetaryAdd(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.00")
	usd50, _ := NewMonetaryFromString(USD, "50.00")
	gbp100, _ := NewMonetaryFromString(GBP, "100.00")

	tests := []struct {
		name        string
		m1          *Monetary
		m2          *Monetary
		expectError bool
		expected    string
	}{
		{
			name:        "same asset addition",
			m1:          usd100,
			m2:          usd50,
			expectError: false,
			expected:    "150.00",
		},
		{
			name:        "different asset addition",
			m1:          usd100,
			m2:          gbp100,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.m1.Add(tt.m2)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.FormatAmount() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.FormatAmount())
			}
		})
	}
}

func TestMonetarySubtract(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.00")
	usd50, _ := NewMonetaryFromString(USD, "50.00")
	usd200, _ := NewMonetaryFromString(USD, "200.00")

	tests := []struct {
		name        string
		m1          *Monetary
		m2          *Monetary
		expectError bool
		expected    string
	}{
		{
			name:        "valid subtraction",
			m1:          usd100,
			m2:          usd50,
			expectError: false,
			expected:    "50.00",
		},
		{
			name:        "negative result",
			m1:          usd100,
			m2:          usd200,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.m1.Subtract(tt.m2)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.FormatAmount() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.FormatAmount())
			}
		})
	}
}

func TestMonetaryMultiply(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.00")

	tests := []struct {
		name        string
		m           *Monetary
		factor      *big.Int
		expectError bool
		expected    string
	}{
		{
			name:        "multiply by 2",
			m:           usd100,
			factor:      big.NewInt(2),
			expectError: false,
			expected:    "200.00",
		},
		{
			name:        "multiply by 0",
			m:           usd100,
			factor:      big.NewInt(0),
			expectError: false,
			expected:    "0.00",
		},
		{
			name:        "multiply by negative",
			m:           usd100,
			factor:      big.NewInt(-2),
			expectError: true,
		},
		{
			name:        "multiply by nil",
			m:           usd100,
			factor:      nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.m.Multiply(tt.factor)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.FormatAmount() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.FormatAmount())
			}
		})
	}
}

func TestMonetaryDivide(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.00")

	tests := []struct {
		name        string
		m           *Monetary
		divisor     *big.Int
		expectError bool
		expected    string
	}{
		{
			name:        "divide by 2",
			m:           usd100,
			divisor:     big.NewInt(2),
			expectError: false,
			expected:    "50.00",
		},
		{
			name:        "divide by 0",
			m:           usd100,
			divisor:     big.NewInt(0),
			expectError: true,
		},
		{
			name:        "divide by negative",
			m:           usd100,
			divisor:     big.NewInt(-2),
			expectError: true,
		},
		{
			name:        "divide by nil",
			m:           usd100,
			divisor:     nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.m.Divide(tt.divisor)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.FormatAmount() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result.FormatAmount())
			}
		})
	}
}

func TestMonetaryComparison(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.00")
	usd50, _ := NewMonetaryFromString(USD, "50.00")
	usd100_copy, _ := NewMonetaryFromString(USD, "100.00")
	gbp100, _ := NewMonetaryFromString(GBP, "100.00")

	// Test Equal
	if !usd100.Equal(usd100_copy) {
		t.Errorf("expected equal amounts to be equal")
	}

	if usd100.Equal(usd50) {
		t.Errorf("expected different amounts to not be equal")
	}

	if usd100.Equal(gbp100) {
		t.Errorf("expected different assets to not be equal")
	}

	// Test GreaterThan
	gt, err := usd100.GreaterThan(usd50)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !gt {
		t.Errorf("expected 100 > 50")
	}

	// Test LessThan
	lt, err := usd50.LessThan(usd100)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !lt {
		t.Errorf("expected 50 < 100")
	}

	// Test comparison with different assets
	_, err = usd100.GreaterThan(gbp100)
	if err == nil {
		t.Errorf("expected error when comparing different assets")
	}
}

func TestFindAssetBySymbol(t *testing.T) {
	tests := []struct {
		name     string
		symbol   string
		expected Asset
		found    bool
	}{
		{
			name:     "find USD",
			symbol:   "$",
			expected: USD,
			found:    true,
		},
		{
			name:     "find BTC",
			symbol:   "BTC",
			expected: BTC,
			found:    true,
		},
		{
			name:     "case insensitive",
			symbol:   "btc",
			expected: BTC,
			found:    true,
		},
		{
			name:     "not found",
			symbol:   "XYZ",
			expected: Asset{},
			found:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, found := FindAssetBySymbol(tt.symbol)

			if found != tt.found {
				t.Errorf("expected found=%v, got found=%v", tt.found, found)
			}

			if found && result != tt.expected {
				t.Errorf("expected asset %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFindAssetByName(t *testing.T) {
	tests := []struct {
		name      string
		assetName string
		expected  Asset
		found     bool
	}{
		{
			name:      "find USD",
			assetName: "USD",
			expected:  USD,
			found:     true,
		},
		{
			name:      "find BTC",
			assetName: "BTC",
			expected:  BTC,
			found:     true,
		},
		{
			name:      "case insensitive",
			assetName: "usd",
			expected:  USD,
			found:     true,
		},
		{
			name:      "not found",
			assetName: "XYZ",
			expected:  Asset{},
			found:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, found := FindAssetByName(tt.assetName)

			if found != tt.found {
				t.Errorf("expected found=%v, got found=%v", tt.found, found)
			}

			if found && result != tt.expected {
				t.Errorf("expected asset %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestZero(t *testing.T) {
	zero := Zero(USD)

	if !zero.IsZero() {
		t.Errorf("expected zero amount to be zero")
	}

	if zero.Asset != USD {
		t.Errorf("expected asset to be USD, got %v", zero.Asset)
	}

	if zero.Amount.Sign() != 0 {
		t.Errorf("expected amount to be zero, got %v", zero.Amount)
	}
}

func TestMonetaryString(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.50")

	expected := "[USD ($) 100.50]"
	result := usd100.String()

	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}

	// Test nil amount
	nilMonetary := &Monetary{Asset: USD, Amount: nil}
	expectedNil := "[USD ($) nil]"
	resultNil := nilMonetary.String()

	if resultNil != expectedNil {
		t.Errorf("expected %s, got %s", expectedNil, resultNil)
	}
}

func TestMonetaryJSON(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.50")

	// Test marshaling
	jsonData, err := json.Marshal(usd100)
	if err != nil {
		t.Errorf("error marshaling: %v", err)
	}

	// Test unmarshaling
	var unmarshaled Monetary
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Errorf("error unmarshaling: %v", err)
	}

	if !usd100.Equal(&unmarshaled) {
		t.Errorf("unmarshaled value doesn't match original")
	}
}

func TestMonetaryCopy(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.50")
	copy := usd100.Copy()

	if !usd100.Equal(copy) {
		t.Errorf("copy should be equal to original")
	}

	// Modify original to ensure independence
	usd100.Amount.Add(usd100.Amount, big.NewInt(100))

	if usd100.Equal(copy) {
		t.Errorf("copy should be independent of original")
	}
}

func TestToDecimal(t *testing.T) {
	usd100, _ := NewMonetaryFromString(USD, "100.50")
	decimal := usd100.ToDecimal()

	expected := "100.5"
	result := decimal.String()

	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}

	// Test with nil amount
	nilMonetary := &Monetary{Asset: USD, Amount: nil}
	nilDecimal := nilMonetary.ToDecimal()

	if nilDecimal != nil {
		t.Errorf("expected nil decimal for nil amount")
	}
}

func TestValidateMonetary(t *testing.T) {
	tests := []struct {
		name        string
		monetary    Monetary
		expectError bool
		errorType   Error
	}{
		{
			name:        "valid monetary",
			monetary:    Monetary{Asset: USD, Amount: big.NewInt(100)},
			expectError: false,
		},
		{
			name:        "nil amount",
			monetary:    Monetary{Asset: USD, Amount: nil},
			expectError: true,
			errorType:   ErrNilAmount,
		},
		{
			name:        "negative amount",
			monetary:    Monetary{Asset: USD, Amount: big.NewInt(-100)},
			expectError: true,
			errorType:   ErrNegativeAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMonetary(tt.monetary)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if err != tt.errorType {
					t.Errorf("expected error %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
