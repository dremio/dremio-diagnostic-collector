package strutils_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/strutils"
)

func TestLimitStringTooLong(t *testing.T) {
	a := strutils.LimitString("12345", 1)
	if a != "1" {
		t.Errorf("expected '1' but got '%v'", a)
	}
}

func TestLimitStringWhenStringIsShorterThanLimit(t *testing.T) {
	a := strutils.LimitString("12345", 100)
	if a != "12345" {
		t.Errorf("expected '12345' but got '%v'", a)
	}
}

func TestLimitStringWhenStringIsEmpty(t *testing.T) {
	a := strutils.LimitString("", 100)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}

func TestLimitStringWhenLimitAndStringAreDefault(t *testing.T) {
	a := strutils.LimitString("", 0)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}

func TestLimitStringWhenLimitIsDefault(t *testing.T) {
	a := strutils.LimitString("12345", 0)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}

func TestLimitStringWhenLimitINegative(t *testing.T) {
	a := strutils.LimitString("12345", -1)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}
